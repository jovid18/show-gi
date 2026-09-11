# ECS. 서비스가 둘이고 가르는 것은 SERVER_ROLE 이다. 용량은 EC2 스팟에서 온다(ec2.tf).
#
#   show-gi            상호작용. web + api. 대상 그룹 뒤에 있고 1대로 고정이다
#   show-gi-analysis   분석.     api 하나. 대상 그룹에 붙지 않고 대수가 손잡이다
#
# 상호작용 태스크에 컨테이너 둘을 넣는다. host 모드에서 둘이 인스턴스의 네트워크
# 네임스페이스를 같이 쓰므로 web 이 localhost:8080 으로 api 에 닿는다. 따로 떼면 타깃
# 그룹과 서비스가 두 벌이 되는데, 따로 스케일할 이유가 없다.

locals {
  api_image = "${aws_ecr_repository.app["api"].repository_url}:${var.image_tag}"
  web_image = "${aws_ecr_repository.app["web"].repository_url}:${var.image_tag}"

  # 비밀은 값 대신 경로로 들어간다. ECS 가 Parameter Store 에서 읽어 컨테이너에 넣으므로
  # 값이 디스크에도 로그에도 남지 않는다.
  ssm_prefix = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter/show-gi/prod"

  # 티어 둘이 같은 이미지를 같은 손잡이로 돌린다. 갈리는 것은 SERVER_ROLE 과 비밀 목록
  # 둘뿐이라 나머지를 여기 모은다. 따로 적으면 한쪽만 고쳐서 두 티어가 다른 엔진 설정으로
  # 돌고, 그러면 시험의 값을 티어 간에 대조할 수 없다.
  #
  # ENGINE_CMD 는 여기 두지 않는다. 태스크 정의의 environment 가 이미지의 ENV 를 덮어써서
  # 이미지를 바꿀 때 경고 없이 어긋난다(journal §11). 운영 손잡이는 이미지 안에 없는 값이라
  # 덮어쓸 대상이 없다.
  #
  # 탐색 풀이 2다. 4로 올려 재 봤고 대기가 탐색 시간으로 옮겨갔을 뿐이라 되돌렸다
  # (journal §110).
  #
  # 해시는 건드리지 않는다. 치환표 크기가 바뀌면 같은 국면의 탐색 결과가 달라져서
  # 앞 시험과 대조가 깨진다.
  #
  # ENVIRONMENT 가 지표의 손잡이다. 비어 있으면 서버가 EMF 를 내보내지 않는다(cmd/api 의
  # startEmitter). 이 값이 EMF 문서의 dimensions 로 나가므로 바꾸면 알람 쪽도 같이 바꾼다
  # (alarms.tf). 티어를 dimensions 로 올리지 않는 이유는 journal §120 에 있다.
  api_env = [
    { name = "ENGINE_POOL_SIZE", value = "2" },
    { name = "ENGINE_MATE_POOL_SIZE", value = "1" },
    { name = "ENGINE_HASH_MB", value = "64" },
    { name = "ENVIRONMENT", value = "prod" },
    { name = "LOG_LEVEL", value = "info" },
  ]

  # 빈 목록 셋과 hostPort 를 적어 둔다. 적지 않으면 AWS 가 채운 값과 우리 JSON 이 달라서
  # plan 이 매번 태스크 정의를 「바뀌었다」로 읽고 리비전을 하나씩 쌓는다.
  #
  # host 모드에서 hostPort 는 containerPort 와 같아야 한다. 포트가 겹치므로 한 인스턴스에
  # api 태스크가 하나뿐이고, 늘리는 단위가 태스크 대신 EC2 다.
  api_container = {
    name           = "api"
    image          = local.api_image
    essential      = true
    portMappings   = [{ containerPort = 8080, hostPort = 8080, protocol = "tcp" }]
    mountPoints    = []
    systemControls = []
    volumesFrom    = []

    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = aws_cloudwatch_log_group.app.name
        "awslogs-region"        = var.aws_region
        "awslogs-stream-prefix" = "api"
      }
    }
  }
}

resource "aws_ecs_cluster" "main" {
  name = "show-gi"

  setting {
    name  = "containerInsights"
    value = "disabled" # 켜면 CloudWatch 요금이 붙는다
  }
}

