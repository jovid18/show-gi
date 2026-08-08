#!/bin/bash
# 부팅 시 한 번 실행된다. 애플리케이션은 여기서 배포하지 않는다 —
# 도커와 도구만 깔고, 코드는 사람이 deploy/README.md의 절차로 올린다.
#
# 로그: /var/log/cloud-init-output.log
set -euxo pipefail

dnf update -y
dnf install -y docker git

# compose v2는 dnf에 없다. 플러그인 디렉터리에 직접 넣는다
ARCH="$(uname -m)"
COMPOSE_VERSION="v2.40.0"
mkdir -p /usr/local/lib/docker/cli-plugins
curl -fsSL \
	"https://github.com/docker/compose/releases/download/${COMPOSE_VERSION}/docker-compose-linux-${ARCH}" \
	-o /usr/local/lib/docker/cli-plugins/docker-compose
chmod +x /usr/local/lib/docker/cli-plugins/docker-compose

systemctl enable --now docker

# SSM으로 들어오면 ssm-user가 된다. sudo 없이 docker를 쓸 수 있게 한다
usermod -aG docker ec2-user || true
usermod -aG docker ssm-user || true

# 배포 스크립트가 여기서 돈다. 퍼블릭 레포라 자격증명이 필요 없다.
# 첫 배포가 이 클론에 의존하므로 부팅 시 미리 받아둔다 — CI는 checkout만 한다.
if [ ! -d /opt/show-gi/.git ]; then
	git clone --quiet https://github.com/jovid18/show-gi.git /opt/show-gi
fi
chown -R ec2-user:ec2-user /opt/show-gi

# 엔진 3개가 동시에 도는 동안 postgres가 메모리를 못 잡으면 OOM 킬러가
# 엉뚱한 프로세스를 죽인다. 스왑을 조금 둬서 완충한다
if [ ! -f /swapfile ]; then
	dd if=/dev/zero of=/swapfile bs=1M count=2048
	chmod 600 /swapfile
	mkswap /swapfile
	swapon /swapfile
	echo '/swapfile none swap sw 0 0' >>/etc/fstab
fi
