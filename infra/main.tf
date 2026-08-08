# 공용 데이터 소스. 실제 자원은 ecs.tf / alb.tf / rds.tf / ecr.tf에 있다.
#
# EC2가 없다. 인스턴스도, 인스턴스 프로파일도, user_data도, SSM 세션도 사라졌다 —
# Fargate가 실행 환경을 맡으면서 "서버를 준비하는 코드"가 통째로 필요 없어졌다.

data "aws_caller_identity" "current" {}

data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
}
