# 대시보드. 알람이 「누가 알아채나」라면 여기는 「사람이 무엇을 보나」다.
#
# 코드로 두는 이유는 회차 비교다. 콘솔에서 그때그때 위젯을 고르면 축도 기간도 회차마다
# 달라져서 「지난번보다 나아졌나」를 말할 수 없다 — 같은 화면이 떠야 그 물음이 성립한다.
#
# 지표 이름은 EMF 가 내는 것과 맞춰야 한다(internal/metrics 의 collect). 어긋나면 위젯이
# 「데이터 없음」으로 조용히 비고, 알람과 달리 아무도 안 알려준다.
#
# 대시보드 3개까지 무료다. 하나로 두는 것은 그 한도 때문이 아니라, 회차 중에 볼 화면이
# 둘이면 사람이 둘 다 안 보기 때문이다.

locals {
  # 차원 둘을 다 적어야 한다. EMF 가 Service·Environment 를 차원으로 내므로 하나만
  # 적으면 그런 계열이 없다(alarms.tf 의 engine_pool_wait 과 같은 함정).
  #
  # 티어가 둘인데 차원은 그대로다. 두 티어가 같은 계열에 올리므로 위젯의 값은 둘을 합친
  # 것이고, 통계가 그 합치는 법이다 — 카운터는 Sum, 게이지는 Maximum 이다(journal §120).
  dash_dims = ["Service", "api", "Environment", "prod"]

  # 위젯 하나가 보는 창. EMF 가 1분에 한 줄이라 그보다 잘게 볼 수 없다.
  dash_period = 60
}

