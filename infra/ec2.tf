# ECS 의 용량 공급. 시작 템플릿은 하나이고 그룹이 둘이다. 티어마다 한 그룹이고 그 위에
# 태스크가 하나씩 뜬다(아래 asg_tiers).

# ECS 최적화 AMI. arm64 를 고른다. 엔진이 arm64 Debian 바이너리라 x86 에서는 돌지 않는다.
# SSM 파라미터라 AWS 가 갱신하면 다음 apply 가 새 AMI 를 집는다.
data "aws_ssm_parameter" "ecs_ami" {
  name = "/aws/service/ecs/optimized-ami/amazon-linux-2023/arm64/recommended/image_id"
}

# ─── 인스턴스 IAM ───────────────────────────────────────────

# 태스크 롤과 다른 역할이다. 이쪽은 ECS 에이전트가 인스턴스를 클러스터에 등록하고 태스크를
# 받아오는 데 쓴다. 섞으면 컨테이너가 클러스터를 조작할 수 있게 된다.
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

# aws ecs execute-command 로 컨테이너에 들어가는 통로다. host 모드라 그 세션이 인스턴스를
# 거치므로 태스크 롤의 ssmmessages 만으로는 모자라다.
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

  # 태스크 보안그룹을 그대로 쓴다. host 모드라 태스크가 인스턴스의 ENI 로 나가므로 필요한
  # 규칙이 정확히 같다. 새로 만들면 RDS 와 ALB 의 규칙을 양쪽 다 고쳐야 한다.
  vpc_security_group_ids = [aws_security_group.task.id]

  # 스팟을 여기서 켜지 않는다. ASG 의 mixed_instances_policy 가 구매 방식을 쥐고 있어서
  # 둘을 같이 적으면 기동이 거절된다. 대국 중에 회수되면 그 판은 aborted 로 닫히고
  # 이어하기가 살린다(journal §51).

  # 크레딧은 T 계열에만 있는 개념이라 다른 계열에 이 블록을 주면 기동이 거절된다.
  #
  # standard 다. unlimited 은 크레딧을 넘긴 만큼 vCPU 시간당 따로 청구되는데 엔진 탐색이
  # 그것을 태우기 쉽다. 대신 크레딧이 마르면 baseline 으로 조여서 탐색이 열 배 넘게
  # 느려진다(journal §108).
  dynamic "credit_specification" {
    for_each = startswith(var.instance_type, "t") ? [1] : []

    content {
      cpu_credits = "standard"
    }
  }

  # 루트 볼륨. AMI 스냅샷보다 작게 줄 수 없고, AL2023 ECS AMI 가 30 GiB 라 그것이 하한이다.
  block_device_mappings {
    device_name = "/dev/xvda"

    ebs {
      volume_size           = 30
      volume_type           = "gp3"
      encrypted             = true
      delete_on_termination = true
    }
  }

  # IMDSv2 만 받는다. v1 은 SSRF 하나로 인스턴스 자격증명이 새는 통로이고, 이 인스턴스의
  # 역할이 ECS 클러스터를 조작할 수 있다.
  metadata_options {
    http_tokens                 = "required"
    http_endpoint               = "enabled"
    http_put_response_hop_limit = 2 # 컨테이너에서도 닿아야 한다(host 모드라 홉이 하나 더다)
  }

  # 에이전트에게 어느 클러스터인지 알려준다. 없으면 인스턴스는 뜨지만 클러스터에 등록되지
  # 않아 서비스가 태스크를 띄우지 못한다. 두 번째 줄이 켜는 스팟 드레이닝은 2분 전 통보에
  # 태스크를 내려서, 회수 순간에 오는 요청이 죽은 인스턴스로 가지 않게 한다.
  user_data = base64encode(<<-EOT
    #!/bin/bash
    cat >> /etc/ecs/ecs.config <<'CFG'
    ECS_CLUSTER=${aws_ecs_cluster.main.name}
    ECS_ENABLE_SPOT_INSTANCE_DRAINING=true
    CFG
  EOT
  )

  # ASG 가 만드는 자원에는 시작 템플릿의 이 블록으로만 태그가 전달된다. 인스턴스와 볼륨에도
  # 붙어야 프로바이더의 default_tags 로 골라낼 수 있다.
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

# 그룹이 둘이고 가르는 것은 티어다(ecs.tf 의 SERVER_ROLE). 시작 템플릿도 클러스터도 같고,
# 다른 것은 「몇 대까지 가나」 하나다.
#
#   show-gi            상호작용.  1/1/1 고정
#   show-gi-analysis   분석.      0 ~ var.analysis_max_instances
#
# 분석 쪽 아래가 0 인 것이 절약 모드다(journal §125). 부하가 없으면 그 대가 뜨지 않고,
# 밀린 手는 상호작용 대가 겸해서 집는다(ecs.tf 의 SERVER_ROLE). 늘리는 것은 EC2 다.
# 한 인스턴스에 api 태스크가 하나뿐이라 그렇다(ecs.tf).
#
# 상호작용 쪽은 늘리지 않는다. 방이 짝지은 프로세스의 메모리에 있어서(journal §98) 두 대면
# 초대·매칭이 절반 확률로 깨진다.
#
# for_each 로 묶은 것은 대조 때문이다. 두 그룹의 구매 정책·타입 후보가 갈리면 「분석 2대」
# 시험이 상호작용 시험과 다른 박스에서 돈 것이 되어 용량표에 적을 수 없다.
locals {
  asg_tiers = {
    interactive = { name = "show-gi", min = 1, max = 1 }
    analysis    = { name = "show-gi-analysis", min = 0, max = var.analysis_max_instances }
  }
}

