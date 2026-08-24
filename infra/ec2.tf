# ECS의 용량 공급. Fargate에서 EC2 스팟 한 대로 옮긴 자리다.
#
# 태스크는 4 vCPU / 8 GiB였고 Fargate에서 24시간 돌리면 월 $115다. 쓰는 사람이 한 명인
# 서비스에 그 값을 낼 이유가 없어서 desired_count 가 0으로 내려가 있었는데, 0이면
# 배포 워크플로의 헬스체크가 매 머지마다 빨간불이 된다 — 상시 빨간 CI는 안 보는 CI다.
#
# t4g.small 스팟 한 대면 월 ~$8이고, 그 위에 태스크가 늘 하나 떠 있다.

# ECS 최적화 AMI. arm64를 고른다 — 엔진이 arm64 Debian 바이너리라 x86에서는 안 돈다
# (CI도 arm64로 굽는다). SSM 파라미터라 AWS가 갱신하면 다음 apply가 새 AMI를 집는다.
data "aws_ssm_parameter" "ecs_ami" {
  name = "/aws/service/ecs/optimized-ami/amazon-linux-2023/arm64/recommended/image_id"
}

# ─── 인스턴스 IAM ───────────────────────────────────────────

# 태스크 롤과 다른 역할이다. 이쪽은 ECS 에이전트가 「이 인스턴스를 클러스터에
# 등록하고 태스크를 받아온다」를 하는 데 쓰고, 태스크 롤은 컨테이너 안의 코드가 쓴다.
# 섞으면 컨테이너가 클러스터를 조작할 수 있게 된다.
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

resource "aws_iam_role_policy_attachment" "instance_ecs" {
  role       = aws_iam_role.instance.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonEC2ContainerServiceforEC2Role"
}

