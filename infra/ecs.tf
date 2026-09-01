# ECS. docker compose + 손으로 쓴 배포 스크립트를 대신한다.
#
# 바꾼 이유는 비용이 아니라 직접 쓴 배포 글루가 전부 사라지기 때문이다: 배포 스크립트,
# 비밀을 셸로 내보내는 스크립트, compose 오버레이, 헬스체크 루프, ECR 로그인, 인증서 볼륨 —
# 전부 ECS·ALB의 기본 기능으로 대체된다. 내가 쓴 것만 내가 유지보수해야 한다.
#
# 용량은 EC2 스팟에서 온다(ec2.tf). 한때 Fargate였고, 그때 사라졌던 시작 템플릿과
# ASG가 그래서 돌아왔다 — 배포 스크립트는 안 돌아왔다. 그건 Fargate가 아니라 ECS가 맡던 일이다.
#
# 서비스가 둘이고 가르는 것은 SERVER_ROLE 이다.
#
#   show-gi            상호작용.  web + api. 대상 그룹 뒤에 있고 1대로 고정이다
#   show-gi-analysis   분석.      api 하나. 대상 그룹에 안 붙고 대수가 손잡이다
#
# 상호작용 태스크에 컨테이너 둘을 넣는다. host 모드에서는 두 컨테이너가 인스턴스의 네트워크
# 네임스페이스를 같이 쓰므로 web이 localhost:8080으로 api에 닿는다.
# 따로 떼면 타깃 그룹과 서비스가 두 벌이 되는데, 따로 스케일할 이유가 없다.

locals {
  api_image = "${aws_ecr_repository.app["api"].repository_url}:${var.image_tag}"
  web_image = "${aws_ecr_repository.app["web"].repository_url}:${var.image_tag}"

  # 비밀은 값이 아니라 경로로 들어간다. ECS 가 Parameter Store 에서 읽어 컨테이너에
  # 직접 넣는다 — 값이 디스크에 남지 않고 로그에도 안 찍힌다. deploy/env.sh 를 대체한 자리다.
  ssm_prefix = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter/show-gi/prod"

  # 티어 둘이 같은 이미지를 같은 손잡이로 돌린다. 갈리는 것은 SERVER_ROLE 과 비밀 목록
  # 둘뿐이라 나머지를 여기 모은다 — 갈라 적으면 한쪽만 고쳐서 두 티어가 다른 엔진 설정으로
  # 돌고, 그러면 회차의 값이 티어 간에 대조가 안 된다.
  #
  # ENGINE_CMD 를 여기 두지 않는다. 엔진 실행 경로는 이미지 내부 구조라 Terraform이 알 수
  # 없는 값이고, 태스크 정의의 environment 는 이미지의 ENV 를 덮어쓴다. 양쪽에 적어두면
  # 이미지를 바꿀 때 조용히 어긋난다 — 실제로 엔진을 やねうら王로 바꾼 배포에서 여기 남아
  # 있던 fairy-stockfish 가 이겨서, 배포는 성공했는데 대국만 안 되는 상태가 됐다.
  #
  # 운영 손잡이(ENGINE_POOL_SIZE·ENGINE_HASH_MB 등)는 여기 둬도 된다.
  # 이미지 안에 없는 값이라 덮어쓸 대상이 없다.
  #
  # 탐색 풀이 2다. 4로 올려 재 봤고 이득이 없어 되돌렸다(journal §110).
  #
  # 올릴 이유가 없는 것이 구조로 정해져 있다. 빌린 구간이 탐색 하나뿐이고(usi.Pool.Do
  # 가 Acquire·탐색·Release 를 붙여 두고 DB 는 그 밖이다) 그 탐색이 CPU 바운드라,
  # 슬롯을 코어보다 많이 줘도 같은 CPU 를 잘게 쪼개는 것뿐이다.
  #
  # 실측이 둘 다 그것을 말한다. 엔진 6판은 풀 대기 p95 가 8.52초에서 3.48초로
  # 내려가는 대신 탐색 p95 가 7~20초로 올라갔고 CPU 는 99% 에서 99% 였다. 대인전
  # 8판은 풀 대기가 매분 표본 100개에 p95 0.000 이었다 — 경합조차 없던 자원이다.
  #
  # 해시는 안 건드린다. 치환표 크기가 바뀌면 같은 국면의 탐색 결과가 달라져서
  # 앞 회차와 대조가 깨진다.
  #
  # ENVIRONMENT 가 지표의 손잡이다. 비어 있으면 서버가 EMF 를 안 내므로(cmd/api 의
  # startEmitter) 이 한 줄이 CloudWatch 커스텀 지표를 켜고 끈다. 값은 EMF 문서의
  # Environment 차원이 되므로, 이 값을 바꾸면 알람의 dimensions 도 같이 바꾼다
  # (infra/alarms.tf). 티어를 차원으로 안 올리는 이유는 journal §120 에 있다.
  api_env = [
    { name = "ENGINE_POOL_SIZE", value = "2" },
    { name = "ENGINE_MATE_POOL_SIZE", value = "1" },
    { name = "ENGINE_HASH_MB", value = "64" },
    { name = "ENVIRONMENT", value = "prod" },
    { name = "LOG_LEVEL", value = "info" },
  ]

  # 빈 목록 셋과 hostPort 를 적어 둔다. 안 적으면 AWS 가 채운 값과 우리 JSON 이 달라서
  # plan 이 매번 태스크 정의를 「바뀌었다」로 읽고 리비전을 하나씩 쌓는다 — 서비스가
  # task_definition 변경을 무시하므로 해는 없지만, 그러면 apply 가 무엇을 바꾸는지를
  # 손잡이 하나 고칠 때마다 다시 읽어야 한다.
  #
  # host 모드에서 hostPort 는 containerPort 와 같아야 한다. 여기 적는 값이 그것이고,
  # 한 인스턴스에 태스크가 하나뿐인 이유이기도 하다.
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
    value = "disabled" # 켜면 CloudWatch 요금이 붙는다. 필요해지면 켠다
  }
}

