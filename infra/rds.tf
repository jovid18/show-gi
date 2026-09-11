# postgres 를 인스턴스에서 뺀다. 컨테이너로 두면 DB 의 수명이 EC2 에 묶이고, 실제로 가장
# 자주 데이터를 잃는 경로는 디버깅 중의 docker compose down -v 다.
#
# 다시 만들기 비싼 것이 여기 산다. 엔진이 depth 14 로 계산한 국면 캐시다.

resource "random_password" "db" {
  length = 32
  # RDS가 거부하는 문자를 뺀다: / @ " 공백
  override_special = "!#$%&*()-_=+[]{}<>:?"
}

resource "aws_db_subnet_group" "main" {
  name       = "show-gi"
  subnet_ids = data.aws_subnets.default.ids
}

resource "aws_security_group" "db" {
  name = "show-gi-db"
  # 보안그룹의 description 은 불변이다. 고치면 Terraform 이 SG 를 다시 만들려 하고, RDS 가
  # 그 SG 를 쓰고 있으면 ENI 를 떼지 못해 apply 가 중간에 멈춘다.
  description = "show-gi: postgres from the app instance only"
  vpc_id      = data.aws_vpc.default.id
}

# 태스크 보안그룹에서만 들어올 수 있다. CIDR 대신 보안그룹을 참조한다. 태스크는 배포마다
# IP 가 바뀌므로 CIDR 로는 표현할 수 없다.
resource "aws_vpc_security_group_ingress_rule" "db_from_app" {
  security_group_id            = aws_security_group.db.id
  description                  = "postgres from the ECS task"
  referenced_security_group_id = aws_security_group.task.id
  from_port                    = 5432
  to_port                      = 5432
  ip_protocol                  = "tcp"
}

# 운영자 노트북에서 직접 붙는 통로.
#
# NAT 나 bastion 없이 붙는다. RDS 가 default VPC 의 퍼블릭 서브넷에 있어서, 막고 있는 것은
# publicly_accessible 과 이 규칙뿐이다.
#
# 실질 방어선이 이 규칙이다. 여기 없는 주소는 포트에 닿지도 못하고, 비밀번호는 그 다음 겹이다.
#
# admin_cidr 이 없으면 규칙 자체가 생기지 않는다. 값을 주지 않고 apply 하면 이미 있던 규칙이
# 지워지고, 그게 의도다.
resource "aws_vpc_security_group_ingress_rule" "db_from_admin" {
  count = var.admin_cidr == null ? 0 : 1

  security_group_id = aws_security_group.db.id
  description       = "postgres from the operator laptop"
  cidr_ipv4         = var.admin_cidr
  from_port         = 5432
  to_port           = 5432
  ip_protocol       = "tcp"
}

resource "aws_db_instance" "main" {
  identifier     = "show-gi"
  engine         = "postgres"
  engine_version = "17.10" # 로컬 개발 이미지(pgvector:pg17)와 메이저를 맞춘다

  instance_class    = var.db_instance_class
  allocated_storage = 20 # gp3 최소치
  storage_type      = "gp3"
  storage_encrypted = true

  db_name  = "showgi"
  username = "showgi"
  password = random_password.db.result

  db_subnet_group_name   = aws_db_subnet_group.main.name
  vpc_security_group_ids = [aws_security_group.db.id]

  # 공개 엔드포인트를 준다. 누가 닿을 수 있는지는 보안그룹이 정한다.
  #
  # 닫아두면 프로덕션 데이터를 보거나 고칠 방법이 일회용 ECS 태스크뿐인데, 그 방식은 조회
  # 결과가 CloudWatch 로만 나가고 운영자 정책에 로그 읽기 권한이 없다(docs/06-status.md §7).
  #
  # 담기는 것이 본인 대국 기록이고 접근이 단일 IP 로 제한된다는 조건에서 감수한다.
  publicly_accessible = true

  # 7일치는 무료다. 시점 복구가 되면 잘못된 마이그레이션도 되돌릴 수 있다
  backup_retention_period = 7
  backup_window           = "18:00-19:00" # JST 03:00-04:00, 작업하지 않는 시간
  maintenance_window      = "Mon:19:30-Mon:20:30"

  # 마이너 버전은 알아서 올린다. 마감 주에 보안 패치를 손으로 챙길 여유가 없다
  auto_minor_version_upgrade = true

  # 대회가 끝나면 전부 지운다. 스냅샷을 요구하면 destroy 가 막혀서 정리할 수 없다
  skip_final_snapshot = true
  deletion_protection = false

  # 느린 쿼리를 로그로 본다. 개입 판정이 착수 경로에 있어서 DB가 늦으면 바로 체감된다
  enabled_cloudwatch_logs_exports = ["postgresql"]
}

# 접속 문자열을 Parameter Store 에 넣는다. 앱은 다른 환경변수와 똑같이 받아 가므로 RDS 로
# 옮겼다는 사실을 애플리케이션 코드가 알 필요가 없다.
resource "aws_ssm_parameter" "database_url" {
  name  = "/show-gi/prod/DATABASE_URL"
  type  = "SecureString"
  value = "postgres://${aws_db_instance.main.username}:${urlencode(random_password.db.result)}@${aws_db_instance.main.endpoint}/${aws_db_instance.main.db_name}?sslmode=require"
}

output "db_endpoint" {
  value = aws_db_instance.main.endpoint
}