# aws ecs execute-command 로 컨테이너에 들어가는 통로다. host 네트워크 모드라
# 그 세션이 인스턴스를 거치므로, 태스크 롤의 ssmmessages 만으로는 모자라다.
resource "aws_iam_role_policy_attachment" "instance_ssm" {
  role       = aws_iam_role.instance.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "instance" {
  name = "show-gi-instance"
  role = aws_iam_role.instance.name
}

# ─── 시작 템플릿 ────────────────────────────────────────────

resource "aws_launch_template" "app" {
  name_prefix   = "show-gi-"
  image_id      = data.aws_ssm_parameter.ecs_ami.value
  instance_type = var.instance_type

  iam_instance_profile {
    arn = aws_iam_instance_profile.instance.arn
  }

  # 태스크 보안그룹을 그대로 쓴다. host 모드라 태스크가 인스턴스의 ENI로 나가므로
  # 필요한 규칙이 정확히 같다 — ALB에서 80만 들어오고 밖으로는 전부 열려 있다.
  # 새로 만들면 RDS와 ALB의 규칙을 양쪽 다 고쳐야 하고, 그 둘이 어긋나는 것이 더 비싸다.
  vpc_security_group_ids = [aws_security_group.task.id]

  # 스팟. 회수되면 사이트가 내려가는데, 그 길이가 「새 인스턴스를 받아 띄우는 시간」이
  # 아니다 — 재고가 없으면 대체가 안 되고 돌아올 때까지다. 실제로 26분 내려갔다
  # (journal §106). 타입 하나에 AZ 둘이라 풀이 좁은 것이 그 길이를 정한다.
  #
  # 대국 중이었으면 그 판은 aborted 로 닫히고 이어하기가 살린다(journal §51).
  #
  # max_price 를 안 적는다. 적으면 시세가 그 위로 오를 때 인스턴스를 못 받는데,
  # 상한 없는 스팟도 온디맨드 가격을 넘지 않는다 — 상한은 안전장치가 아니라 가동률 손해다.
  instance_market_options {
    market_type = "spot"

    spot_options {
      # 한 대뿐이므로 「중단되면 다시 받는다」가 유일한 전략이다. stop 이나 hibernate 는
      # 그 인스턴스를 되살리려 하고, 그러면 용량이 돌아올 때까지 계속 내려가 있다.
      instance_interruption_behavior = "terminate"
    }
  }

  # T 계열은 크레딧으로 돈다. unlimited 이면 크레딧을 넘긴 만큼 vCPU 시간당 따로
  # 청구되는데, 엔진 탐색이 그것을 태우기 쉬운 모양이다 — 한 수에 CPU를 몰아 쓴다.
  # standard 는 넘기면 느려지는 대신 청구가 예측 가능하다. 한 사람이 쓰는 서비스에서
  # 놀라운 청구서보다 잠깐 느린 탐색이 낫다.
  credit_specification {
    cpu_credits = "standard"
  }

  # 루트 볼륨. AMI 스냅샷보다 작게 못 준다 — AL2023 ECS AMI가 30 GiB라 그것이 하한이다.
  block_device_mappings {
    device_name = "/dev/xvda"

    ebs {
      volume_size           = 30
      volume_type           = "gp3"
      encrypted             = true
      delete_on_termination = true
    }
  }

  # IMDSv2만 받는다. v1은 SSRF 하나로 인스턴스 자격증명이 새는 통로다 —
  # 이 인스턴스의 역할이 ECS 클러스터를 조작할 수 있으므로 그 값이 크다.
  metadata_options {
    http_tokens                 = "required"
    http_endpoint               = "enabled"
    http_put_response_hop_limit = 2 # 컨테이너에서도 닿아야 한다(host 모드라 홉이 하나 더다)
  }

  # 에이전트에게 어느 클러스터인지 알려준다. 이 한 줄이 없으면 인스턴스는 뜨지만
  # 클러스터에 등록되지 않고, 서비스는 「용량이 없다」로 태스크를 못 띄운다.
  #
  # 스팟 드레이닝을 켜는 것이 두 번째 줄이다. 2분 전 통보를 받으면 에이전트가 태스크를
  # 내리고 ALB가 타깃을 빼므로, 회수 순간에 오는 요청이 죽은 인스턴스로 가지 않는다.
  user_data = base64encode(<<-EOT
    #!/bin/bash
    cat >> /etc/ecs/ecs.config <<'CFG'
    ECS_CLUSTER=${aws_ecs_cluster.main.name}
    ECS_ENABLE_SPOT_INSTANCE_DRAINING=true
    CFG
  EOT
  )

  # 태그가 인스턴스와 볼륨에도 붙어야 프로바이더의 default_tags 로 골라낼 수 있다 —
  # ASG가 만드는 자원에는 시작 템플릿의 이 블록으로만 전달된다.
  tag_specifications {
    resource_type = "instance"
    tags          = { Name = "show-gi" }
  }

  tag_specifications {
    resource_type = "volume"
    tags          = { Name = "show-gi" }
  }

  lifecycle {
    create_before_destroy = true
  }
}

# ─── 오토스케일링 그룹 ──────────────────────────────────────

# 스케일링을 하지 않는다. 1/1/1로 고정이고, 이 그룹이 하는 일은 「한 대를 늘 살려
# 둔다」 하나다 — 스팟이 회수되면 대신 새 인스턴스를 받아 온다. 받아 올 재고가 있을
# 때만 그렇다(journal §106).
#
# health_check_type 이 EC2 다. 앱 상태를 아무도 안 본다는 뜻이다 — 앱이 느려지거나
# 죽어도 인스턴스는 교체되지 않고, 이미 종료된 것을 치우는 것이 전부다.
#
# 용량 공급자(capacity provider)를 안 만든다. 그것이 하는 일은 태스크 수를 보고 인스턴스를
# 늘리는 것인데, 여기는 태스크도 인스턴스도 1로 고정이라 늘릴 것이 없다. 서비스가
# launch_type = "EC2" 로 클러스터에 등록된 인스턴스에 바로 얹는다.
resource "aws_autoscaling_group" "app" {
  name                = "show-gi"
  min_size            = 1
  max_size            = 1
  desired_capacity    = 1
  vpc_zone_identifier = local.alb_subnet_ids

  # ALB가 켜진 AZ에만 둔다(local.alb_subnet_ids). ALB는 활성 AZ의 타깃에만
  # 라우팅하므로, 세 번째 서브넷에 뜨면 인스턴스는 정상인데 사이트가 503이다.

  launch_template {
    id      = aws_launch_template.app.id
    version = "$Latest"
  }

  # ALB 헬스체크를 안 본다(EC2 가 기본값이다). 태스크가 배포 중에 잠깐 내려가는데
  # (deployment_maximum_percent = 100), ALB 기준으로 보면 ASG가 그것을 인스턴스 고장으로
  # 읽고 멀쩡한 인스턴스를 죽인다 — 그러면 배포마다 인스턴스가 새로 뜬다.

  # 스팟이 회수된 뒤 새 인스턴스가 ECS에 등록되고 태스크를 받는 데 시간이 걸린다.
  health_check_grace_period = 180

  # ASG가 인스턴스를 갈아치울 때 태그를 새로 다는 대신 시작 템플릿의 것을 쓴다.
  # 여기 tag 블록을 두면 default_tags 와 두 벌이 되어 plan 이 매번 흔들린다.

  lifecycle {
    # desired_capacity 를 무시하지 않는다. 스케일링하는 것이 없으므로 terraform 이
    # 이 값의 유일한 주인이고, 손으로 바꿔도 다음 apply 가 되돌리는 것이 맞다
    # (aws_ecs_service.app.desired_count 와 같은 판단이다).
    create_before_destroy = false
  }
}

output "instance_type" {
  value = var.instance_type
}
