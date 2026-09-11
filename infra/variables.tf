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
    RDS 인스턴스 클래스. 데이터가 작고 질의도 단순해서 가장 작은 것으로 시작한다.
    개입 판정이 착수 경로에 있으므로 느려지면 그때 올린다.
    스토리지와 달리 클래스는 줄일 수도 있다.
  EOT
  type        = string
  default     = "db.t4g.micro" # 2 vCPU(버스터블) / 1 GiB
}

variable "instance_type" {
  description = <<-EOT
    ECS 컨테이너 인스턴스의 타입. arm64여야 한다. 엔진이 arm64 Debian 바이너리다.

    지금은 절약 모드라 t4g.small 이다(journal §125).

    이 값으로는 용량표가 성립하지 않는다. T 계열은 크레딧이 마르면 baseline 으로
    조여서 탐색이 8배 느려진다(journal §108). 시험을 다시 돌리기 전에 c6g.large 로
    되돌린다.

    task_memory 가 이 값에 묶인다. 아래에서 1536 을 유지하는 이유가 그것이다.
  EOT
  type        = string
  default     = "t4g.small"
}

variable "instance_type_fallbacks" {
  description = <<-EOT
    스팟 재고가 없을 때 대신 받을 타입들. instance_type 다음 순서로 시도한다.

    타입 하나로는 사이트가 재고에 매인다. 회수 자체가 재고가 마른 신호라 대체도
    같이 실패한다(journal §106 · §109). 풀이 타입 수 × AZ 수 이므로 타입을 늘리는
    것이 가장 싼 수단이다.

    전부 arm64 여야 한다. t3·t3a 는 x86_64 라 그 타입을 받은 날 이미지가 아예 뜨지
    않는다. 전부 2 GiB 이상이어야 한다. task_memory 가 들어가지 못하는 타입이 섞이면
    그날 태스크가 뜨지 않고, 1 GiB 인 t4g.micro 에서는 엔진이 기동에 실패한다(journal §125).

    절약 모드라 T 계열이다. 시험용으로 되돌릴 때는 instance_type 과 함께 비버스터블
    2 vCPU 목록으로 바꾼다. c7g·m6g·m7g·r6g·c6gn 의 .large 였다.
  EOT
  type        = list(string)
  default     = ["t4g.medium", "t4g.large"]
}

variable "on_demand_base_capacity" {
  description = <<-EOT
    온디맨드로 받을 그룹당 최소 대수. 1이면 두 그룹이 통째로 온디맨드다.
    base 를 넘는 대도 base 를 따라가게 묶어 뒀다(ec2.tf).

    부하 시험 동안만 1이다. 스팟 회수를 밟으면 잰 것이 용량 대신 회수 복구 시간이
    된다(journal §109). 시험이 끝나면 0으로 되돌린다. 상시로 두면 값이 네 배다.

    그 apply 가 도는 대를 갈아치운다(journal §124). 아래 percentage 가 이 값에서
    유도되므로 ASG 가 구매 정책을 맞추려고 온디맨드를 스팟으로 바꿔 낀다.
    사람이 보지 않는 시간에 건다.

    부하 때 자동으로 붙는 두 번째 대도 이 값이 0이면 스팟이다(journal §124).
    그룹 공통 변수라 「분석만 온디맨드」로는 나눌 수 없다.
  EOT
  type        = number
  default     = 0
}