resource "aws_cloudwatch_log_group" "app" {
  name = "/ecs/show-gi"
  # 기본이 무기한이라 눈에 띄지 않게 쌓인다. 지표는 CloudWatch 쪽에 15개월 남으므로,
  # 원본 로그는 「그때 무슨 요청이었나」를 되짚는 동안만 있으면 된다.
  retention_in_days = 14
}

# ─── 역할 ───────────────────────────────────────────────────

# 실행 역할: ECS 에이전트가 쓴다. 이미지를 받아오고 비밀을 읽어 컨테이너에 주입한다.
# 애플리케이션은 이 역할을 쓰지 않으므로 앱이 뚫려도 이 권한은 노출되지 않는다.
resource "aws_iam_role" "task_execution" {
  name = "show-gi-task-execution"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "task_execution" {
  role       = aws_iam_role.task_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

resource "aws_iam_role_policy" "task_execution_secrets" {
  name = "read-parameters"
  role = aws_iam_role.task_execution.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["ssm:GetParameters"]
        Resource = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter/show-gi/*"
      },
      {
        Effect    = "Allow"
        Action    = "kms:Decrypt"
        Resource  = "*"
        Condition = { StringEquals = { "kms:ViaService" = "ssm.${var.aws_region}.amazonaws.com" } }
      },
    ]
  })
}

# 태스크 역할: 애플리케이션 자신의 권한. AWS API 를 쓰지 않으므로 ECS Exec 통로만 있다.
resource "aws_iam_role" "task" {
  name = "show-gi-task"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy" "task_exec_channel" {
  name = "ecs-exec"
  role = aws_iam_role.task.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "ssmmessages:CreateControlChannel",
        "ssmmessages:CreateDataChannel",
        "ssmmessages:OpenControlChannel",
        "ssmmessages:OpenDataChannel",
      ]
      Resource = "*"
    }]
  })
}

# ─── 태스크 ─────────────────────────────────────────────────

resource "aws_ecs_task_definition" "app" {
  # 옛 리비전을 지우지 않는다. 기본값은 새 리비전을 만들면서 예전 것을 deregister 하는데,
  # 그러면 런북의 롤백 경로가 INACTIVE 리비전을 가리키게 된다. apply 직후 잠깐은 서비스가
  # 가리키는 리비전이 INACTIVE 라, 그 사이 태스크가 죽으면 ECS 가 새로 띄우지 못한다.
  skip_destroy = true

  family                   = "show-gi"
  requires_compatibilities = ["EC2"]

  # awsvpc 대신 host 다. EC2 런치 타입의 awsvpc 는 태스크 ENI 에 공인 IP 를 붙일 수 없어서
  # 밖으로 나가는 길이 NAT 게이트웨이뿐이고(월 $40), Google OAuth 토큰 교환이 그 길로
  # 나가므로 막히면 로그인 전체가 깨진다.
  #
  # bridge 도 쓸 수 없다. Caddy 가 reverse_proxy localhost:8080 으로 api 를 부르는데
  # (apps/web/Caddyfile) bridge 는 컨테이너마다 네임스페이스를 나눈다.
  network_mode = "host"

  # 인스턴스에서 실제로 예약되는 값이다. 인스턴스 크기를 넘으면 태스크가 배치되지 않는다
  # (variables.tf 의 task_cpu · task_memory).
  cpu    = var.task_cpu
  memory = var.task_memory

  execution_role_arn = aws_iam_role.task_execution.arn
  task_role_arn      = aws_iam_role.task.arn

  container_definitions = jsonencode([
    {
      name           = "web"
      image          = local.web_image
      essential      = true
      portMappings   = [{ containerPort = 80, hostPort = 80, protocol = "tcp" }]
      environment    = []
      mountPoints    = []
      systemControls = []
      volumesFrom    = []
      logConfiguration = {
        logDriver = "awslogs"
        options = {
          "awslogs-group"         = aws_cloudwatch_log_group.app.name
          "awslogs-region"        = var.aws_region
          "awslogs-stream-prefix" = "web"
        }
      }
      dependsOn = [{ containerName = "api", condition = "START" }]
    },
    merge(local.api_container, {
      # 사람을 받는 티어다. both 라 큐를 세우기도 하고 집기도 한다.
      #
      # 절약 모드의 자리다(journal §125). 분석 티어가 평시에 0대라 이 대가 겸하지 않으면
      # 밀린 手를 누구도 집지 않는다. 되돌릴 때 interactive 로 바꾼다. 그러면 사람의 착수가
      # 분석에 밀리지 않는, 티어를 가른 값이 돌아온다.
      environment = concat(local.api_env, [{ name = "SERVER_ROLE", value = "both" }])

      # 로그인과 세션이 이 티어에만 있다. OPENAI_API_KEY 도 마찬가지다. 기보를 가져오는
      # 요청이 사람에게서 오고(server/kifu_import.go) 분석 워커는 그 계층을 지나지 않는다.
      # 없어도 뜨고, 그때는 결정적 파서로 읽히는 기보만 들어온다(internal/kifunorm).
      secrets = [
        for k in [
          "DATABASE_URL",
          "SESSION_SECRET",
          "GOOGLE_CLIENT_ID",
          "GOOGLE_CLIENT_SECRET",
          "OPENAI_API_KEY",
        ] : { name = k, valueFrom = "${local.ssm_prefix}/${k}" }
      ]
    }),
  ])
}

# 분석 티어. 사람을 받지 않으므로 web 컨테이너가 없다. 대상 그룹 뒤에 없어 Caddy 가 넘길
# 요청이 없고, 띄우면 80 을 잡아 누구도 보지 않는 서버가 인스턴스마다 하나씩 는다.
#
# /healthz 를 밖에서 물어볼 수 없다. 대신 이 티어가 죽으면 AnalysisBacklogPlies 가 부푸는
# 것으로 보인다(alarms.tf, journal §119).
resource "aws_ecs_task_definition" "analysis" {
  # 옛 리비전을 지우지 않는다. 상호작용 쪽과 같은 판단이다(위).
  skip_destroy = true

  family                   = "show-gi-analysis"
  requires_compatibilities = ["EC2"]

  # host 인 것도 상호작용 쪽과 같은 판단이다(위).
  network_mode = "host"

  cpu    = var.task_cpu
  memory = var.task_memory

  execution_role_arn = aws_iam_role.task_execution.arn
  task_role_arn      = aws_iam_role.task.arn

  container_definitions = jsonencode([
    merge(local.api_container, {
      # 큐를 집는 티어다. /healthz·/metrics 말고는 전부 503 이다(cmd/api 의 analysisRole).
      environment = concat(local.api_env, [{ name = "SERVER_ROLE", value = "analysis" }])

      # DATABASE_URL 하나다. 로그인도 세션도 이 티어에 없고, 주지 않는 만큼 이 박스가
      # 뚫렸을 때 나가는 것이 적다.
      secrets = [
        { name = "DATABASE_URL", valueFrom = "${local.ssm_prefix}/DATABASE_URL" },
      ]
    }),
  ])
}

# ─── 용량 공급자 ────────────────────────────────────────────

# 티어마다 하나다. 없으면 ECS 가 클러스터의 아무 인스턴스에나 태스크를 얹어서 따로 둔
# 것이 섞인다.
#
# 상호작용 쪽은 managed_scaling 이 꺼져 있다. min=max=1 이라 움직일 값이 없고, 꺼 두면
# 그 ASG 의 대수를 terraform 이 계속 정한다.
resource "aws_ecs_capacity_provider" "interactive" {
  name = "show-gi-interactive"

  auto_scaling_group_provider {
    auto_scaling_group_arn = aws_autoscaling_group.tier["interactive"].arn

    managed_scaling {
      status = "DISABLED"
    }
  }
}

# 분석 쪽은 켠다. 배치하지 못한 태스크를 보고 ASG 의 desired 를 올리므로, 늘리는 손잡이가
# 서비스의 desired_count 하나가 된다. 그 값은 스케일 정책이 정한다(autoscale.tf).
resource "aws_ecs_capacity_provider" "analysis" {
  name = "show-gi-analysis"

  auto_scaling_group_provider {
    auto_scaling_group_arn = aws_autoscaling_group.tier["analysis"].arn

    managed_scaling {
      status = "ENABLED"

      # 100 은 「인스턴스를 남기지 않는다」다. 이 티어는 사람이 기다리지 않으므로 늘 한 대가
      # 노는 값을 낼 이유가 없다.
      target_capacity = 100

      # 한 번에 한 대씩 움직인다. 시험이 재는 것이 「한 대 늘리면 처리량이 얼마나 느나」라서
      # 계단이 두 대씩이면 그 기울기를 읽을 수 없다.
      minimum_scaling_step_size = 1
      maximum_scaling_step_size = 1

      # 새 대가 뜨고 태스크를 받기까지가 실측으로 4분쯤이다(journal §109). 그 전에 다음
      # 계단을 밟으면 아직 일을 시작하지 않은 대를 「모자라다」로 읽는다.
      instance_warmup_period = 240
    }

    # 종료 보호를 켜지 않는다. 스케일 인이 일하는 중인 대를 가져가도 그 手의 행이 표에 남고
    # 임차가 풀리면 다른 대가 다시 집는다(journal §118).
    managed_termination_protection = "DISABLED"

    # AWS 기본값이라 코드에 보이지 않던 값이다. 켜져 있으면 ECS 가 ASG 에 드레이닝 훅을
    # 심는데, 실측으로 그 훅이 완료되지 않아 스케일 인마다 HeartbeatTimeout 3600초를 다
    # 낸다(journal §134).
    #
    # 끄지 않은 것은 대안의 값을 재 보지 않았기 때문이다. 끄면 드레이닝이 없어지고, 훅의
    # 타임아웃만 줄이면 ECS 가 뒤늦게 완료를 보낼 때 무슨 일이 나는지 모른다.
    managed_draining = "ENABLED"
  }
}

# 클러스터에 등록해야 서비스가 이름으로 고를 수 있다. 기본 전략은 두지 않는다. 두 서비스가
# 각자 자기 것을 명시하므로, 기본값이 있으면 잘못 적었을 때 경고 없이 붙는다.
#
# 여기서 이름을 빼도 공급자는 지워지지 않는다. 쓰는 서비스가 있는 동안 삭제가 거절되므로
# 되돌릴 때는 서비스를 먼저 옮긴다.
resource "aws_ecs_cluster_capacity_providers" "main" {
  cluster_name = aws_ecs_cluster.main.name

  capacity_providers = [
    aws_ecs_capacity_provider.interactive.name,
    aws_ecs_capacity_provider.analysis.name,
  ]
}

# ─── 서비스 ─────────────────────────────────────────────────

# 인스턴스의 보안그룹이기도 하다. host 모드라 태스크가 인스턴스의 ENI 로 나가므로 규칙이
# 하나면 되고, 시작 템플릿이 이것을 그대로 참조한다(ec2.tf).
resource "aws_security_group" "task" {
  name        = "show-gi-task"
  description = "show-gi: ALB to task"
  vpc_id      = data.aws_vpc.default.id
}

# ALB 에서만 들어온다. 인스턴스는 공인 IP 를 갖지만(ECR · Parameter Store · Google OAuth
# egress) 인바운드는 ALB 보안그룹으로 잠겨 있다.
#
# api 의 8080 은 열지 않는다. host 모드에서 그 포트가 인스턴스에 그대로 노출되므로 여는
# 순간 엔진이 공개된다.
resource "aws_vpc_security_group_ingress_rule" "task_from_alb" {
  security_group_id            = aws_security_group.task.id
  referenced_security_group_id = aws_security_group.alb.id
  from_port                    = 80
  to_port                      = 80
  ip_protocol                  = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "task_all" {
  security_group_id = aws_security_group.task.id
  description       = "ECR, Parameter Store, Google OAuth"
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

resource "aws_ecs_service" "app" {
  name            = "show-gi"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.app.arn
  desired_count   = 1

  # launch_type 대신 용량 공급자로 얹는다. launch_type = "EC2" 는 클러스터의 아무 인스턴스나
  # 고르므로 이 태스크가 분석 대에 앉고, 그러면 상호작용 대가 빈 채로 요금만 나간다.
  capacity_provider_strategy {
    capacity_provider = aws_ecs_capacity_provider.interactive.name
    weight            = 1
  }

  # 컨테이너에 셸로 들어갈 수 있게 한다. 명령은 outputs.tf 의 shell 에 있다.
  enable_execute_command = true

  # network_configuration 이 없다. host 모드에서는 태스크가 인스턴스의 ENI 를 쓰므로
  # 서브넷도 보안그룹도 시작 템플릿이 정한다(ec2.tf).

  load_balancer {
    target_group_arn = aws_lb_target_group.web.arn
    container_name   = "web"
    container_port   = 80
  }

  # 옛 태스크를 먼저 내리고 새 것을 띄운다. 인스턴스가 태스크 하나에 맞춰진 한 대뿐이라
  # 두 개가 동시에 올라가지 못하고, 100/200 으로 두면 새 태스크가 영원히 배치되지 않아
  # 배포가 서킷 브레이커에 걸려 되돌아간다.
  #
  # 대가는 배포마다 1~2분 내려가는 것이다. 무중단을 되찾으려면 인스턴스를 두 대로 늘린다.
  deployment_minimum_healthy_percent = 0
  deployment_maximum_percent         = 100

  deployment_circuit_breaker {
    enable   = true
    rollback = true # 새 버전이 뜨지 않으면 자동으로 되돌린다
  }

  # 시작 직후에는 헬스체크 실패를 봐준다. 엔진 프로세스 기동과, 스팟 회수 뒤 새 인스턴스가
  # 이미지를 처음 받는 시간이 여기 들어온다. 60초로는 서킷 브레이커가 먼저 걸린다.
  health_check_grace_period_seconds = 120

  lifecycle {
    # 배포는 CI 가 새 리비전을 등록해서 한다. terraform 이 그걸 되돌리면 apply 한 번에
    # 옛 이미지로 돌아간다
    ignore_changes = [task_definition]
  }

  # ASG 가 먼저다. 등록된 인스턴스가 없으면 서비스가 첫 apply 에서 태스크를 띄우지 못한 채
  # 몇 분을 기다린다. 용량 공급자도 클러스터에 붙은 뒤여야 이름으로 고를 수 있다.
  depends_on = [
    aws_lb_listener.https,
    aws_autoscaling_group.tier,
    aws_ecs_cluster_capacity_providers.main,
  ]
}

# 분석 티어. 대상 그룹에 붙지 않는다. 여기에 사람이 오면 방이 이 프로세스의 메모리에 있어서
# 짝이 맞지 않고(journal §98) 로그에 아무것도 남지 않는다. 배포 워크플로가 /healthz 의
# role 을 열 번 물어 그것을 막는다(.github/workflows/images.yml).
#
# 늘리는 손잡이가 desired_count 하나다. 용량 공급자가 미배치 태스크를 보고 EC2 를 따라
# 올린다(위 analysis). 여기 적힌 1은 서비스를 처음 만들 때만 쓰이고, 그 뒤로는 밀린 手가
# 정한다(autoscale.tf).
resource "aws_ecs_service" "analysis" {
  name            = "show-gi-analysis"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.analysis.arn
  desired_count   = 1

  capacity_provider_strategy {
    capacity_provider = aws_ecs_capacity_provider.analysis.name
    weight            = 1
  }

  enable_execute_command = true

  # 옛 태스크를 먼저 내리고 새 것을 띄운다. 상호작용 쪽과 같은 판단이다(위). 내려가 있는
  # 동안 밀린 手가 쌓이지만 행이 표에 남으므로 잃지 않는다(journal §118).
  deployment_minimum_healthy_percent = 0
  deployment_maximum_percent         = 100

  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }

  # health_check_grace_period_seconds 가 없다. 로드밸런서 헬스체크를 봐주는 유예라서
  # 대상 그룹이 없는 서비스에는 줄 수 없고, 적으면 ECS 가 거절한다.

  lifecycle {
    # 배포는 CI 가 새 리비전을 등록해서 한다(상호작용 쪽과 같은 판단).
    #
    # desired_count 를 무시하는 것이 상호작용 쪽과 갈리는 자리다. 스케일 정책이 그 값을
    # 정하는데(autoscale.tf) terraform 도 정하면 스케일 아웃이 다음 apply 에 취소된다.
    ignore_changes = [task_definition, desired_count]
  }

  depends_on = [
    aws_autoscaling_group.tier,
    aws_ecs_cluster_capacity_providers.main,
  ]
}

output "cluster" {
  value = aws_ecs_cluster.main.name
}

output "service" {
  value = aws_ecs_service.app.name
}

output "analysis_service" {
  value = aws_ecs_service.analysis.name
}
