# ALB가 TLS를 끝낸다. Caddy가 하던 인증서 발급·갱신이 ACM으로 넘어간다.
#
# Fargate로 옮기면서 이건 선택이 아니라 필수가 됐다 — Fargate는 로컬 디스크가
# 휘발성이라 Caddy가 받아둔 인증서가 배포마다 사라지고, 재발급을 반복하면
# Let's Encrypt의 주당 5회 실패 한도에 걸려 사이트가 평문으로 떨어진다.
# ACM 인증서는 AWS가 보관하고 자동 갱신한다.

resource "aws_security_group" "alb" {
  name        = "show-gi-alb"
  description = "show-gi: public HTTP/HTTPS"
  vpc_id      = data.aws_vpc.default.id
}

# HTTPS로 넘기기 위한 리다이렉트를 받는다.
# description에는 ASCII만 넣는다 — AWS가 허용하는 문자 집합이 정해져 있어
# 한글을 넣으면 InvalidParameterValue로 규칙 생성이 실패한다.
resource "aws_vpc_security_group_ingress_rule" "alb_http" {
  security_group_id = aws_security_group.alb.id
  description       = "redirect to HTTPS"
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_ingress_rule" "alb_https" {
  security_group_id = aws_security_group.alb.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "alb_to_task" {
  security_group_id            = aws_security_group.alb.id
  referenced_security_group_id = aws_security_group.task.id
  from_port                    = 80
  to_port                      = 80
  ip_protocol                  = "tcp"
}

# AZ 수만큼 퍼블릭 IPv4가 과금되므로 ALB의 하한인 2개만 쓴다.
# sort는 data 소스의 순서가 보장되지 않아 plan을 결정적으로 만들기 위한 것
locals {
  alb_subnet_ids = slice(sort(data.aws_subnets.default.ids), 0, 2)
}

resource "aws_lb" "main" {
  name               = "show-gi"
  load_balancer_type = "application"
  subnets            = local.alb_subnet_ids
  security_groups    = [aws_security_group.alb.id]

  # 대국은 WebSocket 하나로 오래 열려 있고, 플레이어가 한 수를 몇 분씩 고민한다.
  # 기본값 60초를 그대로 두면 그 사이 연결이 끊긴다 — 이 한 줄이 그 방어다.
  idle_timeout = 900
}

resource "aws_lb_target_group" "web" {
  name        = "show-gi-web"
  port        = 80
  protocol    = "HTTP"
  vpc_id      = data.aws_vpc.default.id
  target_type = "ip" # awsvpc 모드의 Fargate 태스크는 IP로 등록된다

  health_check {
    path = "/healthz"
    # web(Caddy)이 아니라 **api까지 닿는 경로**를 본다. Caddy만 살아 있고 api가
    # 죽은 상태를 "정상"으로 보면, 배포가 성공한 척하고 끝난다
    matcher             = "200"
    interval            = 15
    timeout             = 5
    healthy_threshold   = 2
    unhealthy_threshold = 3
  }

  # 배포 시 옛 태스크가 진행 중인 요청을 마치도록 기다린다
  deregistration_delay = 30
}

# ─── 인증서 ─────────────────────────────────────────────────

resource "aws_acm_certificate" "main" {
  domain_name               = var.domain
  subject_alternative_names = ["www.${var.domain}"]
  validation_method         = "DNS"

  lifecycle {
    create_before_destroy = true
  }
}

# 검증 레코드를 Route53에 자동으로 넣는다. 사람이 콘솔에서 복사하지 않는다
resource "aws_route53_record" "cert_validation" {
  for_each = {
    for o in aws_acm_certificate.main.domain_validation_options : o.domain_name => {
      name   = o.resource_record_name
      record = o.resource_record_value
      type   = o.resource_record_type
    }
  }

  zone_id         = data.aws_route53_zone.main.zone_id
  name            = each.value.name
  type            = each.value.type
  records         = [each.value.record]
  ttl             = 60
  allow_overwrite = true
}

resource "aws_acm_certificate_validation" "main" {
  certificate_arn         = aws_acm_certificate.main.arn
  validation_record_fqdns = [for r in aws_route53_record.cert_validation : r.fqdn]
}

# ─── 리스너 ─────────────────────────────────────────────────

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.main.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = "redirect"
    redirect {
      port        = "443"
      protocol    = "HTTPS"
      status_code = "HTTP_301"
    }
  }
}

resource "aws_lb_listener" "https" {
  load_balancer_arn = aws_lb.main.arn
  port              = 443
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-TLS13-1-2-2021-06" # TLS 1.2 미만 거부
  certificate_arn   = aws_acm_certificate_validation.main.certificate_arn

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.web.arn
  }
}

# ─── DNS ────────────────────────────────────────────────────

data "aws_route53_zone" "main" {
  name         = "${var.domain}."
  private_zone = false
}

# EIP가 아니라 ALB를 가리키는 별칭 레코드다. ALB의 IP는 바뀌므로 A 레코드에
# 주소를 박으면 언젠가 조용히 끊긴다
resource "aws_route53_record" "apex" {
  zone_id = data.aws_route53_zone.main.zone_id
  name    = var.domain
  type    = "A"

  alias {
    name                   = aws_lb.main.dns_name
    zone_id                = aws_lb.main.zone_id
    evaluate_target_health = true
  }
}

resource "aws_route53_record" "www" {
  zone_id = data.aws_route53_zone.main.zone_id
  name    = "www.${var.domain}"
  type    = "A"

  alias {
    name                   = aws_lb.main.dns_name
    zone_id                = aws_lb.main.zone_id
    evaluate_target_health = true
  }
}

output "alb_dns" {
  value = aws_lb.main.dns_name
}
