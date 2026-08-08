# postgres를 인스턴스에서 뺀다.
#
# 컨테이너로 두면 DB의 수명이 EC2에 묶인다. 그 자체도 문제지만, 실제로 가장 자주
# 데이터를 잃는 경로는 디버깅 중의 `docker compose down -v`다 — 로컬에서는 맞는
# 명령이라 손이 먼저 나간다.
#
# 그리고 다시 만들기 비싼 것들이 여기 산다: LLM으로 생성한 설명 캐시(다시 만들면
# 돈이 든다), 엔진이 depth 14로 계산한 국면 캐시, 출처 검증까지 끝낸 RAG 코퍼스.

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
  # 보안그룹의 description은 **불변**이다. 고치면 Terraform이 SG를 다시 만들려 하고,
  # RDS가 그 SG를 쓰고 있으면 ENI를 떼지 못해 apply가 중간에 멈춘다.
  # 문구가 낡았더라도 그대로 두는 편이 싸다.
  description = "show-gi: postgres from the app instance only"
  vpc_id      = data.aws_vpc.default.id
}

# **태스크 보안그룹에서만** 들어올 수 있다. CIDR이 아니라 보안그룹을 참조하는 것이
# 요점이다 — Fargate 태스크는 배포마다 IP가 바뀌므로 CIDR로는 애초에 표현할 수 없다.
resource "aws_vpc_security_group_ingress_rule" "db_from_app" {
  security_group_id            = aws_security_group.db.id
  description                  = "postgres from the ECS task"
  referenced_security_group_id = aws_security_group.task.id
  from_port                    = 5432
  to_port                      = 5432
  ip_protocol                  = "tcp"
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
  # 인터넷에서 직접 붙을 수 없다. 접근 경로는 앱 인스턴스뿐이다
  publicly_accessible = false

  # 7일치는 무료다. 이게 RDS로 옮기는 이유의 절반이다 —
  # 시점 복구가 되면 잘못된 마이그레이션도 되돌릴 수 있다
  backup_retention_period = 7
  backup_window           = "18:00-19:00" # JST 03:00-04:00, 작업 안 하는 시간
  maintenance_window      = "Mon:19:30-Mon:20:30"

  # 마이너 버전은 알아서 올린다. 마감 주에 보안 패치를 손으로 챙길 여유가 없다
  auto_minor_version_upgrade = true

  # 대회가 끝나면 통째로 지운다. 스냅샷을 요구하면 destroy가 막혀서,
  # 정리해야 할 때 정리가 안 된다. 운영 서비스라면 반대로 둔다
  skip_final_snapshot = true
  deletion_protection = false

  # 느린 쿼리를 로그로 본다. 개입 판정이 착수 경로에 있어서 DB가 늦으면 바로 체감된다
  enabled_cloudwatch_logs_exports = ["postgresql"]
}

# 접속 문자열을 Parameter Store에 넣는다. 앱은 다른 환경변수와 똑같이 받아 간다 —
# RDS로 옮겼다는 사실을 애플리케이션 코드가 알 필요가 없다.
resource "aws_ssm_parameter" "database_url" {
  name  = "/show-gi/prod/DATABASE_URL"
  type  = "SecureString"
  value = "postgres://${aws_db_instance.main.username}:${urlencode(random_password.db.result)}@${aws_db_instance.main.endpoint}/${aws_db_instance.main.db_name}?sslmode=require"
}

output "db_endpoint" {
  value = aws_db_instance.main.endpoint
}
