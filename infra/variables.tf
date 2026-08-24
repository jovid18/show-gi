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

variable "instance_type" {
  description = <<-EOT
    ECS 컨테이너 인스턴스의 타입. **arm64여야 한다** — 엔진이 arm64 Debian 바이너리다.

    **버스터블(T 계열)을 쓰지 않는다.** 엔진 탐색이 CPU를 계속 몰아 써서 크레딧이 마르고,
    그러면 AWS가 baseline 20%로 조인다 — 실측으로 탐색이 0.24초에서 8.5초가 됐다
    (journal §108). T 계열에서 「N판 된다」는 「N판을 몇 분 된다」라서 용량표에 적을 수 없다.

    c6g.large(2 vCPU / 4 GiB)가 그 자리를 대신한다. 지속 vCPU 당 값으로는 t4g.small 보다
    싸다 — 크레딧이 마른 t4g.small 이 0.4 vCPU 라서다. m6g.large 는 같은 2 vCPU 에 8 GiB 라
    안 쓰는 RAM 값을 더 낸다.

    올릴 때 `task_memory` 는 따라 올리지 않아도 된다. 엔진이 쓰는 양은 `ENGINE_HASH_MB` 가
    정하고 그 값이 그대로이므로, 늘려 봐야 아무도 안 쓴다.
  EOT
  type        = string
  default     = "c6g.large"
}

variable "task_cpu" {
  description = <<-EOT
    태스크가 예약하는 CPU 단위(1024 = 1 vCPU). **인스턴스의 vCPU를 넘으면 태스크가 아예
    배치되지 않는다** — Fargate처럼 임의의 조합을 고르는 것이 아니라 실제 하드웨어에서 뺀다.

    c6g.large가 2 vCPU라 2048이 상한이고, 그것을 다 쓴다 — 태스크가 하나뿐이라 남겨 둘
    이유가 없고, 엔진 탐색은 주는 만큼 쓴다.
  EOT
  type        = string
  default     = "2048"
}

variable "task_memory" {
  description = <<-EOT
    태스크가 예약하는 메모리(MiB). **인스턴스에서 실제로 빠지는 값이다.**

    c6g.large는 4096 MiB라 1536이 넉넉히 들어간다. 이 값을 안 올리는 이유는 엔진이 쓰는
    양이 `ENGINE_HASH_MB` 로 정해져서다 — 예약만 늘리면 남는 것을 아무도 안 쓴다.
    t4g.small(2048 MiB)에서는 OS와 ECS 에이전트가 먹고 남는 ~1750 MiB 가 상한이었다.

    엔진이 쓰는 양은 `ENGINE_POOL_SIZE` · `ENGINE_MATE_POOL_SIZE` · `ENGINE_HASH_MB` 로
    정해진다(ecs.tf의 그 자리). **이 값만 올리면 안 늘어난다.**
  EOT
  type        = string
  default     = "1536"
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

variable "alarm_email" {
  description = <<-EOT
    알람을 받을 메일 주소. **비워 두는 것이 기본이다** — 레포가 퍼블릭이라 주소를
    커밋할 수 없다. 값은 `terraform.tfvars`(gitignore 대상)에 적는다.

    비면 SNS 주제만 만들고 구독은 안 만든다. 알람은 그대로 돌고 콘솔에서만 보인다.

    **주소를 적어 apply 하면 확인 메일이 한 통 온다. 그 링크를 눌러야 활성된다** —
    누르기 전까지는 알람이 울려도 메일이 오지 않는다.
  EOT
  type        = string
  default     = ""
}
