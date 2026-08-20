# 알람. 지표를 내는 것과 「누가 알아채나」는 다른 일이라 파일을 갈라 둔다.
#
# 커스텀 지표는 api 컨테이너가 stdout 으로 내는 EMF 에서 나온다(internal/metrics).
# CloudWatch 가 로그에서 뽑아 show-gi 이름 공간에 넣으므로, 여기서 만들 것은 없고
# 이름을 맞추기만 한다 — 이름이 어긋나면 알람이 「데이터 없음」으로 조용히 산다.
#
# ALB 쪽 지표는 EMF 와 무관하게 AWS 가 항상 낸다. 그래서 「사이트가 떴나」는 그쪽을
# 보고, 「무엇이 느린가」는 우리 지표를 본다.

resource "aws_sns_topic" "alarms" {
  name = "show-gi-alarms"
}

# 메일 주소는 변수로 받는다. 비워 두면 구독이 없고 알람은 콘솔에만 남는다 —
# 주소를 커밋할 수 없어서다(레포가 퍼블릭이다).
#
# 구독은 만든 뒤 메일에서 한 번 눌러야 활성된다. terraform 은 pending 상태까지만
# 만들고, 그 상태에서는 알람이 울려도 메일이 안 온다.
resource "aws_sns_topic_subscription" "alarms_email" {
  count = var.alarm_email == "" ? 0 : 1

  topic_arn = aws_sns_topic.alarms.arn
  protocol  = "email"
  endpoint  = var.alarm_email
}

# ─── 사이트가 떴나 ───────────────────────────────────────────

# 정상 타깃이 0인 상태. 「비정상 수」가 아니라 「정상 수」를 보는 이유는 타깃이 아예
# 등록되지 않은 경우까지 같은 알람이 덮기 때문이다 — 비정상 수는 그때 0을 낸다.
#
# 5분인 것은 정상 배포가 그보다 짧기 때문이다. 인스턴스가 한 대라 배포는 유일한
# 태스크를 먼저 내리고(ecs.tf 의 deployment_minimum_healthy_percent = 0) 새 것을 띄우며,
# 거기에 기동 유예 120초가 붙는다. 스팟 회수도 2~3분이다(README 맨 위). 1분 두 회차로
# 두면 정상 배포마다 울리고, 그러면 사람이 알람을 무시하기 시작한다.
resource "aws_cloudwatch_metric_alarm" "no_healthy_target" {
  alarm_name          = "show-gi-no-healthy-target"
  alarm_description   = "정상 타깃이 5분 동안 없다. 배포나 스팟 회수로는 이만큼 안 걸린다"
  namespace           = "AWS/ApplicationELB"
  metric_name         = "HealthyHostCount"
  statistic           = "Maximum"
  period              = 60
  evaluation_periods  = 5
  threshold           = 1
  comparison_operator = "LessThanThreshold"

  dimensions = {
    TargetGroup  = aws_lb_target_group.web.arn_suffix
    LoadBalancer = aws_lb.main.arn_suffix
  }

  alarm_actions = [aws_sns_topic.alarms.arn]
  ok_actions    = [aws_sns_topic.alarms.arn]

  # 타깃이 하나도 등록되지 않으면 이 지표가 아예 안 나온다. 그것도 「정상이 0」이므로
  # 위반으로 본다 — 위의 5분이 정상 배포를 이미 걸러 준다.
  treat_missing_data = "breaching"
}

# ─── 무엇이 깨졌나 ───────────────────────────────────────────

# 5xx 와 panic 을 합쳐서 본다. 이 앱에서 5xx 는 대개 우리 버그다 — 엔진·DB가 없는
# 상태는 503으로 나가지만 그건 기동 때 한 번 정해지고 화면이 미리 막는다.
#
# 둘을 갈라 두면 한쪽이 알람 없는 쪽이 된다. panic 이 업그레이드된 연결에서 나면 그
# 요청의 상태가 이미 101이라 5xx 로 안 잡히기 때문이다(internal/server/observe.go).
resource "aws_cloudwatch_metric_alarm" "server_errors" {
  alarm_name          = "show-gi-5xx"
  alarm_description   = "5분 동안 5xx 나 panic 이 났다. 같은 시각의 request_id 로 요청 로그를 찾는다"
  evaluation_periods  = 1
  threshold           = 1
  comparison_operator = "GreaterThanOrEqualToThreshold"

  metric_query {
    id          = "total"
    expression  = "errors + panics"
    label       = "5xx + panic"
    return_data = true
  }

  metric_query {
    id = "errors"
    metric {
      namespace   = "show-gi"
      metric_name = "HttpServerErrors"
      period      = 300
      stat        = "Sum"
      dimensions  = { Service = "api", Environment = "prod" }
    }
  }

  metric_query {
    id = "panics"
    metric {
      namespace   = "show-gi"
      metric_name = "HttpPanics"
      period      = 300
      stat        = "Sum"
      dimensions  = { Service = "api", Environment = "prod" }
    }
  }

  alarm_actions = [aws_sns_topic.alarms.arn]

  # EMF 지표는 태스크가 내려간 동안 아예 안 나온다. 그걸 위반으로 보면 배포마다 울린다 —
  # 「사이트가 떴나」는 위의 ALB 알람이 맡는다.
  treat_missing_data = "notBreaching"
}

# ─── 무엇이 느린가 ───────────────────────────────────────────

# 엔진 풀 대기. 이 앱에서 지연의 원인은 거의 이것이다 — 풀이 프로덕션에서 탐색 2개뿐이고
# (ecs.tf 의 ENGINE_POOL_SIZE) 빌리는 자리가 여섯이라, 겹치면 뒤에 온 요청이 줄을 선다.
#
# 임계치 3초는 아직 실측이 아니다. 지금은 이 알람의 일이 「울리나 안 울리나」로
# 평상시 분포를 알려주는 것이고, 값은 그 뒤에 정한다. 5분 두 회차로 둔 것은 한 번의
# 겹침(퀴즈 생성이 판 끝에서 수십 초를 잡는 것)으로는 안 울리게 하려는 것이다.
resource "aws_cloudwatch_metric_alarm" "engine_pool_wait" {
  alarm_name          = "show-gi-engine-pool-wait"
  alarm_description   = "엔진을 빌리기까지의 대기 p95가 10분 동안 3초를 넘었다. 풀 크기(ENGINE_POOL_SIZE)를 올릴 자리다"
  namespace           = "show-gi"
  metric_name         = "EnginePoolWaitSeconds"
  extended_statistic  = "p95"
  period              = 300
  evaluation_periods  = 2
  threshold           = 3
  comparison_operator = "GreaterThanThreshold"

  # 차원 둘을 다 적어야 한다. EMF 가 Service·Environment 를 차원으로 내므로
  # 하나만 적으면 그런 계열이 없어서 알람이 「데이터 없음」으로 조용히 산다.
  dimensions = { Service = "api", Environment = "prod" }

  alarm_actions = [aws_sns_topic.alarms.arn]

  # 아무도 안 두는 시간에는 표본이 없어 지표가 안 나온다. 조용한 것은 위반이 아니다.
  treat_missing_data = "notBreaching"
}
