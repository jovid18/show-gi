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
