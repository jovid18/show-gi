# state 를 로컬에 두지 않는다. 날리면 Terraform 이 만든 자원을 손으로 추적해야 하고,
# state 에는 평문 비밀이 들어가는데 이 레포는 퍼블릭이다.
#
# 버킷과 잠금 테이블은 Terraform 밖에서 먼저 만든다. state 를 담을 자원을 state 로
# 관리하려는 순환을 피하기 위해서다. 명령은 deploy/README.md 에 있다.

terraform {
  backend "s3" {
    bucket         = "show-gi-terraform-state-058264445568"
    key            = "prod/terraform.tfstate"
    region         = "ap-northeast-1"
    encrypt        = true
    dynamodb_table = "show-gi-terraform-lock"

    # 백엔드는 variables.tf 를 읽지 못한다. 변수가 평가되기 전에 초기화되므로 프로파일을
    # 여기 한 번 더 적는다. 적지 않으면 기본 프로파일로 붙으려 하고, 이 기계의 기본
    # 프로파일은 만료된 회사 자격증명이라 init 에서 403 이 난다.
    profile = "show-gi"
  }
}

# 잠금에 DynamoDB 를 쓰는 것은 버전 때문이다. S3 네이티브 잠금(use_lockfile)이 테이블을
# 없애주지만 Terraform 1.11+ 가 필요하고 지금 로컬은 1.5.7 이다. 올릴 때 dynamodb_table 을
# use_lockfile = true 로 바꾸고 테이블을 지운다.