resource "aws_cloudwatch_dashboard" "main" {
  dashboard_name = "show-gi"

  dashboard_body = jsonencode({
    widgets = [
      {
        type   = "text"
        x      = 0
        y      = 0
        width  = 24
        height = 3
        properties = {
          markdown = join("\n", [
            "## 읽는 순서",
            "",
            "1. **엔진 풀 대기 — 대국**이 튀면 사람이 기다린 것이다. 같은 창에서 **분석 백로그**가 같이 올랐으면 사후 분석이 대국을 굶긴 것이고, 그것이 분석기를 별도 서비스로 뗄지의 판단 근거다(journal §101).",
            "2. **버려진 판**이 0이 아니면 그 자체가 사고다. 그 판은 평가치도 실력 추정도 없이 남고 다시 재지 않는다.",
            "3. 대기가 아니라 **탐색 시간**이 길면 원인이 큐가 아니라 CPU다 — 태스크 하나가 2 vCPU 이고 엔진이 최대 셋 돈다(journal §91).",
            "4. **탐색 수**는 티어 둘을 합친 값이고, 대를 늘렸을 때 보는 것은 **캐시를 뺀 몫**이다 — 캐시가 답한 것은 엔진을 안 쓰므로 합계는 거의 안 움직인다(journal §121). 모양은 **분석 백로그**가, 크기는 그 값이 말한다.",
            "5. **오토스케일**은 밀린 手가 100을 5분 넘기면 대를 하나 올린다(journal §124). 그 위젯에서 볼 것은 순서다 — 백로그가 올라가고, 대수가 따라 오르고, 백로그가 스스로 내려온다. 대수는 올랐는데 백로그가 안 내려오면 상한(`analysis_max_instances`)이 모자란 것이다.",
          ])
        }
      },
      {
        type   = "metric"
        x      = 0
        y      = 3
        width  = 12
        height = 6
        properties = {
          title  = "엔진 풀 대기 (초) — 탐색: 대국만 vs 전부 · 詰み"
          region = var.aws_region
          view   = "timeSeries"
          period = local.dash_period
          metrics = [
            concat(["show-gi", "EnginePoolWaitGameSeconds"], local.dash_dims, [{ stat = "p95", label = "대국 p95" }]),
            concat(["show-gi", "EnginePoolWaitGameSeconds"], local.dash_dims, [{ stat = "Maximum", label = "대국 max" }]),
            concat(["show-gi", "EnginePoolWaitSeconds"], local.dash_dims, [{ stat = "p95", label = "전부 p95" }]),
            # 詰み 풀을 같은 판에 그린다. 단위가 같고 묻는 것도 같다 — 어느 풀이 굶는가다.
            # 크기가 2라 이 선이 0에서 뜨는 순간이 곧 포화다(journal §111).
            concat(["show-gi", "MatePoolWaitSeconds"], local.dash_dims, [{ stat = "p95", label = "詰み p95" }]),
            concat(["show-gi", "MatePoolWaitSeconds"], local.dash_dims, [{ stat = "Maximum", label = "詰み max" }]),
          ]
          # 알람과 같은 선을 긋는다. 임계치가 아직 실측이 아니라(alarms.tf) 이 선의 일은
          # 「평상시가 어디에 있나」를 눈에 보이게 하는 것이다.
          annotations = { horizontal = [{ label = "알람 임계 3초", value = 3 }] }
        }
      },
      {
        type   = "metric"
        x      = 12
        y      = 3
        width  = 12
        height = 6
        properties = {
          title  = "사후 분석 — 밀린 手, 버려진 판, 中断된 판"
          region = var.aws_region
          view   = "timeSeries"
          period = local.dash_period
          metrics = [
            concat(["show-gi", "AnalysisBacklogPlies"], local.dash_dims, [{ stat = "Maximum", label = "밀린 手" }]),
            concat(["show-gi", "AnalysisGamesDropped"], local.dash_dims, [{ stat = "Sum", label = "버려진 판", yAxis = "right" }]),
            # 상대의 수를 시한 안에 못 얻어 접은 판. 부하 회차에서 서버가 무너지는 신호가
            # 이것이고, games.result 로는 셀 수 없다(journal §104).
            concat(["show-gi", "GamesAborted"], local.dash_dims, [{ stat = "Sum", label = "中断된 판", yAxis = "right" }]),
          ]
          # 축을 가른다. 밀린 手는 수백까지 가고 나머지 둘은 0이나 1이라, 한 축에 두면
          # 뒤엣것이 바닥에 붙어 안 보인다.
          yAxis = { right = { min = 0 } }
        }
      },
      {
        type   = "metric"
        x      = 0
        y      = 9
        width  = 12
        height = 6
        properties = {
          title  = "엔진 — 탐색 시간(초)과 점유"
          region = var.aws_region
          view   = "timeSeries"
          period = local.dash_period
          metrics = [
            concat(["show-gi", "EngineSearchSeconds"], local.dash_dims, [{ stat = "p50", label = "탐색 p50" }]),
            concat(["show-gi", "EngineSearchSeconds"], local.dash_dims, [{ stat = "p95", label = "탐색 p95" }]),
            concat(["show-gi", "EngineSearchSeconds"], local.dash_dims, [{ stat = "Maximum", label = "탐색 max" }]),
            concat(["show-gi", "EnginePoolInUse"], local.dash_dims, [{ stat = "Maximum", label = "점유(제일 바쁜 대)", yAxis = "right" }]),
          ]
          # 풀이 둘이다. 점유가 2에 붙어 있는 동안의 대기가 진짜 포화다.
          yAxis = { right = { min = 0, max = 3 } }
        }
      },
      {
        type   = "metric"
        x      = 12
        y      = 9
        width  = 12
        height = 6
        properties = {
          title  = "국면 캐시 히트율 (%) · 엔진이 실제로 돌린 탐색"
          region = var.aws_region
          view   = "timeSeries"
          period = local.dash_period
          metrics = [
            [{ expression = "100 * cached / searches", label = "히트율", id = "hit" }],
            # 대를 늘렸을 때 크기를 말하는 선이다. 합계는 캐시가 덮어서 거의 안 움직인다 —
            # 회차 실측이 합계 +11% 에 이 값 +46% 였다(journal §121).
            [{ expression = "searches - cached", label = "계산 탐색", id = "computed", yAxis = "right" }],
            concat(["show-gi", "EngineSearches"], local.dash_dims, [{ stat = "Sum", id = "searches", visible = false }]),
            concat(["show-gi", "EngineSearchesCached"], local.dash_dims, [{ stat = "Sum", id = "cached", visible = false }]),
          ]
          # 초반과 中盤이 갈린다 — 실측이 첫 2분 65.7%, 그 뒤 2.3% 였다(journal §91).
          # 회차 중에 이 선이 내려가는 지점이 곧 中盤에 들어간 지점이다.
          yAxis = { left = { min = 0, max = 100 } }
        }
      },
      {
        type   = "metric"
        x      = 0
        y      = 15
        width  = 12
        height = 6
        properties = {
          title  = "부하의 크기 — 세션과 요청"
          region = var.aws_region
          view   = "timeSeries"
          period = local.dash_period
          metrics = [
            concat(["show-gi", "WsSessionsActive"], local.dash_dims, [{ stat = "Maximum", label = "열린 대국 세션" }]),
            concat(["show-gi", "HttpRequests"], local.dash_dims, [{ stat = "Sum", label = "요청", yAxis = "right" }]),
          ]
        }
      },
      {
        type   = "metric"
        x      = 12
        y      = 15
        width  = 12
        height = 6
        properties = {
          title  = "사고 — 5xx 와 panic"
          region = var.aws_region
          view   = "timeSeries"
          period = local.dash_period
          metrics = [
            concat(["show-gi", "HttpServerErrors"], local.dash_dims, [{ stat = "Sum", label = "5xx" }]),
            concat(["show-gi", "HttpPanics"], local.dash_dims, [{ stat = "Sum", label = "panic" }]),
          ]
          # 0이 정상인 화면이라 축을 고정한다. 자동 축은 0만 있는 구간에서 눈금을 확대해
          # 아무 일도 없는 날의 그래프가 요동치는 것처럼 보인다.
          yAxis = { left = { min = 0 } }
        }
      },
      {
        type   = "metric"
        x      = 0
        y      = 21
        width  = 12
        height = 6
        properties = {
          title  = "오토스케일 — 밀린 手가 분석 대수를 움직인다"
          region = var.aws_region
          view   = "timeSeries"
          period = local.dash_period
          metrics = [
            concat(["show-gi", "AnalysisBacklogPlies"], local.dash_dims, [{ stat = "Maximum", label = "밀린 手" }]),
            # 대수는 ASG 그룹 지표로 본다. 정책이 실제로 움직이는 것은 서비스의
            # desired_count 인데 그 값은 Container Insights 를 켜야 지표로 나오고,
            # 여기서 묻는 것은 「EC2 가 붙었나」라서 아래쪽이 답이다(ec2.tf).
            ["AWS/AutoScaling", "GroupDesiredCapacity", "AutoScalingGroupName", aws_autoscaling_group.tier["analysis"].name,
            { stat = "Maximum", label = "분석 대수 — 목표", yAxis = "right" }],
            ["AWS/AutoScaling", "GroupInServiceInstances", "AutoScalingGroupName", aws_autoscaling_group.tier["analysis"].name,
            { stat = "Maximum", label = "분석 대수 — 붙은 것", yAxis = "right" }],
          ]
          # 임계선을 그린다. 이 선을 5분 넘긴 것이 스케일 아웃의 조건이라, 선이 없으면
          # 「왜 지금 올랐나」를 눈으로 못 맞춘다(alarms.tf 의 analysis_backlog).
          annotations = {
            horizontal = [{ label = "스케일 아웃 임계 100", value = 100 }]
          }
          # 두 선의 크기가 두 자릿수 다르다. 대수는 1~2 라 왼쪽 축에 두면 바닥에 붙는다.
          yAxis = { right = { min = 0, max = 3 } }
        }
      },
      {
        type   = "metric"
        x      = 12
        y      = 21
        width  = 12
        height = 6
        properties = {
          title  = "詰み 캐시 — 히트율(%)과 부른 횟수"
          region = var.aws_region
          view   = "timeSeries"
          period = local.dash_period
          metrics = [
            [{ expression = "100 * cached / mates", label = "히트율", id = "hit" }],
            concat(["show-gi", "MateSearches"], local.dash_dims, [{ stat = "Sum", id = "mates", visible = false }]),
            concat(["show-gi", "MateSearchesCached"], local.dash_dims, [{ stat = "Sum", id = "cached", visible = false }]),
            # 부른 총수를 같이 그린다. 히트율만 보면 조용해진 것과 안 쓰이는 것이 같은
            # 그림이 되고, 이 층은 대국이 없으면 호출 자체가 0이다.
            concat(["show-gi", "MateSearches"], local.dash_dims, [{ stat = "Sum", label = "부른 횟수", yAxis = "right" }]),
          ]
          yAxis = { left = { min = 0, max = 100 } }
        }
      },
    ]
  })
}

output "dashboard_url" {
  description = "부하 회차 중에 여는 화면"
  value       = "https://${var.aws_region}.console.aws.amazon.com/cloudwatch/home?region=${var.aws_region}#dashboards/dashboard/${aws_cloudwatch_dashboard.main.dashboard_name}"
}
