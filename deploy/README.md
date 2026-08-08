# 배포 런북

도쿄(`ap-northeast-1`) EC2 한 대에 `docker compose`. Caddy가 TLS를 끝내고 정적 파일을 서빙하며 api로 프록시한다. 구조와 그 선택의 근거는 [docs/02-architecture.md](../docs/02-architecture.md).

```
Route53 (show-gi.com) → EIP → EC2 c7g.xlarge
                                └ docker compose
                                    ├ web (Caddy)  :80 :443   TLS · 정적 · /api·/ws 프록시
                                    ├ api          :8080      Go + fairy-stockfish 동봉
                                    └ db           :5432      postgres 17 + pgvector
```

**22번 포트는 열려 있지 않다.** 인스턴스에는 SSM Session Manager로 들어간다.

---

## 0. 한 번만 (부트스트랩)

### IAM 사용자

관리자 자격으로 콘솔이나 CLI에서 만든다. Terraform이 자기 자신을 만들 수는 없다.

```sh
aws iam create-user --user-name show-gi-operator
aws iam put-user-policy --user-name show-gi-operator \
  --policy-name show-gi-operator --policy-document file://infra/iam-policy.json
aws iam create-access-key --user-name show-gi-operator
```

정책은 [`infra/iam-policy.json`](../infra/iam-policy.json)에 있고, **이 프로젝트가 만드는 자원만** 만질 수 있다. 같은 계정에 다른 프로젝트가 살아 있으므로 관리자 권한을 주지 않는다.

발급받은 키를 `~/.aws/credentials`에 넣는다. **키를 레포에 커밋하거나 대화에 붙여넣지 않는다.**

```ini
[show-gi]
aws_access_key_id = ...
aws_secret_access_key = ...
region = ap-northeast-1
```

> 기본 프로파일의 리전이 서울(`ap-northeast-2`)이라, `--region`이나 프로파일 리전을 빠뜨리면 **자원이 조용히 서울에 생긴다.** 위처럼 프로파일에 리전을 박아두는 것이 그 방어다.

### state 버킷

state를 담을 자원을 state로 관리할 수 없으므로 이것만 손으로 만든다.

```sh
aws s3api create-bucket --bucket show-gi-terraform-state-058264445568 \
  --region ap-northeast-1 --create-bucket-configuration LocationConstraint=ap-northeast-1 \
  --profile show-gi
aws s3api put-bucket-versioning --bucket show-gi-terraform-state-058264445568 \
  --versioning-configuration Status=Enabled --profile show-gi
aws s3api put-public-access-block --bucket show-gi-terraform-state-058264445568 \
  --public-access-block-configuration \
  BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true \
  --profile show-gi
```

버전 관리를 켜는 것은 잘못된 apply로 state가 깨졌을 때 되돌릴 수 있는 유일한 수단이기 때문이다.

잠금 테이블도 만든다. S3 네이티브 잠금이 이걸 없애주지만 Terraform 1.11+가 필요하고 로컬은 1.5.7이다.

```sh
aws dynamodb create-table --table-name show-gi-terraform-lock \
  --attribute-definitions AttributeName=LockID,AttributeType=S \
  --key-schema AttributeName=LockID,KeyType=HASH \
  --billing-mode PAY_PER_REQUEST --region ap-northeast-1 --profile show-gi
```

### 도메인

Route53에서 `show-gi.com`을 등록하면 호스팅 존이 **자동으로 생긴다.** 따로 만들지 말 것 — 존이 둘이면 NS가 갈려서 도메인이 뜨지 않는다.

**등록자 이메일 인증을 반드시 끝낸다.** ICANN 규정이라 15일 안에 인증하지 않으면 도메인이 정지된다.

---

## 1. 인프라 올리기

```sh
cd infra
terraform init
terraform plan
terraform apply
```

`terraform output connect`가 인스턴스 접속 명령을 알려준다.

## 2. 코드 올리고 띄우기

```sh
aws ssm start-session --target <instance-id> --region ap-northeast-1 --profile show-gi

sudo -iu ec2-user
git clone https://github.com/jovid18/show-gi.git && cd show-gi
cp .env.example .env && vi .env      # SITE_ADDRESS, ACME_EMAIL, 키들
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build
```

`session-manager-plugin`이 없다면 [AWS 문서](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html)대로 설치한다. macOS는 `brew install --cask session-manager-plugin`.

## 3. 확인

```sh
curl -sI https://show-gi.com | head -3          # 200 + HSTS 헤더
curl -s  https://show-gi.com/healthz            # {"ok":true}
docker compose logs -f web                      # 인증서 발급 로그
```

---

## 자주 물리는 곳

**인증서가 안 나온다.** Caddy가 `show-gi.com`으로 요청을 받아야 발급이 된다. DNS가 아직 이 서버를 안 가리키거나, 80번 포트가 막혀 있으면 ACME 챌린지가 실패한다. `dig show-gi.com +short`로 EIP와 맞는지 먼저 본다.

**인증서를 반복해서 못 받는다.** Let's Encrypt는 **같은 도메인에 주당 5회** 발급 실패 한도가 있다. `caddy-data` 볼륨을 지우고 재시작을 반복하면 여기 걸린다 — 볼륨은 그대로 두고 로그부터 읽는다.

**스키마를 고쳤는데 반영이 안 된다.** `docker-entrypoint-initdb.d`의 SQL은 **데이터 볼륨이 비어 있을 때만** 실행된다. 개발 중이면 `docker compose down -v`, 운영이면 `002_*.sql`을 새로 만든다.

**엔진이 안 뜬다.** `fairy-stockfish`는 데비안에서 `/usr/games`에 깔리고 그 경로는 기본 PATH에 없다. Dockerfile이 PATH를 넣어주고 있으니, 직접 실행해볼 때만 주의하면 된다.

---

## 비용과 정리

|                     |                        |
| ------------------- | ---------------------- |
| c7g.xlarge 24시간   | 주 **$28** 내외 (추정) |
| EBS 30GB + EIP + 존 | 주 $2 미만             |
| 도메인              | $16/년                 |

**대회가 끝나면 반드시 정리한다.** c7g.xlarge를 켜둔 채 잊으면 **월 $120**이 계속 나간다.

```sh
cd infra && terraform destroy     # 전부 지운다
# 또는 잠시 멈추기만:
aws ec2 stop-instances --instance-ids <id> --region ap-northeast-1 --profile show-gi
```

개발 중에도 밤에 인스턴스를 멈추면 컴퓨트 요금이 멈춘다(EBS·EIP는 남는다). 다만 데모 영상을 찍기 시작하는 D5부터는 계속 켜두는 편이 안전하다.
