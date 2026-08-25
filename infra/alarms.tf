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
# 보는 것은 borrower=game 이다. 합친 값에는 사후 분석과 검토가 섞여 있어서, 그쪽이 튀어도
# 사람이 기다린 것인지 알 수 없다.
#
# 1분 창으로 최근 5분 중 3분을 요구한다. 5분 창 두 회차였을 때는 동시 6판에서 1분 p95 가
# 5초인데도 안 울렸다(journal §104). 단발 겹침(퀴즈 생성이 판 끝에서 수십 초를 잡는 것)에는
# 여전히 안 울린다 — 3분이 필요하다.
#
# 표본이 적을 때 1분 p95 가 튀는 함정은 이 지표에 없다. 풀이 2라 한두 판이 겹칠 때 대기는
# 구조적으로 0이고, 실측도 동시 1판에서 0.0 이었다.
#
# 임계 3초는 실측으로 잡았다. 동시 3판이 2.4초까지, 4판이 3.5초까지 간다 — 3초는 네 판째를
# 잡고 세 판째는 넘긴다.
resource "aws_cloudwatch_metric_alarm" "engine_pool_wait" {
  alarm_name          = "show-gi-engine-pool-wait"
  alarm_description   = "대국이 엔진을 빌리기까지의 대기 p95가 최근 5분 중 3분에서 3초를 넘었다. 풀이 아니라 vCPU 를 올릴 자리다(journal §104)"
  namespace           = "show-gi"
  metric_name         = "EnginePoolWaitGameSeconds"
  extended_statistic  = "p95"
  period              = 60
  evaluation_periods  = 5
  datapoints_to_alarm = 3
  threshold           = 3
  comparison_operator = "GreaterThanThreshold"

  # 차원 둘을 다 적어야 한다. EMF 가 Service·Environment 를 차원으로 내므로
  # 하나만 적으면 그런 계열이 없어서 알람이 「데이터 없음」으로 조용히 산다.
  dimensions = { Service = "api", Environment = "prod" }

  alarm_actions = [aws_sns_topic.alarms.arn]

  # 아무도 안 두는 시간에는 표본이 없어 지표가 안 나온다. 조용한 것은 위반이 아니다.
  treat_missing_data = "notBreaching"
}

# 사후 분석이 밀린다. 위의 engine_pool_wait 은 이 상태를 못 본다 — 대인전 포화에서
# 사람의 풀 대기는 표본이 아예 0이었다(journal §107 · §108). 병목이 풀이 아니라
# 「엔진이 낼 수 있는 탐색 수」라서, 대국은 우선줄이라 안 밀리고 분석만 줄을 선다.
#
# 그래서 신호가 계열이 다르다. 이른 것은 EngineSearchSeconds 이고(대시보드가 맡는다)
# 확정은 이 값의 지속 증가다. 실측으로 갈리는 폭이 넓다 — 6판이 최고 13에 0으로
# 돌아오고, 8판은 4분째에 140을 넘겨 756까지 단조로 자랐다(journal §108).
#
# 임계 100 은 그 사이에 둔다. 6판은 근처에도 안 가고 8판은 4분째에 넘긴다.
#
# 5분을 다 요구하는 것은 봉우리를 거르기 위해서다. 판이 끝나는 순간 아직 안 잰 手가
# 한꺼번에 들어오므로(journal §105) 단발로 100을 넘는 것은 정상이다 — 5분을 넘겨
# 머무르는 것이 「따라가지 못하고 있다」다.
#
# 태스크가 둘이어도 통계가 맞다. 줄이 표라(018·019) 게이지가 표를 읽은 전역 값이고
# 태스크마다 같은 숫자를 올린다 — Maximum 이 그 값 그대로다(journal §119).
# Sum 으로 바꾸면 태스크 수만큼 곱해진다.
resource "aws_cloudwatch_metric_alarm" "analysis_backlog" {
  alarm_name          = "show-gi-analysis-backlog"
  alarm_description   = "사후 분석 줄이 5분 내내 100手를 넘었다. 되짚기가 그만큼 늦게 준비된다 — 박스의 탐색 처리량이 도착을 못 따라가는 자리다(journal §108)"
  namespace           = "show-gi"
  metric_name         = "AnalysisBacklogPlies"
  statistic           = "Maximum"
  period              = 60
  evaluation_periods  = 5
  datapoints_to_alarm = 5
  threshold           = 100
  comparison_operator = "GreaterThanThreshold"

  dimensions = { Service = "api", Environment = "prod" }

  alarm_actions = [aws_sns_topic.alarms.arn]
  ok_actions    = [aws_sns_topic.alarms.arn]

  # 아무도 안 두면 지표가 안 나온다. 조용한 것은 위반이 아니다.
  treat_missing_data = "notBreaching"
}

