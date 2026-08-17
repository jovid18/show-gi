# ECS Fargate. EC2 + docker compose를 대신한다.
#
# 바꾼 이유는 비용이 아니라(주 $2 차이) **직접 쓴 배포 글루가 전부 사라지기 때문**이다:
# 배포 스크립트, 비밀을 셸로 내보내는 스크립트, compose 오버레이, 헬스체크 루프,
# ECR 로그인, 인증서 볼륨 — 전부 ECS·ALB의 기본 기능으로 대체된다.
# 내가 쓴 것만 내가 유지보수해야 한다.
#
# 태스크 하나에 컨테이너 둘을 넣는다. awsvpc 모드에서는 같은 태스크의 컨테이너가
# 네트워크 네임스페이스를 공유하므로 web이 localhost:8080으로 api에 닿는다.
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
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"

  # 엔진 탐색이 CPU를 지속적으로 쓴다. 상대 수·선행 계산·mate 탐색이 동시에 돌아야
  # 개입 판정이 사용자가 눈치채기 전에 끝난다.
  cpu    = var.task_cpu
  memory = var.task_memory

  runtime_platform {
    cpu_architecture        = "ARM64" # Graviton. x86보다 싸고 CI도 arm64로 굽는다
    operating_system_family = "LINUX"
  }

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
      environment = []

      # **여기가 env.sh를 대체하는 지점이다.** ECS가 Parameter Store에서 읽어
      # 컨테이너에 직접 넣는다. 값이 디스크에 남지 않고, 로그에도 안 찍힌다.
      secrets = [
        for k in [
          "DATABASE_URL",
          "SESSION_SECRET",
          "ORCA_API_KEY",
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

resource "aws_security_group" "task" {
  name        = "show-gi-task"
  description = "show-gi: ALB to task"
  vpc_id      = data.aws_vpc.default.id
}

# ALB에서만 들어온다. 태스크는 공인 IP를 갖지만(ECR·인터넷 egress 때문에)
# 인바운드는 ALB 보안그룹으로 잠겨 있다.
resource "aws_vpc_security_group_ingress_rule" "task_from_alb" {
  security_group_id            = aws_security_group.task.id
  referenced_security_group_id = aws_security_group.alb.id
  from_port                    = 80
  to_port                      = 80
  ip_protocol                  = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "task_all" {
  security_group_id = aws_security_group.task.id
  description       = "ECR, Parameter Store, LLM API"
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

resource "aws_ecs_service" "app" {
  name            = "show-gi"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.app.arn
  desired_count   = 0
  launch_type     = "FARGATE"

  # 컨테이너에 셸로 들어갈 수 있게 한다. SSH도, 배스천도 필요 없다:
  #   aws ecs execute-command --cluster show-gi --task <id> --container api --interactive --command /bin/sh
  enable_execute_command = true

  network_configuration {
    # ALB가 켜진 AZ와 같은 서브넷만 쓴다 — ALB는 활성 AZ의 타깃에만 라우팅한다
    subnets = local.alb_subnet_ids
    # NAT 게이트웨이(월 $40+)를 피하려고 공인 서브넷에 둔다. 인바운드는 보안그룹이 막는다
    assign_public_ip = true
    security_groups  = [aws_security_group.task.id]
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.web.arn
    container_name   = "web"
    container_port   = 80
  }

  # 새 태스크가 헬스체크를 통과한 뒤에 옛 태스크를 내린다. deploy.sh의
  # 헬스체크 루프가 하던 일을 ECS가 한다.
  deployment_minimum_healthy_percent = 100
  deployment_maximum_percent         = 200

  deployment_circuit_breaker {
    enable   = true
    rollback = true # 새 버전이 안 뜨면 자동으로 되돌린다
  }

  # 시작 직후에는 헬스체크 실패를 봐준다. 엔진 프로세스가 뜨는 데 시간이 걸린다
  health_check_grace_period_seconds = 60

  lifecycle {
    # 배포는 CI가 새 리비전을 등록해서 한다. terraform이 그걸 되돌리면
    # apply 한 번에 옛 이미지로 돌아간다. desired_count는 terraform 소유라
    # CLI로 수동 스케일링해도 다음 apply가 코드 값으로 되돌린다
    ignore_changes = [task_definition]
  }

  depends_on = [aws_lb_listener.https]
}

output "cluster" {
  value = aws_ecs_cluster.main.name
}

output "service" {
  value = aws_ecs_service.app.name
}