resource "aws_cloudwatch_log_group" "app" {
  name = "/ecs/show-gi"
  # 기본이 무기한이라 조용히 쌓인다. 14일인 것은 요청 로그와 EMF 가 같은 그룹으로
  # 들어오기 때문이다 — 지표는 CloudWatch 쪽에 15개월 남으므로 원본 로그를 길게 둘
  # 이유가 「그때 무슨 요청이었나」를 되짚는 것뿐이고, 그건 2주면 된다.
  retention_in_days = 14
}

# ─── 역할 ───────────────────────────────────────────────────

# 실행 역할: ECS 에이전트가 쓴다. 이미지를 받아오고 비밀을 읽어 컨테이너에 주입한다.
# 애플리케이션이 쓰는 역할이 아니다 — 그래서 앱이 뚫려도 이 권한은 노출되지 않는다.
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

# 비밀 주입. 이게 deploy/env.sh를 대체한다 — 값을 셸로 꺼내 파일에 쓰는 과정이 없고,
# 컨테이너 환경변수로 ECS가 직접 넣는다.
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

# 태스크 역할: 애플리케이션 자신의 권한. 지금은 AWS API를 쓰지 않으므로 비어 있고,
# ECS Exec(디버깅용 셸)에 필요한 것만 있다.
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
  # 옛 리비전을 지우지 않는다.
  #
  # 기본값은 새 리비전을 만들면서 예전 것을 deregister 하는데, 그러면 두 가지가 깨진다.
  # ① 롤백 경로가 사라진다 — 런북의 "이전 리비전으로 서비스를 돌린다"가
  #    INACTIVE 리비전을 가리키게 된다.
  # ② apply 직후 잠깐, 서비스가 가리키는 리비전이 INACTIVE가 된다. 그 사이 태스크가
  #    죽으면 ECS가 새 태스크를 못 띄운다.
  #
  # 배포 워크플로가 Terraform 밖에서 리비전을 계속 쌓으므로, 여기서 정리하려 해봐야
  # 자기가 만든 하나만 지운다. 얻는 것 없이 위 둘만 잃는다.
  skip_destroy = true

  family                   = "show-gi"
  requires_compatibilities = ["EC2"]

  # awsvpc 가 아니라 host 다. EC2 런치 타입에서 awsvpc 를 쓰면 태스크 ENI에
  # 공인 IP를 붙일 수 없고(그 옵션은 Fargate 전용이다), 그러면 밖으로 나가는 길이 NAT
  # 게이트웨이뿐이다 — 월 $40이라 인스턴스보다 비싸다. Google OAuth의 토큰 교환이
  # 서버에서 밖으로 나가는 호출이라 그 길이 막히면 로그인이 통째로 깨진다.
  #
  # bridge 는 안 된다. Caddy가 reverse_proxy localhost:8080 으로 api 를 부르는데
  # (apps/web/Caddyfile) bridge 는 컨테이너마다 네임스페이스를 갈라서 그 한 줄이 깨진다.
  # host 는 두 컨테이너가 인스턴스의 네임스페이스를 같이 쓰므로 그대로 닿는다.
  network_mode = "host"

  # 엔진 탐색이 CPU를 지속적으로 쓴다. 인스턴스가 t4g.small(2 vCPU / 2 GiB) 한 대라
  # 이 값이 그 안에 들어와야 태스크가 배치된다 — Fargate와 달리 남는 만큼 쓰는 것이
  # 아니라 인스턴스에서 실제로 예약된다.
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
      # 밀린 手를 아무도 안 집는다 — 부하가 오면 알람이 전용 대를 부르고, 그때는 둘이
      # 같이 집는다.
      #
      # 되돌릴 때 interactive 로 바꾼다. 그러면 이 박스의 엔진이 분석에 안 쓰여서
      # 티어를 가른 값(사람의 착수가 분석에 안 밀린다)이 돌아온다.
      environment = concat(local.api_env, [{ name = "SERVER_ROLE", value = "both" }])

      # 로그인과 세션이 이 티어에만 있다. 분석 티어는 사람을 안 받으므로 DATABASE_URL 뿐이다.
      secrets = [
        for k in [
          "DATABASE_URL",
          "SESSION_SECRET",
          "GOOGLE_CLIENT_ID",
          "GOOGLE_CLIENT_SECRET",
        ] : { name = k, valueFrom = "${local.ssm_prefix}/${k}" }
      ]
    }),
  ])
}

