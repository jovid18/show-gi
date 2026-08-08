output "public_ip" {
  description = "EIP. Route53 A 레코드가 이 주소를 가리킨다"
  value       = aws_eip.app.public_ip
}

output "instance_id" {
  description = "SSM 접속에 쓴다"
  value       = aws_instance.app.id
}

output "connect" {
  description = "인스턴스에 들어가는 명령 (SSH 포트는 열려 있지 않다)"
  value       = "aws ssm start-session --target ${aws_instance.app.id} --region ${var.aws_region} --profile ${var.aws_profile}"
}

output "url" {
  value = "https://${var.domain}"
}
