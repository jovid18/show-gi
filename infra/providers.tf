terraform {
  required_version = ">= 1.5.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region  = var.aws_region
  profile = var.aws_profile

  # 태그를 프로바이더 레벨에서 붙인다. 대회가 끝나고 정리할 때 Project 태그로
  # 골라낼 수 있어야 한다 — 이 계정에는 다른 프로젝트 자원도 있다.
  default_tags {
    tags = {
      Project   = "show-gi"
      ManagedBy = "terraform"
    }
  }
}