# ─── 스팟이 회수되기 전에 알기 ───────────────────────────────

# 지표 알람으로는 회수를 미리 못 안다. HealthyHostCount 가 결측이 된 뒤에야 위반이
# 되므로 위 no_healthy_target 은 실측으로 12분 늦게 울렸다(journal §107).
#
# AWS 가 EventBridge 로 두 가지를 미리 준다. 지표가 아니라 이벤트라 알람으로 못 받고,
# 그래서 여기만 EventBridge 를 쓴다.
#
#   EC2 Instance Rebalance Recommendation  회수 위험이 높아졌다 (보통 가장 이르다)
#   EC2 Spot Instance Interruption Warning 회수 2분 전
#
# 2분으로 용량을 못 구해 온다. 값은 「왜 내려갔나」를 나중에 추측하지 않는 것과,
# 부하 회차 도중이면 그 회차가 곧 무효가 되는 것을 그 자리에서 아는 것이다 — 계단
# 하나가 8분인데 박스가 12분을 산 날이 있었다(journal §107).
resource "aws_sns_topic" "spot" {
  name = "show-gi-spot"
}

# 알람 토픽에 얹지 않는다. 두 가지 이유이고 둘 다 실무적이다.
#
# 첫째, 알람 토픽에는 정책 리소스가 없어서 AWS 기본 정책으로 돌고 있다. EventBridge 를
# 붙이려면 정책을 명시해야 하는데, 그 순간 기본 정책이 대체되어 지금 오는 알람 메일이
# 조용히 끊길 수 있다.
#
# 둘째, 스팟 이벤트는 시끄럽다 — 하루에 다섯 번 뜬 날이 있다. 갈라 두면 이쪽만 끌 수 있다.
resource "aws_sns_topic_subscription" "spot_email" {
  count = var.alarm_email == "" ? 0 : 1

  topic_arn = aws_sns_topic.spot.arn
  protocol  = "email"
  endpoint  = var.alarm_email
}

# EventBridge 가 이 토픽에 넣을 수 있게 한다. 기본 정책은 계정 안의 주체만 허용하고
# 서비스 주체는 안 들어가 있어서, 이 문장이 없으면 규칙이 조용히 아무것도 안 한다.
data "aws_iam_policy_document" "spot_topic" {
  statement {
    actions   = ["SNS:Publish"]
    resources = [aws_sns_topic.spot.arn]

    principals {
      type        = "Service"
      identifiers = ["events.amazonaws.com"]
    }
  }
}

resource "aws_sns_topic_policy" "spot" {
  arn    = aws_sns_topic.spot.arn
  policy = data.aws_iam_policy_document.spot_topic.json
}

# 계정 전체의 스팟 이벤트를 받는다. 인스턴스 id 로 좁히지 않는 이유는 그 id 가 회수마다
# 바뀌기 때문이다 — 이 계정에 스팟은 이 서비스뿐이라 좁힐 것이 없다.
resource "aws_cloudwatch_event_rule" "spot_interruption" {
  name        = "show-gi-spot-interruption"
  description = "스팟 회수 2분 전 통보와 재조정 권고"

  event_pattern = jsonencode({
    source = ["aws.ec2"]
    "detail-type" = [
      "EC2 Spot Instance Interruption Warning",
      "EC2 Instance Rebalance Recommendation",
    ]
  })
}

# 메일로 읽을 수 있게 바꿔서 보낸다. 원본 이벤트를 그대로 보내면 JSON 한 덩어리가 와서
# 무슨 일인지 읽는 데 시간이 걸린다.
resource "aws_cloudwatch_event_target" "spot_to_sns" {
  rule      = aws_cloudwatch_event_rule.spot_interruption.name
  target_id = "sns"
  arn       = aws_sns_topic.spot.arn

  input_transformer {
    input_paths = {
      kind     = "$.detail-type"
      instance = "$.detail.instance-id"
      at       = "$.time"
    }
    # 따옴표가 있어야 SNS 가 문자열로 받는다. 없으면 규칙이 유효하지 않다.
    input_template = "\"<kind> — <instance> at <at>\""
  }
}