# 분석 티어. 사람을 안 받으므로 web 컨테이너가 없다.
#
# 컨테이너가 하나인 것이 이 티어의 정의다. Caddy 는 사람의 요청을 api 로 넘기는 자리인데
# 여기는 대상 그룹 뒤에 없어서 넘길 요청이 없고, 띄우면 80 을 잡아 아무도 안 보는 서버가
# 인스턴스마다 하나씩 는다.
#
# /healthz 를 밖에서 못 물어본다. 대신 이 티어가 죽으면 AnalysisBacklogPlies 가
# 부푸는 것으로 보인다(alarms.tf) — 게이지를 티어마다 올리는 이유가 그것이다(journal §119).
resource "aws_ecs_task_definition" "analysis" {
  # 옛 리비전을 지우지 않는다. 이유는 상호작용 쪽과 같다(위).
  skip_destroy = true

  family                   = "show-gi-analysis"
  requires_compatibilities = ["EC2"]

  # host 인 것은 상호작용 쪽과 같은 이유다(위). 포트가 겹치므로 이 모드가 곧
  # 「한 인스턴스에 태스크 하나」의 강제이기도 하다 — 늘리는 것이 태스크가 아니라 EC2 다.
  network_mode = "host"

  cpu    = var.task_cpu
  memory = var.task_memory

  execution_role_arn = aws_iam_role.task_execution.arn
  task_role_arn      = aws_iam_role.task.arn

  container_definitions = jsonencode([
    merge(local.api_container, {
      # 큐를 집는 티어다. /healthz·/metrics 말고는 전부 503 이다(cmd/api 의 analysisRole).
      environment = concat(local.api_env, [{ name = "SERVER_ROLE", value = "analysis" }])

      # DATABASE_URL 하나다. 로그인도 세션도 이 티어에 없으므로 나머지 셋은 줄 이유가 없고,
      # 안 주는 만큼 이 박스가 뚫렸을 때 나가는 것이 적다.
      secrets = [
        { name = "DATABASE_URL", valueFrom = "${local.ssm_prefix}/DATABASE_URL" },
      ]
    }),
  ])
}

# ─── 용량 공급자 ────────────────────────────────────────────

# 티어마다 하나다. 하는 일은 「이 서비스의 태스크를 저 ASG 에만 얹는다」이고,
# 그것이 없으면 ECS 가 클러스터의 아무 인스턴스에나 얹어서 갈라 둔 것이 섞인다.
#
# 상호작용 쪽은 managed_scaling 이 꺼져 있다. 켜 봐야 min=max=1 이라 움직일 값이 없고,
# 꺼 두면 그 ASG 의 대수를 terraform 이 계속 든다.
resource "aws_ecs_capacity_provider" "interactive" {
  name = "show-gi-interactive"

  auto_scaling_group_provider {
    auto_scaling_group_arn = aws_autoscaling_group.tier["interactive"].arn

    managed_scaling {
      status = "DISABLED"
    }
  }
}

