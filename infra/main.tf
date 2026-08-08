# EC2 한 대에 docker compose. 모듈로 쪼개지 않은 것은 자원이 10개 남짓이라
# 쪼개면 오히려 읽기 어려워지기 때문이다.

data "aws_vpc" "default" {
  default = true
}

# 최신 Amazon Linux 2023 (arm64). AMI ID를 박아두면 리전이 바뀌거나 이미지가
# 폐기될 때 조용히 깨진다 — SSM 파라미터가 항상 최신을 가리킨다.
data "aws_ssm_parameter" "al2023" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-arm64"
}

# ─── 네트워크 ───────────────────────────────────────────────

resource "aws_security_group" "web" {
  name        = "show-gi-web"
  description = "show-gi: HTTP/HTTPS in, all out"
  vpc_id      = data.aws_vpc.default.id
}

resource "aws_vpc_security_group_ingress_rule" "http" {
  security_group_id = aws_security_group.web.id
  description       = "Caddy: ACME 챌린지와 HTTPS 리다이렉트"
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_ingress_rule" "https" {
  security_group_id = aws_security_group.web.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"
}

# HTTP/3. Caddy가 UDP 443으로도 받는다
resource "aws_vpc_security_group_ingress_rule" "https_udp" {
  security_group_id = aws_security_group.web.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 443
  to_port           = 443
  ip_protocol       = "udp"
}

# 기본값 null이면 규칙이 만들어지지 않는다 = 22번 포트가 열리지 않는다.
resource "aws_vpc_security_group_ingress_rule" "ssh" {
  for_each = var.ssh_cidr == null ? toset([]) : toset(var.ssh_cidr)

  security_group_id = aws_security_group.web.id
  description       = "비상용 SSH"
  cidr_ipv4         = each.value
  from_port         = 22
  to_port           = 22
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "all" {
  security_group_id = aws_security_group.web.id
  description       = "패키지 설치, ACME, LLM API"
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

# ─── 인스턴스 ───────────────────────────────────────────────

# SSM Session Manager로 들어간다. 그래서 SSH 키도, 22번 포트도 필요 없다 —
# 열려 있지 않은 포트는 공격당하지 않는다.
resource "aws_iam_role" "instance" {
  name = "show-gi-instance"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "ssm" {
  role       = aws_iam_role.instance.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

# 이미지를 받아오기만 한다. 인스턴스가 이미지를 밀어 넣을 수 있으면, 서버가
# 뚫렸을 때 다음 배포에 그 이미지가 실려 나간다 — 읽기 전용이 그 경로를 끊는다.
resource "aws_iam_role_policy_attachment" "ecr_read" {
  role       = aws_iam_role.instance.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly"
}

resource "aws_iam_instance_profile" "instance" {
  name = "show-gi-instance"
  role = aws_iam_role.instance.name
}

resource "aws_instance" "app" {
  ami                    = data.aws_ssm_parameter.al2023.value
  instance_type          = var.instance_type
  vpc_security_group_ids = [aws_security_group.web.id]
  iam_instance_profile   = aws_iam_instance_profile.instance.name

  user_data                   = file("${path.module}/user_data.sh")
  user_data_replace_on_change = false # 스크립트를 고쳐도 인스턴스를 다시 만들지 않는다

  root_block_device {
    volume_size = var.root_volume_gb
    volume_type = "gp3"
    encrypted   = true
  }

  # IMDSv2 강제. v1은 SSRF 한 번으로 인스턴스 자격증명이 새어 나가는 고전적인 경로다
  metadata_options {
    http_tokens                 = "required"
    http_endpoint               = "enabled"
    http_put_response_hop_limit = 2 # 컨테이너에서 한 홉 더 간다
  }

  tags = { Name = "show-gi" }
}

# ─── DNS ────────────────────────────────────────────────────

# 인스턴스를 다시 만들어도 IP가 유지되어야 한다. IP가 바뀌면 DNS 전파를 기다리는
# 동안 Caddy가 인증서를 못 받고, Let's Encrypt 실패 한도까지 밀릴 수 있다.
resource "aws_eip" "app" {
  instance = aws_instance.app.id
  domain   = "vpc"
}

data "aws_route53_zone" "main" {
  name         = "${var.domain}."
  private_zone = false
}

resource "aws_route53_record" "apex" {
  zone_id = data.aws_route53_zone.main.zone_id
  name    = var.domain
  type    = "A"
  ttl     = 60 # 짧게 둔다. 마감 중에 서버를 옮길 일이 생기면 이 값이 대기 시간이 된다
  records = [aws_eip.app.public_ip]
}

resource "aws_route53_record" "www" {
  zone_id = data.aws_route53_zone.main.zone_id
  name    = "www.${var.domain}"
  type    = "A"
  ttl     = 60
  records = [aws_eip.app.public_ip]
}
