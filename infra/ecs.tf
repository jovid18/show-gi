# ECS. docker compose + 손으로 쓴 배포 스크립트를 대신한다.
#
# 바꾼 이유는 비용이 아니라 **직접 쓴 배포 글루가 전부 사라지기 때문**이다: 배포 스크립트,
# 비밀을 셸로 내보내는 스크립트, compose 오버레이, 헬스체크 루프, ECR 로그인, 인증서 볼륨 —
# 전부 ECS·ALB의 기본 기능으로 대체된다. 내가 쓴 것만 내가 유지보수해야 한다.
#
# **용량은 EC2 스팟 한 대에서 온다**(ec2.tf). 한때 Fargate였고, 그때 사라졌던 시작 템플릿과
# ASG가 그래서 돌아왔다 — 배포 스크립트는 안 돌아왔다. 그건 Fargate가 아니라 ECS가 맡던 일이다.
#
# 태스크 하나에 컨테이너 둘을 넣는다. `host` 모드에서는 두 컨테이너가 인스턴스의 네트워크
# 네임스페이스를 같이 쓰므로 web이 localhost:8080으로 api에 닿는다.
# 따로 떼면 타깃 그룹과 서비스가 두 벌이 되는데, 따로 스케일할 이유가 없다.

locals {
  api_image = "${aws_ecr_repository.app["api"].repository_url}:${var.image_tag}"
  web_image = "${aws_ecr_repository.app["web"].repository_url}:${var.image_tag}"
}

resource "aws_ecs_cluster" "main" {
  name = "show-gi"

  setting {
    name  = "containerInsights"
    value = "disabled" # 켜면 CloudWatch 요금이 붙는다. 필요해지면 켠다
  }
}

