# 공용 데이터 소스. 실제 자원은 ecs.tf / ec2.tf / alb.tf / rds.tf / ecr.tf 에 있다.

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
