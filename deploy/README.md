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

## 0. 한 번만 (부트스트랩) — **완료됨**

아래는 이미 만들어져 있다. 기록으로 남기는 것이고, 계정을 새로 판다면 이 순서대로 하면 된다.

### IAM

`show-gi-operator` 사용자와 같은 이름의 **관리형 정책**. 정책 문서는 [`infra/iam-policy.json`](../infra/iam-policy.json)에 있다.

```sh
aws iam create-user --user-name show-gi-operator
aws iam create-policy --policy-name show-gi-operator \
  --policy-document file://infra/iam-policy.json
aws iam attach-user-policy --user-name show-gi-operator \
  --policy-arn arn:aws:iam::058264445568:policy/show-gi-operator
```

> **인라인 정책(`put-user-policy`)으로는 안 된다.** 사용자 인라인 정책은 2048바이트가 상한이고 이 정책은 그보다 크다. 관리형 정책은 6144자까지 되고 버전 관리도 된다.
>
> 그리고 정책 JSON에 `Comment` 같은 임의 키를 넣으면 거부된다. IAM 문법은 `Version`·`Id`·`Statement`만 허용한다.

정책이 실제로 막는 것 (확인함):

| 시도                                      | 결과                                                       |
| ----------------------------------------- | ---------------------------------------------------------- |
| 도쿄 EC2 조회                             | 통과                                                       |
| **서울** EC2 조회                         | `UnauthorizedOperation` — 리전 조건에 걸린다               |
| 계정 전체 S3 버킷 목록                    | `AccessDenied` — 다른 프로젝트가 안 보인다                 |
| show-gi 역할에 `AdministratorAccess` 부착 | 거부 — `AttachRolePolicy`가 SSM Core 정책 하나로 묶여 있다 |

마지막 줄이 요점이다. 역할에 아무 정책이나 붙일 수 있으면 최소권한이 의미가 없다 — 그 역할을 EC2에 넘겨 인스턴스에서 관리자 권한을 쓸 수 있기 때문이다. `PassRole`도 `ec2.amazonaws.com`으로만 제한했다.

액세스 키는 `~/.aws/credentials`의 `[show-gi]` 프로파일에 있다. **키를 레포에 커밋하거나 채팅에 붙여넣지 않는다.**

> 기본 프로파일의 리전이 서울(`ap-northeast-2`)이라, 프로파일을 빠뜨리면 **자원이 조용히 서울에 생긴다.** 프로파일에 리전을 박아두는 것이 그 방어이고, `backend.tf`에도 프로파일을 한 번 더 적은 이유가 같다 — 백엔드는 `variables.tf`를 읽지 못한다.

### state 버킷과 잠금 테이블

버킷은 버전 관리·기본 암호화·퍼블릭 접근 차단을 켰다. 버전 관리는 잘못된 apply로 state가 깨졌을 때 되돌릴 수 있는 유일한 수단이다.

```sh
B=show-gi-terraform-state-058264445568
aws s3api create-bucket --bucket $B --region ap-northeast-1 \
  --create-bucket-configuration LocationConstraint=ap-northeast-1
aws s3api put-bucket-versioning --bucket $B --versioning-configuration Status=Enabled
aws s3api put-public-access-block --bucket $B --public-access-block-configuration \
  BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true
aws s3api put-bucket-encryption --bucket $B --server-side-encryption-configuration \
  '{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}'

aws dynamodb create-table --table-name show-gi-terraform-lock \
  --attribute-definitions AttributeName=LockID,AttributeType=S \
  --key-schema AttributeName=LockID,KeyType=HASH \
  --billing-mode PAY_PER_REQUEST --region ap-northeast-1
```

**부트스트랩은 관리자 자격으로 한다.** `show-gi-operator`에게는 버킷을 만들 권한이 없다 — 자기가 쓸 state 저장소를 자기가 만들 수 있으면 그건 최소권한이 아니다.

### 도메인

`show-gi.com` 등록 완료. 호스팅 존(`Z00883671JMPHNULHF2GS`)이 자동으로 생겼다. **따로 만들지 말 것** — 존이 둘이면 NS가 갈려서 도메인이 뜨지 않는다.

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
