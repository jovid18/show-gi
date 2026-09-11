# 오토스케일. 분석 티어의 대수를 알람이 돌린다.
#
# 층이 둘이고 이 파일이 움직이는 것은 위층 하나다. 서비스의 desired_count 를 올리면 용량
# 공급자가 미배치 태스크를 보고 EC2 를 따라 올린다(ecs.tf 의 analysis). 실측으로 3분이다
# (journal §121).
#
# 그래서 ASG 에 스케일 정책을 붙이지 않는다. 그 ASG 의 desired 는 이미 용량 공급자가 드는
# 값이라, 정책이 같은 값을 들면 둘이 서로 되돌린다.
#
# 신호와 임계는 여기서 정하지 않는다. AnalysisBacklogPlies 와 100 은 alarms.tf 에 있고,
# 이 파일은 그 알람에 손잡이를 잇는 것까지다.

# 정책이 움직일 수 있는 폭. 아래가 0 인 것은 상호작용 대가 SERVER_ROLE=both 로 큐를 겸해
# 집기 때문이다(ecs.tf, journal §125).
resource "aws_appautoscaling_target" "analysis" {
  service_namespace  = "ecs"
  resource_id        = "service/${aws_ecs_cluster.main.name}/${aws_ecs_service.analysis.name}"
  scalable_dimension = "ecs:service:DesiredCount"

  min_capacity = 0
  max_capacity = var.analysis_max_instances
}

# 목표 추적 대신 단계 조정이다. 목표 추적은 알람 둘을 스스로 만들고 임계를 스스로 정하는데,
# 이 층의 임계는 실측으로 이미 잡혀 있다(alarms.tf). 다시 지으면 잴 때마다 어느 쪽이
# 울렸는지를 따져야 한다.
#
# 밀린 手는 대수에 반비례하지도 않아서(journal §121) 목표 추적이 요구하는 「용량 한 단위당
# 부하」 꼴에서 벗어난다.
resource "aws_appautoscaling_policy" "analysis_out" {
  name               = "show-gi-analysis-out"
  service_namespace  = aws_appautoscaling_target.analysis.service_namespace
  resource_id        = aws_appautoscaling_target.analysis.resource_id
  scalable_dimension = aws_appautoscaling_target.analysis.scalable_dimension
  policy_type        = "StepScaling"

  step_scaling_policy_configuration {
    adjustment_type         = "ChangeInCapacity"
    metric_aggregation_type = "Maximum"

    # 한 번 올린 뒤 5분은 다시 올리지 않는다. 새 대가 뜨고 태스크를 받기까지가 3~4분이라
    # (journal §109 · §121) 그 전에 다시 재면 아직 일을 시작하지 않은 대를 「모자라다」로
    # 읽는다.
    cooldown = 300

    # 계단 하나다. 상한이 2라 폭을 가를 것이 없고, 계단이 둘이면 기울기를 읽을 수 없다
    # (ecs.tf 의 maximum_scaling_step_size 와 같은 판단).
    step_adjustment {
      metric_interval_lower_bound = 0
      scaling_adjustment          = 1
    }
  }
}

# 되돌리는 쪽. 없으면 시험 뒤에 두 대가 그대로 떠 있고, terraform 이 desired 의 주인이
# 아니라서(ecs.tf 의 ignore_changes) apply 로도 내려오지 않는다.
resource "aws_appautoscaling_policy" "analysis_in" {
  name               = "show-gi-analysis-in"
  service_namespace  = aws_appautoscaling_target.analysis.service_namespace
  resource_id        = aws_appautoscaling_target.analysis.resource_id
  scalable_dimension = aws_appautoscaling_target.analysis.scalable_dimension
  policy_type        = "StepScaling"

  step_scaling_policy_configuration {
    adjustment_type         = "ChangeInCapacity"
    metric_aggregation_type = "Maximum"

    # 올리는 쪽보다 길다. 대를 뺀 직후에 큐가 다시 차는 것이 「뺀 것이 틀렸다」인데, 그
    # 판단이 서려면 남은 대가 한 계단을 받아 봐야 한다.
    cooldown = 600

    step_adjustment {
      metric_interval_upper_bound = 0
      scaling_adjustment          = -1
    }
  }
}

# 스케일 인의 신호를 임계 100 의 반대쪽에 두지 않는다. 밀린 手는 포화 신호라 「비었다」와
# 「대수가 충분하다」가 같은 값으로 나온다(journal §121). 임계를 그 사이에 두면 부하가 도는
# 중에 대를 빼고, 그러면 백로그가 다시 부풀어 또 스케일 아웃한다.
#
# 그래서 이 알람은 「일이 아예 없다」만 본다. 임계 1 은 手 하나라도 밀려 있으면 빼지 않는다는
# 뜻이고, 30분을 다 요구하는 것은 여유가 있는 상태와 일이 없는 상태를 가르기 위해서다.
#
# 사람에게는 알리지 않는다. 조용해진 것을 사고로 보지 않는다. 언제 움직였는지는
# describe-scaling-activities 에 남는다(deploy/README.md).
resource "aws_cloudwatch_metric_alarm" "analysis_idle" {
  alarm_name          = "show-gi-analysis-idle"
  alarm_description   = "사후 분석 큐가 30분 내내 비어 있다. 분석 대를 하나 뺀다"
  namespace           = "show-gi"
  metric_name         = "AnalysisBacklogPlies"
  statistic           = "Maximum"
  period              = 60
  evaluation_periods  = 30
  datapoints_to_alarm = 30
  threshold           = 1
  comparison_operator = "LessThanThreshold"

  # dimensions 둘을 다 적는다. 하나만 적으면 알람이 「데이터 없음」으로 그대로 산다
  # (alarms.tf 의 같은 함정).
  dimensions = { Service = "api", Environment = "prod" }

  alarm_actions = [aws_appautoscaling_policy.analysis_in.arn]

  # 이 게이지는 태스크가 도는 동안 매분 나온다(비어 있으면 0 이다). 결측은 「조용하다」 대신
  # 「양쪽 티어가 다 내려가 있다」이고, 그 상태에서 대수를 정할 근거가 없다.
  treat_missing_data = "notBreaching"
}
