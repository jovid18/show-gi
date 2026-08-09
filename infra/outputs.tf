output "url" {
  value = "https://${var.domain}"
}

output "shell" {
  description = "컨테이너에 셸로 들어가기 (SSH도 배스천도 없다)"
  value       = "aws ecs execute-command --cluster show-gi --container api --interactive --command /bin/sh --region ${var.aws_region} --profile ${var.aws_profile} --task <task-id>"
}

output "logs" {
  value = "aws logs tail /ecs/show-gi --follow --region ${var.aws_region} --profile ${var.aws_profile}"
}

output "db_psql" {
  description = <<-EOT
    노트북에서 프로덕션 DB에 붙는 명령.

    **접속 문자열을 여기 값으로 내보내지 않는다** — 비밀번호가 들어가면 `terraform output`
    한 번이 터미널 기록과 스크롤백에 남는다. 대신 그때그때 SSM에서 꺼내 쓴다.
  EOT
  value       = "psql \"$(aws ssm get-parameter --name /show-gi/prod/DATABASE_URL --with-decryption --region ${var.aws_region} --profile ${var.aws_profile} --query Parameter.Value --output text)\""
}
