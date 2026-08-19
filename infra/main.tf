# 공용 데이터 소스. 실제 자원은 ecs.tf / ec2.tf / alb.tf / rds.tf / ecr.tf에 있다.
#
# EC2가 돌아왔다. Fargate가 실행 환경을 맡으면서 한때 통째로 사라졌던 자리인데,
# 24시간 도는 4 vCPU / 8 GiB의 값이 쓰는 사람 수에 안 맞아 스팟 한 대로 되돌렸다(ec2.tf).
# 되돌아온 것은 시작 템플릿·ASG·인스턴스 프로파일이고, 배포 스크립트는 안 돌아왔다 —
# 그건 Fargate가 아니라 ECS가 맡던 일이다.

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