# 분석 쪽은 켠다. 배치 못 한 태스크를 보고 ASG 의 desired 를 올리는 것이 이 블록이고,
# 그래서 늘리는 손잡이가 서비스의 desired_count 하나가 된다 — 그 값을 스케일 정책이
# 든다(autoscale.tf).
resource "aws_ecs_capacity_provider" "analysis" {
  name = "show-gi-analysis"

  auto_scaling_group_provider {
    auto_scaling_group_arn = aws_autoscaling_group.tier["analysis"].arn

    managed_scaling {
      status = "ENABLED"

      # 100 은 「인스턴스를 남기지 않는다」다. 여유분을 두면 늘 한 대가 놀고, 이 티어는
      # 사람이 안 기다리므로 그 값을 낼 이유가 없다 — 밀린 手는 늦게 재도 사람이 안 본다.
      target_capacity = 100

      # 한 번에 한 대씩 움직인다. 대당 2 vCPU 인데 회차가 재는 것이 「한 대 늘리면
      # 처리량이 얼마나 느나」라서, 계단이 두 대씩이면 그 기울기를 못 읽는다.
      minimum_scaling_step_size = 1
      maximum_scaling_step_size = 1

      # 새 대가 뜨고 ECS 에 등록되어 태스크를 받기까지가 실측으로 4분쯤이다(journal §109).
      # 그 전에 다음 계단을 밟으면 아직 일을 시작 안 한 대를 「모자라다」로 읽는다.
      instance_warmup_period = 240
    }

    # 종료 보호를 안 켠다. 스케일 인이 일하는 중인 대를 가져가도 그 手의 행이 표에 남고
    # 임차가 풀리면 다른 대가 다시 집는다(journal §118) — 잃는 것이 판이 아니라 시간이다.
    managed_termination_protection = "DISABLED"
  }
}

# 클러스터에 등록해야 서비스가 이름으로 고를 수 있다. 기본 전략은 안 둔다 —
# 두 서비스가 각자 자기 것을 명시하므로, 기본값이 있으면 잘못 적었을 때 조용히 붙는다.
#
# 여기서 이름을 빼는 것으로는 공급자가 안 지워진다. 쓰는 서비스가 있는 동안 삭제가
# 거절되므로, 되돌릴 때는 서비스를 먼저 옮긴다.
resource "aws_ecs_cluster_capacity_providers" "main" {
  cluster_name = aws_ecs_cluster.main.name

  capacity_providers = [
    aws_ecs_capacity_provider.interactive.name,
    aws_ecs_capacity_provider.analysis.name,
  ]
}

# ─── 서비스 ─────────────────────────────────────────────────

# 인스턴스의 보안그룹이기도 하다. host 네트워크 모드라 태스크가 인스턴스의 ENI로
# 나가므로 규칙이 하나면 되고, 시작 템플릿이 이것을 그대로 참조한다(ec2.tf).
# 이름을 안 바꾼 것은 RDS와 ALB의 규칙이 이 이름을 가리키고 있어서다.
resource "aws_security_group" "task" {
  name        = "show-gi-task"
  description = "show-gi: ALB to task"
  vpc_id      = data.aws_vpc.default.id
}

