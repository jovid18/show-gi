# 이미지는 GitHub Actions에서 굽고 ECR에 올린다. 인스턴스는 받아서 띄우기만 한다.
#
# 인스턴스에서 직접 빌드해도 되지만, 8GiB짜리 박스에서 node 번들과 Go를 굽는 동안
# postgres와 엔진이 같은 메모리를 두고 다툰다. 빌드를 밖으로 빼면 배포가
# `pull && up`으로 줄어든다 — 마감 주에 하루 몇 번씩 하는 일이라 차이가 크다.
#
# 레포가 퍼블릭이라 GitHub의 arm64 러너를 공짜로 쓴다. QEMU 에뮬레이션 없이
# 네이티브로 구우므로 Graviton 인스턴스와 아키텍처가 그대로 맞는다.

locals {
  ecr_repos = ["api", "web"]
}

resource "aws_ecr_repository" "app" {
  for_each = toset(local.ecr_repos)

  name                 = "show-gi/${each.key}"
  image_tag_mutability = "MUTABLE" # latest 태그를 옮겨 쓴다

  image_scanning_configuration {
    scan_on_push = true
  }
}

# 태그별로 이미지를 쌓아두면 저장 요금이 조용히 는다. 되돌릴 만큼만 남긴다.
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
# 장기 액세스 키를 CI에 두지 않는다. OIDC로 워크플로 실행마다 단기 자격증명을 받는다 —
# 유출될 키가 애초에 존재하지 않는 것이 시크릿을 잘 숨기는 것보다 낫다.

# 이건 **계정 단위** 자원이다. 이 계정에는 아직 없어서 여기서 만들지만, 다른
# 프로젝트가 나중에 같은 프로바이더를 쓰기 시작하면 `terraform destroy`가 그쪽까지
# 끊는다. 그때는 이 블록을 지우고 data 소스로 바꿔 참조만 한다.
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
          # **main 브랜치로 한정한다.** 퍼블릭 레포라 아무나 PR을 열 수 있고,
          # 조건을 `repo:jovid18/show-gi:*`로 열어두면 남의 브랜치에서 이미지를
          # 밀어 넣을 수 있다. 배포되는 이미지는 main에서만 나온다.
          "token.actions.githubusercontent.com:sub" = "repo:jovid18/show-gi:ref:refs/heads/main"
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

# 배포까지 CI가 한다. 사람이 서버에 들어가 명령을 치는 것은 재현되지 않고,
# 기억에 의존하는 절차는 마감 주 새벽에 틀린다.
#
# 인바운드 포트를 열지 않고 배포하기 위해 SSM Run Command를 쓴다 — SSH를 여는
# 순간 키 관리와 22번 포트가 같이 돌아온다.
resource "aws_iam_role_policy" "github_actions_deploy" {
  name = "ssm-deploy"
  role = aws_iam_role.github_actions.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        # 문서는 AWS 관리형 셸 실행 문서 하나로 한정한다
        Effect   = "Allow"
        Action   = "ssm:SendCommand"
        Resource = "arn:aws:ssm:${var.aws_region}::document/AWS-RunShellScript"
      },
      {
        # 대상은 이 프로젝트 태그가 붙은 인스턴스뿐이다. 인스턴스 ID를 워크플로에
        # 박아두면 인스턴스를 다시 만들 때마다 CI를 고쳐야 한다
        Effect   = "Allow"
        Action   = "ssm:SendCommand"
        Resource = "arn:aws:ec2:${var.aws_region}:${data.aws_caller_identity.current.account_id}:instance/*"
        Condition = {
          StringEquals = { "ssm:resourceTag/Project" = "show-gi" }
        }
      },
      {
        # 실행 결과를 폴링해서 실패하면 CI를 빨갛게 만든다.
        # 이게 없으면 배포가 조용히 실패하고 워크플로는 초록으로 남는다
        Effect   = "Allow"
        Action   = ["ssm:GetCommandInvocation", "ssm:ListCommandInvocations", "ssm:ListCommands"]
        Resource = "*"
      },
      {
        Effect   = "Allow"
        Action   = "ec2:DescribeInstances"
        Resource = "*"
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
