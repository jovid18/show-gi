variable "aws_region" {
  description = "배포 리전. 사용자가 일본에 있으므로 도쿄다"
  type        = string
  default     = "ap-northeast-1"
}

variable "aws_profile" {
  description = "~/.aws/credentials의 프로파일 이름. 이 프로젝트 전용 IAM 사용자를 쓴다"
  type        = string
  default     = "show-gi"
}

variable "domain" {
  description = "Route53에 등록된 도메인. 호스팅 존이 이미 있어야 한다"
  type        = string
  default     = "show-gi.com"
}

variable "instance_type" {
  description = <<-EOT
    엔진 3개(상대 수 / 선행 계산 / mate 탐색)와 postgres가 한 대에 올라간다.
    엔진 탐색은 지속 CPU 부하라 버스터블(t계열)은 크레딧이 떨어지는 순간 느려진다 —
    데모 영상을 찍는 중에 그렇게 되면 되돌릴 방법이 없다.
  EOT
  type        = string
  default     = "c7g.xlarge" # 4 vCPU / 8 GiB, Graviton3
}

variable "root_volume_gb" {
  description = "루트 EBS 크기(GiB). 도커 이미지와 postgres 데이터가 여기 들어간다"
  type        = number
  default     = 30
}

variable "ssh_cidr" {
  description = <<-EOT
    22번 포트를 열어줄 CIDR. 기본은 null = **포트를 아예 열지 않는다.**
    평소 접속은 SSM Session Manager로 한다 (deploy/README.md).
    SSM이 죽었을 때의 비상용으로만 자기 IP를 넣는다: ["1.2.3.4/32"]
  EOT
  type        = list(string)
  default     = null
}
