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

variable "db_instance_class" {
  description = <<-EOT
    RDS 인스턴스 클래스. 데이터가 작고(코퍼스 50행, 국면 캐시 수천 건) 질의도
    단순해서 가장 작은 것으로 시작한다. 개입 판정이 착수 경로에 있으므로 느려지면
    바로 체감되는데, 그때 올리면 된다 — 스토리지와 달리 클래스는 줄일 수도 있다.
  EOT
  type        = string
  default     = "db.t4g.micro" # 2 vCPU(버스터블) / 1 GiB
}

variable "task_cpu" {
  description = <<-EOT
    Fargate vCPU (1024 = 1 vCPU). 엔진 셋(상대 수·선행 계산·mate 탐색)이 동시에
    돌아야 개입 판정이 착수 흐름을 끊지 않는다. 느려지면 올린다 — EC2와 달리
    인스턴스를 갈아엎지 않고 태스크 정의만 바꾸면 된다.
  EOT
  type        = string
  default     = "4096"
}

variable "task_memory" {
  description = "Fargate 메모리(MiB). 4096 vCPU에서 허용되는 최소치가 8192다"
  type        = string
  default     = "8192"
}

variable "image_tag" {
  description = <<-EOT
    태스크 정의가 처음 참조할 이미지 태그. 이후 배포는 CI가 커밋 SHA로 새 리비전을
    등록하므로 이 값은 최초 생성에만 쓰인다 (서비스는 task_definition 변경을 무시한다).
  EOT
  type        = string
  default     = "latest"
}

variable "admin_cidr" {
  description = <<-EOT
    운영자 노트북에서 RDS에 붙을 주소(CIDR). 예: "203.0.113.9/32".

    **레포에 커밋하지 않는다.** 퍼블릭 레포라 집 IP가 그대로 공개된다. `.gitignore` 가
    `*.tfvars` 를 막으므로 `infra/terraform.tfvars` 에 두면 커밋되지 않고 매 apply 에
    자동으로 먹는다 (terraform.tfvars.example 참조).

    비우면 그 통로가 아예 안 열린다. **값을 안 주고 apply 하면 이미 있던 규칙이
    지워진다** — 통로가 조용히 남아 있지 않게 하려는 것이다.

    공인 IP가 바뀌면 여기를 고치고 apply 한다. 보안그룹 규칙 하나라 수십 초다.
  EOT
  type        = string
  default     = null

  validation {
    condition     = var.admin_cidr == null ? true : can(cidrhost(var.admin_cidr, 0))
    error_message = "admin_cidr 은 CIDR 표기여야 한다 (예: 203.0.113.9/32)."
  }
}
