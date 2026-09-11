# 이미지는 GitHub Actions 에서 굽고 ECR 에 올린다. 인스턴스는 받아서 띄우기만 한다.
#
# 인스턴스에서 직접 빌드하면 node 번들과 Go 를 굽는 동안 postgres 와 엔진이 같은 메모리를
# 두고 다툰다. 레포가 퍼블릭이라 GitHub 의 arm64 러너를 공짜로 쓰고, QEMU 없이 네이티브로
# 구우므로 Graviton 인스턴스와 아키텍처가 그대로 맞는다.

locals {
  ecr_repos = ["api", "web"]
}

resource "aws_ecr_repository" "app" {
  for_each = toset(local.ecr_repos)

  name                 = "show-gi/${each.key}"
  image_tag_mutability = "MUTABLE" # latest 태그를 옮겨 쓴다

  # 이미지가 남아 있는 리포지토리는 그냥 지워지지 않는다. 이 값이 없으면 destroy 가
  # RepositoryNotEmptyException 으로 막힌다(journal §128). 이미지는 CI 가 다시 굽는다.
  force_delete = true

  # 이 플래그 혼자서는 아무 일도 하지 않는다. 레지스트리 쪽 설정이 규칙 없이 비어 있으면
  # 그쪽이 이기고, 실제로 두 이미지 다 ScanNotFoundException 이었다(journal §134).
  image_scanning_configuration {
    scan_on_push = true
  }
}

# 스캔을 실제로 돌리는 자리. 계정 단위 자원이라 레지스트리 하나에 설정이 하나뿐이고,
# 여기 적은 규칙 목록이 그 계정의 전부를 대체한다. 다른 프로젝트가 나중에 자기 규칙을
# 넣으면 이 블록이 그것을 지우므로, 그때는 목록에 더하는 쪽으로 고친다.
#
# BASIC 을 쓴다. ENHANCED 는 Inspector 로 넘어가며 스캔한 이미지 수만큼 과금되는데, 이
# 레포는 한 커밋에 두 이미지이고 CVE 를 볼 사람이 하나다.
resource "aws_ecr_registry_scanning_configuration" "basic" {
  scan_type = "BASIC"

  rule {
    scan_frequency = "SCAN_ON_PUSH"

    repository_filter {
      filter      = "show-gi/*"
      filter_type = "WILDCARD"
    }
  }
}

# 태그별로 이미지를 쌓아두면 저장 요금이 경고 없이 는다. 되돌릴 만큼만 남긴다.
resource "aws_ecr_lifecycle_policy" "app" {
  for_each   = aws_ecr_repository.app
  repository = each.value.name

  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "태그 없는 이미지는 하루 뒤 지운다"
        selection = {
          tagStatus   = "untagged"
          countType   = "sinceImagePushed"
          countUnit   = "days"
          countNumber = 1
        }
        action = { type = "expire" }
      },
      {
        rulePriority = 2
        description  = "최근 10개만 남긴다"
        selection = {
          tagStatus   = "any"
          countType   = "imageCountMoreThan"
          countNumber = 10
        }
        action = { type = "expire" }
      },
    ]
  })
}

# ─── GitHub Actions ─────────────────────────────────────────
#
# 장기 액세스 키를 CI 에 두지 않는다. OIDC 로 워크플로 실행마다 단기 자격증명을 받는다.

# 계정 단위 자원이다. 이 계정에는 아직 없어서 여기서 만들지만, 다른 프로젝트가 나중에 같은
# 프로바이더를 쓰기 시작하면 terraform destroy 가 그쪽까지 끊는다. 그때는 이 블록을 지우고
# data 소스로 바꿔 참조만 한다.
resource "aws_iam_openid_connect_provider" "github" {
  url             = "https://token.actions.githubusercontent.com"
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = ["6938fd4d98bab03faadb97b34396831e3780aea1"]
}

resource "aws_iam_role" "github_actions" {
  name = "show-gi-github-actions"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Federated = aws_iam_openid_connect_provider.github.arn }
      Action    = "sts:AssumeRoleWithWebIdentity"
      Condition = {
        StringEquals = {
          "token.actions.githubusercontent.com:aud" = "sts.amazonaws.com"
          # main 브랜치로 한정한다. 퍼블릭 레포라 아무나 PR 을 열 수 있고, 조건을
          # repo:jovid18/show-gi:* 로 열어두면 남의 브랜치에서 이미지를 밀어 넣을 수 있다.
          #
          # 값을 둘 적는다. GitHub 이 subject claim 에 불변 ID 를 넣기 시작해서 실제로 오는
          # sub 가 문서에 흔히 적힌 형태와 다르다. 목록은 OR 로 평가되므로 어느 쪽이 와도
          # 통과한다.
          #
          # 현재 형식을 확인하는 법:
          #   gh api /repos/jovid18/show-gi/actions/oidc/customization/sub
          "token.actions.githubusercontent.com:sub" = [
            "repo:jovid18/show-gi:ref:refs/heads/main",
            "repo:jovid18@143411145/show-gi@1327659382:ref:refs/heads/main",
          ]
        }
      }
    }]
  })
}

resource "aws_iam_role_policy" "github_actions_push" {
  name = "ecr-push"
  role = aws_iam_role.github_actions.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        # 토큰 발급은 리소스를 지정할 수 없는 계정 단위 액션이다
        Effect   = "Allow"
        Action   = "ecr:GetAuthorizationToken"
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "ecr:BatchCheckLayerAvailability",
          "ecr:InitiateLayerUpload",
          "ecr:UploadLayerPart",
          "ecr:CompleteLayerUpload",
          "ecr:PutImage",
          "ecr:BatchGetImage",
          "ecr:GetDownloadUrlForLayer",
        ]
        # 이 프로젝트의 리포지토리에만. 계정의 다른 이미지는 건드리지 못한다
        Resource = [for r in aws_ecr_repository.app : r.arn]
      },
    ]
  })
}

# 배포까지 CI 가 한다. 사람이 서버에 들어가 명령을 치는 것은 재현되지 않는다.
resource "aws_iam_role_policy" "github_actions_deploy" {
  name = "ecs-deploy"
  role = aws_iam_role.github_actions.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["ecs:DescribeTaskDefinition", "ecs:RegisterTaskDefinition"]
        Resource = "*" # 태스크 정의 등록은 리소스를 지정할 수 없다
      },
      {
        # 티어마다 서비스가 하나다. 분석 쪽이 빠지면 배포가 그 서비스에서만 실패하고,
        # 사람이 쓰는 화면은 새것이라 한동안 누구도 알아채지 못한다.
        Effect   = "Allow"
        Action   = ["ecs:DescribeServices", "ecs:UpdateService"]
        Resource = [aws_ecs_service.app.id, aws_ecs_service.analysis.id]
      },
      {
        # 새 리비전에 역할을 붙이려면 넘길 권한이 필요하다. ECS 로만 넘길 수 있게
        # 제한한다. 아니면 임의 역할을 태워 권한을 올릴 수 있다
        Effect   = "Allow"
        Action   = "iam:PassRole"
        Resource = [aws_iam_role.task.arn, aws_iam_role.task_execution.arn]
        Condition = {
          StringEquals = { "iam:PassedToService" = "ecs-tasks.amazonaws.com" }
        }
      },
    ]
  })
}

output "ecr_registry" {
  description = "docker login 대상"
  value       = split("/", aws_ecr_repository.app["api"].repository_url)[0]
}

output "github_actions_role_arn" {
  description = "워크플로의 role-to-assume에 넣는 값"
  value       = aws_iam_role.github_actions.arn
}