resource "aws_cloudwatch_log_group" "app" {
  name              = "/ecs/show-gi"
  retention_in_days = 14 # 대회 기간에 필요한 만큼만. 기본은 무기한이라 조용히 쌓인다
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
  # ① **롤백 경로가 사라진다** — 런북의 "이전 리비전으로 서비스를 돌린다"가
  #    INACTIVE 리비전을 가리키게 된다.
  # ② apply 직후 잠깐, 서비스가 가리키는 리비전이 INACTIVE가 된다. 그 사이 태스크가
  #    죽으면 ECS가 새 태스크를 못 띄운다.
  #
  # 배포 워크플로가 Terraform 밖에서 리비전을 계속 쌓으므로, 여기서 정리하려 해봐야
  # 자기가 만든 하나만 지운다. 얻는 것 없이 위 둘만 잃는다.
  skip_destroy = true

  family                   = "show-gi"
  requires_compatibilities = ["EC2"]

  # **`awsvpc` 가 아니라 `host` 다.** EC2 런치 타입에서 `awsvpc` 를 쓰면 태스크 ENI에
  # 공인 IP를 붙일 수 없고(그 옵션은 Fargate 전용이다), 그러면 밖으로 나가는 길이 NAT
  # 게이트웨이뿐이다 — 월 $40이라 인스턴스보다 비싸다. Google OAuth의 토큰 교환이
  # 서버에서 밖으로 나가는 호출이라 그 길이 막히면 로그인이 통째로 깨진다.
  #
  # `bridge` 는 안 된다. Caddy가 `reverse_proxy localhost:8080` 으로 api 를 부르는데
  # (apps/web/Caddyfile) bridge 는 컨테이너마다 네임스페이스를 갈라서 그 한 줄이 깨진다.
  # `host` 는 두 컨테이너가 인스턴스의 네임스페이스를 같이 쓰므로 그대로 닿는다.
  network_mode = "host"

  # 엔진 탐색이 CPU를 지속적으로 쓴다. 인스턴스가 t4g.small(2 vCPU / 2 GiB) 한 대라
  # **이 값이 그 안에 들어와야 태스크가 배치된다** — Fargate와 달리 남는 만큼 쓰는 것이
  # 아니라 인스턴스에서 실제로 예약된다.
  cpu    = var.task_cpu
  memory = var.task_memory

  execution_role_arn = aws_iam_role.task_execution.arn
  task_role_arn      = aws_iam_role.task.arn

  container_definitions = jsonencode([
    {
      name         = "web"
      image        = local.web_image
      essential    = true
      portMappings = [{ containerPort = 80, protocol = "tcp" }]
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
    {
      name         = "api"
      image        = local.api_image
      essential    = true
      portMappings = [{ containerPort = 8080, protocol = "tcp" }]

      # **ENGINE_CMD 를 여기 두지 않는다.** 엔진 실행 경로는 이미지 내부 구조라
      # Terraform이 알 수 없는 값이고, 태스크 정의의 environment 는 이미지의 ENV 를
      # 덮어쓴다. 양쪽에 적어두면 이미지를 바꿀 때 조용히 어긋난다 —
      # 실제로 엔진을 やねうら王로 바꾼 배포에서 여기 남아 있던 `fairy-stockfish` 가
      # 이겨서, 배포는 성공했는데 대국만 안 되는 상태가 됐다.
      #
      # 운영 손잡이(ENGINE_POOL_SIZE·ENGINE_HASH_MB 등)는 여기 둬도 된다.
      # 이미지 안에 없는 값이라 덮어쓸 대상이 없다.
      #
      # **기본값(탐색 3 · mate 2 · 해시 128MB)은 t4g.small 에 안 들어간다.** 엔진 하나가
      # NNUE 63MB + 해시를 통째로 잡으므로(internal/usi.Pool) 다섯 개면 ~955MB이고, Go 힙과
      # Caddy를 더하면 1.3GB다 — 인스턴스에서 태스크가 쓸 수 있는 것이 ~1.7GB뿐이다.
      #
      # 줄여도 되는 이유는 **쓰는 사람이 한 명**이라는 것이다. 세 풀이 동시에 필요한 것은
      # 상대 수·선행 계산·mate 탐색이 겹칠 때이고, 한 사람이 한 수를 두는 동안에는
      # 그 셋이 순서대로 온다. 탐색 2 + mate 1 이면 개입 판정과 상대 수가 겹치는 것까지 덮는다.
      environment = [
        { name = "ENGINE_POOL_SIZE", value = "2" },
        { name = "ENGINE_MATE_POOL_SIZE", value = "1" },
        { name = "ENGINE_HASH_MB", value = "64" },
      ]

      # **여기가 env.sh를 대체하는 지점이다.** ECS가 Parameter Store에서 읽어
      # 컨테이너에 직접 넣는다. 값이 디스크에 남지 않고, 로그에도 안 찍힌다.
      secrets = [
        for k in [
          "DATABASE_URL",
          "SESSION_SECRET",
          "GOOGLE_CLIENT_ID",
          "GOOGLE_CLIENT_SECRET",
          ] : {
          name      = k
          valueFrom = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter/show-gi/prod/${k}"
        }
      ]

      logConfiguration = {
        logDriver = "awslogs"
        options = {
          "awslogs-group"         = aws_cloudwatch_log_group.app.name
          "awslogs-region"        = var.aws_region
          "awslogs-stream-prefix" = "api"
        }
      }
    },
  ])
}

# ─── 서비스 ─────────────────────────────────────────────────

# **인스턴스의 보안그룹이기도 하다.** `host` 네트워크 모드라 태스크가 인스턴스의 ENI로
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
# **api의 8080은 안 연다.** Caddy가 `localhost:8080` 으로 부르므로 밖에서 닿을 필요가 없고,
# `host` 모드에서 그 포트가 인스턴스에 그대로 노출되므로 여는 순간 엔진이 공개된다.
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
  launch_type     = "EC2"

  # 컨테이너에 셸로 들어갈 수 있게 한다. SSH도, 배스천도 필요 없다:
  #   aws ecs execute-command --cluster show-gi --task <id> --container api --interactive --command /bin/sh
  enable_execute_command = true

  # **`network_configuration` 이 없다.** `host` 네트워크 모드에서는 태스크가 자기 ENI를
  # 갖지 않고 인스턴스의 것을 쓰므로, 서브넷도 보안그룹도 시작 템플릿이 정한다(ec2.tf).

  load_balancer {
    target_group_arn = aws_lb_target_group.web.arn
    container_name   = "web"
    container_port   = 80
  }

  # **옛 태스크를 먼저 내리고 새 것을 띄운다.** 원래는 반대였는데(100/200) 인스턴스가
  # 태스크 하나에 맞춰진 한 대뿐이라 두 개가 동시에 못 올라간다 — 그대로 두면 새 태스크가
  # 영원히 배치되지 않고 배포가 서킷 브레이커에 걸려 되돌아간다.
  #
  # 값은 **배포마다 1~2분 내려간다**는 뜻이다. 쓰는 사람이 한 명이라 받아들이는 쪽이고,
  # 무중단을 되찾으려면 인스턴스를 두 대로 늘리거나(비용 2배) 태스크를 반으로 줄여야 한다.
  deployment_minimum_healthy_percent = 0
  deployment_maximum_percent         = 100

  deployment_circuit_breaker {
    enable   = true
    rollback = true # 새 버전이 안 뜨면 자동으로 되돌린다
  }

  # 시작 직후에는 헬스체크 실패를 봐준다. 엔진 프로세스가 뜨는 데 시간이 걸리고,
  # **스팟이 회수된 뒤 새 인스턴스에서는 이미지를 처음 받는 시간까지 여기 들어온다** —
  # 60초로는 서킷 브레이커가 먼저 걸려 배포가 되돌아간다.
  health_check_grace_period_seconds = 120

  lifecycle {
    # 배포는 CI가 새 리비전을 등록해서 한다. terraform이 그걸 되돌리면
    # apply 한 번에 옛 이미지로 돌아간다. desired_count는 terraform 소유라
    # CLI로 수동 스케일링해도 다음 apply가 코드 값으로 되돌린다
    ignore_changes = [task_definition]
  }

  # **ASG가 먼저다.** 클러스터에 등록된 인스턴스가 없으면 서비스가 태스크를 못 띄우고,
  # 첫 apply 에서 그 상태로 몇 분을 기다린다.
  depends_on = [aws_lb_listener.https, aws_autoscaling_group.app]
}

output "cluster" {
  value = aws_ecs_cluster.main.name
}

output "service" {
  value = aws_ecs_service.app.name
}
