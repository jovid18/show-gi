# state를 로컬에 두지 않는다. 두 가지 이유다.
#   1. 날리면 복구가 안 된다 — Terraform이 만든 자원을 손으로 추적해야 한다
#   2. state에는 평문 비밀이 들어간다. 이 레포는 퍼블릭이라 실수 한 번이 치명적이다
#      (.gitignore로도 막고 있지만 방어는 두 겹이 낫다)
#
# 버킷과 잠금 테이블은 Terraform 밖에서 먼저 만든다. state를 담을 자원을 state로
# 관리하려는 순환을 피하기 위해서다 — 명령 두 줄이고, deploy/README.md에 있다.

terraform {
  backend "s3" {
    bucket         = "show-gi-terraform-state-058264445568"
    key            = "prod/terraform.tfstate"
    region         = "ap-northeast-1"
    encrypt        = true
    dynamodb_table = "show-gi-terraform-lock"

    # 백엔드는 variables.tf를 읽지 못한다 — 변수가 평가되기 전에 초기화되기 때문이다.
    # 그래서 프로파일을 여기 한 번 더 적는다. 안 적으면 기본 프로파일로 붙으려 하고,
    # 이 기계의 기본 프로파일은 만료된 회사 자격증명이라 init에서 403이 난다.
    profile = "show-gi"
  }
}

# 잠금에 DynamoDB를 쓰는 이유는 순전히 버전 때문이다. S3 네이티브 잠금
# (`use_lockfile`)이 테이블을 없애주지만 Terraform 1.11+가 필요하고, 지금 로컬은
# 1.5.7이다. 마감 중에 도구 버전을 올리는 것보다 테이블 하나가 싸다.
# 1.11 이상으로 올릴 때 `dynamodb_table`을 `use_lockfile = true`로 바꾸고
# 테이블을 지우면 된다.