# ALB에서만 들어온다. 인스턴스는 공인 IP를 갖지만(ECR·Parameter Store·Google OAuth의
# egress 때문에) 인바운드는 ALB 보안그룹으로 잠겨 있다.
#
# api의 8080은 안 연다. Caddy가 localhost:8080 으로 부르므로 밖에서 닿을 필요가 없고,
# host 모드에서 그 포트가 인스턴스에 그대로 노출되므로 여는 순간 엔진이 공개된다.
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

  # launch_type 이 아니라 용량 공급자로 얹는다. launch_type = "EC2" 는 클러스터에 등록된
  # 아무 인스턴스나 고르므로, 분석 대가 생긴 순간 이 태스크가 그쪽에 앉을 수 있다 —
  # 그러면 분석 대가 하나 줄고 상호작용 대가 빈 채로 요금만 나간다.
  capacity_provider_strategy {
    capacity_provider = aws_ecs_capacity_provider.interactive.name
    weight            = 1
  }

  # 컨테이너에 셸로 들어갈 수 있게 한다. SSH도, 배스천도 필요 없다:
  #   aws ecs execute-command --cluster show-gi --task <id> --container api --interactive --command /bin/sh
  enable_execute_command = true

  # network_configuration 이 없다. host 네트워크 모드에서는 태스크가 자기 ENI를
  # 갖지 않고 인스턴스의 것을 쓰므로, 서브넷도 보안그룹도 시작 템플릿이 정한다(ec2.tf).

  load_balancer {
    target_group_arn = aws_lb_target_group.web.arn
    container_name   = "web"
    container_port   = 80
  }

  # 옛 태스크를 먼저 내리고 새 것을 띄운다. 원래는 반대였는데(100/200) 인스턴스가
  # 태스크 하나에 맞춰진 한 대뿐이라 두 개가 동시에 못 올라간다 — 그대로 두면 새 태스크가
  # 영원히 배치되지 않고 배포가 서킷 브레이커에 걸려 되돌아간다.
  #
  # 값은 배포마다 1~2분 내려간다는 뜻이다. 쓰는 사람이 한 명이라 받아들이는 쪽이고,
  # 무중단을 되찾으려면 인스턴스를 두 대로 늘리거나(비용 2배) 태스크를 반으로 줄여야 한다.
  deployment_minimum_healthy_percent = 0
  deployment_maximum_percent         = 100

  deployment_circuit_breaker {
    enable   = true
    rollback = true # 새 버전이 안 뜨면 자동으로 되돌린다
  }

  # 시작 직후에는 헬스체크 실패를 봐준다. 엔진 프로세스가 뜨는 데 시간이 걸리고,
  # 스팟이 회수된 뒤 새 인스턴스에서는 이미지를 처음 받는 시간까지 여기 들어온다 —
  # 60초로는 서킷 브레이커가 먼저 걸려 배포가 되돌아간다.
  health_check_grace_period_seconds = 120

  lifecycle {
    # 배포는 CI가 새 리비전을 등록해서 한다. terraform이 그걸 되돌리면
    # apply 한 번에 옛 이미지로 돌아간다. desired_count는 terraform 소유라
    # CLI로 수동 스케일링해도 다음 apply가 코드 값으로 되돌린다
    ignore_changes = [task_definition]
  }

  # ASG가 먼저다. 클러스터에 등록된 인스턴스가 없으면 서비스가 태스크를 못 띄우고,
  # 첫 apply 에서 그 상태로 몇 분을 기다린다. 용량 공급자도 클러스터에 붙은 뒤여야
  # 이름으로 고를 수 있다.
  depends_on = [
    aws_lb_listener.https,
    aws_autoscaling_group.tier,
    aws_ecs_cluster_capacity_providers.main,
  ]
}

# 분석 티어. 대상 그룹에 안 붙는다 — 여기에 사람이 오면 방이 이 프로세스의 메모리에 서서
# 짝이 안 맞고(journal §98) 로그에 아무것도 안 남는다. 배포 워크플로가 /healthz 의 role 을
# 열 번 물어 그것을 막는다(.github/workflows/images.yml).
#
# 늘리는 손잡이가 desired_count 하나다. 용량 공급자가 미배치 태스크를 보고 EC2 를 따라
# 올린다(위 analysis) — 태스크를 늘리는 것이 곧 대를 늘리는 것이다.
#
# 그 값의 주인이 terraform 이 아니다. 밀린 手가 정하고(autoscale.tf), 여기 적힌 1은
# 서비스를 처음 만들 때만 쓰인다.
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

  # 옛 태스크를 먼저 내리고 새 것을 띄운다. 상호작용 쪽과 같은 값이고 이유도 같다 —
  # 인스턴스가 태스크 하나에 맞춰져 있어 두 개가 동시에 못 올라간다.
  #
  # 내려가 있는 동안 밀린 手가 쌓이지만 행이 표에 남으므로 잃지 않는다(journal §118).
  deployment_minimum_healthy_percent = 0
  deployment_maximum_percent         = 100

  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }

  # health_check_grace_period_seconds 가 없다. 그 값은 로드밸런서 헬스체크를 봐주는
  # 유예라서 대상 그룹이 없는 서비스에는 못 준다 — 적으면 ECS 가 거절한다.

  lifecycle {
    # 배포는 CI가 새 리비전을 등록해서 한다(상호작용 쪽과 같은 판단).
    #
    # desired_count 를 무시하는 것은 상호작용 쪽과 갈리는 자리다. 스케일 정책이 그 값을
    # 드는데(autoscale.tf) terraform 도 들면 스케일 아웃이 다음 apply 에 취소된다 —
    # 회차가 손잡이 하나만 고쳐 apply 하는 자리라 그것이 조용히 회차를 무효로 만든다.
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
