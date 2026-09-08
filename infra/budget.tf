# 요금이 예상 밖으로 오르면 알린다.
#
# 이 파일이 생긴 이유는 실측이다. 여덟 달 동안 요금표가 단가 곱셈이었고, 처음 청구서와
# 대조했을 때 추정($48~55)이 실제($62.55)보다 20% 낮았다 — 퍼블릭 IPv4 라인이 표에 아예
# 없었고 대신 넣어 둔 CloudWatch $5 는 청구에 없었다(journal §134). 아무것도 그 어긋남을
# 여덟 달 동안 알리지 않았다.
#
# 잡는 것은 「비싸다」가 아니라 「달라졌다」다. 부하 회차에 c6g.large 온디맨드로 올리거나
# (variables.tf 의 on_demand_base_capacity) 분석 대를 켜 두고 잊으면 하루 $2.15씩 붙는데,
# 그 종류가 상시 요금으로 굳는 것을 사람이 눈치채는 데 한 달이 걸린다.

# 예산은 리전이 없다. 계정 단위 자원이고 ARN 에도 리전 칸이 비어 있어서,
# operator 정책의 리전 조건에 걸리지 않는 자리에 권한을 넣어야 한다
# (iam-policy-autoscale.json 의 ReadGlobalCostAndInventory 와 같은 이유다).
resource "aws_budgets_budget" "monthly" {
  name         = "show-gi-monthly"
  budget_type  = "COST"
  time_unit    = "MONTHLY"
  limit_amount = "80"
  limit_unit   = "USD"

  # 실측이 $62.55 이고 여기에 세금이 붙는다. 예산이 보는 값은 세금 포함이다 —
  # 이번 달 actual 이 CE 일별 합계와 한 자리까지 같았고 그 합계에 Tax 라인이 있다.
  # 그래서 정상적인 한 달이 약 $69 이고, 이 한도는 그 위로 16% 다.
  #
  # 부하 회차는 이 선을 의도적으로 넘는다. 그때 오는 메일은 오진이 아니라 확인이다.

  # 실제로 넘었을 때. 90%로 두면 $72 라 정상 달과의 여유가 5%뿐이어서 정상 달에도
  # 울린다 — 조기 경보는 아래 예측 알림이 맡으므로 이쪽은 실제 초과만 본다.
  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_sns_topic_arns  = [aws_sns_topic.alarms.arn]
    subscriber_email_addresses = []
  }

  # 이대로 가면 넘는다는 예측. 달 중간에 구성을 바꿨을 때 이쪽이 먼저 운다 —
  # 절약 모드를 되돌리는 apply 가 정확히 그 모양이다.
  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "FORECASTED"
    subscriber_sns_topic_arns  = [aws_sns_topic.alarms.arn]
    subscriber_email_addresses = []
  }
}

# 메일이 아니라 SNS 로 보낸다. 예산은 주소를 직접 받을 수도 있는데, 그쪽은 확인 절차가
# 따로이고 확인 링크가 스팸함에 들어가면 경고 없이 아무것도 안 온다 — 2026-09-08 에 알람
# 주제의 확인 메일 두 통이 실제로 거기 있었다(journal §133). 이미 활성된 주제로 보내면
# 그 실패 모드를 한 번만 겪는다.
#
# 주제 정책을 명시하면 기본 정책이 대체된다. 그래서 계정 소유자와 CloudWatch 몫을
# 같이 적어야 한다 — budgets 만 적으면 알람 다섯이 그날부터 조용해지고,
# 그것을 알려 줄 알람이 바로 그 다섯이다.
data "aws_iam_policy_document" "alarms_topic" {
  statement {
    sid       = "OwnerFullAccess"
    actions   = ["SNS:Publish", "SNS:Subscribe", "SNS:GetTopicAttributes", "SNS:SetTopicAttributes"]
    resources = [aws_sns_topic.alarms.arn]

    principals {
      type        = "AWS"
      identifiers = [data.aws_caller_identity.current.account_id]
    }
  }

  statement {
    sid       = "CloudWatchAlarms"
    actions   = ["SNS:Publish"]
    resources = [aws_sns_topic.alarms.arn]

    principals {
      type        = "Service"
      identifiers = ["cloudwatch.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "AWS:SourceOwner"
      values   = [data.aws_caller_identity.current.account_id]
    }
  }

  statement {
    sid       = "Budgets"
    actions   = ["SNS:Publish"]
    resources = [aws_sns_topic.alarms.arn]

    principals {
      type        = "Service"
      identifiers = ["budgets.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "AWS:SourceOwner"
      values   = [data.aws_caller_identity.current.account_id]
    }
  }
}

resource "aws_sns_topic_policy" "alarms" {
  arn    = aws_sns_topic.alarms.arn
  policy = data.aws_iam_policy_document.alarms_topic.json
}