resource "aws_autoscaling_group" "tier" {
  for_each = local.asg_tiers

  name                = each.value.name
  min_size            = each.value.min
  max_size            = each.value.max
  vpc_zone_identifier = local.alb_subnet_ids

  # desired_capacity 를 적지 않는다. 분석 쪽은 ECS 용량 공급자가 이 값을 움직이므로
  # (ecs.tf) terraform 이 되돌리면 스케일 아웃이 다음 apply 에 취소된다.

  # ALB 가 켜진 AZ 에만 둔다(local.alb_subnet_ids). ALB 는 활성 AZ 의 타깃에만 라우팅하므로
  # 세 번째 서브넷에 뜨면 인스턴스는 정상인데 사이트가 503 이다.
  #
  # 분석 티어는 ALB 뒤에 없지만 같은 서브넷을 쓴다. 시험의 값이 AZ 간 RDS 왕복에 흔들리지
  # 않아야 상호작용 대와 대조할 수 있다.

  # 타입 여럿을 후보로 준다. 스팟 풀이 「타입 × AZ」 이므로 하나만 쓰면 그 풀이 마르는 순간
  # 회수와 대체 실패가 같이 온다(journal §109).
  mixed_instances_policy {
    instances_distribution {
      # 값과 되돌릴 조건은 variables.tf 의 on_demand_base_capacity 에 있다.
      on_demand_base_capacity = var.on_demand_base_capacity

      # base 를 넘는 대는 base 를 따라간다. 분석 티어가 두 대일 때 한 대만 스팟이면 회수
      # 하나가 그 시험의 절반을 가져간다(journal §109).
      on_demand_percentage_above_base_capacity = var.on_demand_base_capacity > 0 ? 100 : 0

      # 온디맨드는 override 순서대로 고른다. 시험이 어느 클래스에서 돌았는지가 용량표의
      # 행을 정하므로 값이 재고에 따라 흔들리면 안 된다.
      on_demand_allocation_strategy = "prioritized"

      # 스팟은 재고가 깊은 풀을 고르되 순서를 힌트로 쓴다.
      spot_allocation_strategy = "capacity-optimized-prioritized"
    }

    launch_template {
      launch_template_specification {
        launch_template_id = aws_launch_template.app.id
        version            = "$Latest"
      }

      dynamic "override" {
        for_each = concat([var.instance_type], var.instance_type_fallbacks)

        content {
          instance_type = override.value
        }
      }
    }
  }

  # ALB 헬스체크를 보지 않는다(EC2 가 기본값이다). 태스크가 배포 중에 잠깐 내려가는데
  # (ecs.tf 의 deployment_maximum_percent) ALB 기준으로 보면 ASG 가 그것을 인스턴스 고장으로
  # 읽고 멀쩡한 인스턴스를 죽인다.
  #
  # 앱 상태는 누구도 보지 않는다. 앱이 죽어도 인스턴스는 교체되지 않는다.

  # 스팟이 회수된 뒤 새 인스턴스가 ECS에 등록되고 태스크를 받는 데 시간이 걸린다.
  health_check_grace_period = 180

  # 대수를 지표로 내보낸다. 켜지 않으면 AWS/AutoScaling 계열이 아예 나오지 않아 「밀린 手가
  # 대수를 움직였다」를 한 화면에 그릴 수 없다(dashboard.tf). ECS 쪽 태스크 수는 Container
  # Insights 가 유료다.
  metrics_granularity = "1Minute"
  enabled_metrics     = ["GroupDesiredCapacity", "GroupInServiceInstances"]

  # 콘솔의 인스턴스 목록에서 티어가 갈려야 한다. 시작 템플릿의 같은 태그를 덮는다.
  tag {
    key                 = "Name"
    value               = each.value.name
    propagate_at_launch = true
  }

  lifecycle {
    # 분석 쪽 desired 의 주인이 ECS 다. 두 그룹이 한 resource 라 상호작용 쪽만 빼 둘 수
    # 없고, 그쪽은 min=max=1 이라 무시해도 값이 하나뿐이다.
    ignore_changes = [desired_capacity]
  }
}

# 자원 주소가 바뀌었다. 옮기지 않으면 terraform 이 도는 그룹을 지우고 다시 만들고,
# 인스턴스가 한 대뿐이라 그 사이가 그대로 장애다.
moved {
  from = aws_autoscaling_group.app
  to   = aws_autoscaling_group.tier["interactive"]
}

output "instance_type" {
  value = var.instance_type
}