variable "analysis_max_instances" {
  description = <<-EOT
    분석 티어의 대수 상한. ASG 의 max_size 이자 scalable target 의 max_capacity 다.

    desired 는 여기서 정하지 않는다. 스케일 정책이 그 자리를 가진다(autoscale.tf).
    terraform 이 desired 의 주인이면 스케일 아웃이 다음 apply 에 취소된다.

    손잡이가 하나인 것은 network_mode 가 host 라 태스크 하나에 인스턴스 하나이기
    때문이다(ecs.tf). 태스크를 늘리면 용량 공급자가 EC2 를 따라 올린다.

    2 다. 1대 6판 지속 · 2대 12판 지속까지 실측으로 있고(journal §121 · §122),
    3대는 보지 않기로 했다(journal §123).

    상한이고 요금은 따로다. 큐가 30분 비면 정책이 대수를 1로 되돌린다(autoscale.tf).
    다만 값이 붙는 창은 30분 + 60분이다. 스케일인한 대가 ecs-managed-draining 훅의
    HeartbeatTimeout 이 만료될 때까지 한 시간 더 산다(journal §134).

    상호작용 티어는 이 손잡이가 없다. 방이 짝지은 프로세스의 메모리에 있으므로
    (journal §98) 두 대면 초대·매칭이 절반 확률로 깨진다.
  EOT
  type        = number
  default     = 2

  validation {
    condition     = var.analysis_max_instances >= 1
    error_message = "analysis_max_instances 는 1 이상이어야 한다 — 0이면 밀린 手를 누구도 집지 않는다."
  }
}

variable "task_cpu" {
  description = <<-EOT
    태스크가 예약하는 CPU 단위(1024 = 1 vCPU). 인스턴스의 vCPU를 넘으면 태스크가
    아예 배치되지 않는다. Fargate처럼 임의의 조합을 고르지 않고 실제 하드웨어에서 뺀다.

    c6g.large가 2 vCPU라 2048이 상한이고, 그것을 다 쓴다. 태스크가 하나뿐이고 엔진
    탐색은 주는 만큼 쓴다.
  EOT
  type        = string
  default     = "2048"
}

variable "task_memory" {
  description = <<-EOT
    태스크가 예약하는 메모리(MiB). 인스턴스에서 실제로 빠지는 값이다.

    인스턴스가 ECS 에 등록하는 양을 넘으면 태스크가 아예 배치되지 않는다. 등록량이
    공칭보다 적다. 실측으로 c6gn.large(4 GiB)가 3,811 MiB 를 등록한다.

    정상 상태 RSS 로 이 값을 잡으면 안 된다. 엔진은 평가 파일과 해시를 기동 때
    한꺼번에 잡으므로 봉우리가 정상 상태보다 훨씬 높다(journal §125).
    그 봉우리를 재지 않았으므로 1536 을 유지한다.

    엔진 몫은 ENGINE_POOL_SIZE · ENGINE_MATE_POOL_SIZE · ENGINE_HASH_MB 가 정한다
    (ecs.tf의 그 자리). 이 값만 올리면 늘어나지 않는다.
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

    레포에 커밋하지 않는다. 퍼블릭 레포라 집 IP가 그대로 공개된다. .gitignore 가
    *.tfvars 를 막으므로 infra/terraform.tfvars 에 두면 커밋되지 않고 매 apply 에
    자동으로 먹는다.

    비우면 그 통로가 아예 열리지 않는다. 값을 주지 않고 apply 하면 이미 있던 규칙이
    지워진다. 통로가 조용히 남아 있지 않게 하려는 것이다.

    공인 IP가 바뀌면 여기를 고치고 apply 한다. 보안그룹 규칙 하나라 수십 초다.
    낡으면 거절 대신 타임아웃이 오므로 「DB 가 안 떴나」로 오해하기 쉽다(journal §133).
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
    알람을 받을 메일 주소. 비워 두는 것이 기본이다. 레포가 퍼블릭이라 주소를 커밋할
    수 없고, 값은 terraform.tfvars(gitignore 대상)에 적는다.

    비면 SNS 주제만 만들고 구독은 만들지 않는다. 알람은 그대로 돌고 콘솔에서만 보인다.

    주소를 적어 apply 하면 확인 메일이 한 통 온다. 그 링크를 눌러야 활성되고,
    누르기 전까지는 알람이 울려도 메일이 오지 않는다.
  EOT
  type        = string
  default     = ""
}
