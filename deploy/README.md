# 배포 런북

도쿄(`ap-northeast-1`) ECS. ALB가 TLS를 끝내고, 태스크 안의 Caddy가 정적 파일을 서빙하며 api로 프록시한다. 구조와 그 선택의 근거는 [docs/02-architecture.md](../docs/02-architecture.md).

```
Route53 (show-gi.com) → ALB (ACM TLS) → EC2 스팟 1대 (t4g.small / ARM64)
                                          └ ECS 태스크 (network_mode: host)
                                              ├ web  :80    Caddy — 정적 + /api·/ws 프록시
                                              └ api  :8080  Go + 엔진 동봉
                                        → RDS postgres 17 (비공개)
```

**여전히 서버에 들어가지 않는다.** 22번 포트도 없고 컨테이너에는 ECS Exec으로 들어간다 — 인스턴스가 돌아왔지만 그것을 손으로 준비하는 일은 시작 템플릿(`infra/ec2.tf`)이 맡는다.

> **스팟이라 회수될 수 있다.** 2분 전 통보를 받으면 ECS 에이전트가 태스크를 내리고(`ECS_ENABLE_SPOT_INSTANCE_DRAINING`) ASG가 새 인스턴스를 받는다. 그 사이 **2~3분 사이트가 503**이고, 대국 중이었으면 그 판은 `aborted` 로 닫혀 이어하기에 걸린다.

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

마지막 줄이 요점이다. 역할에 아무 정책이나 붙일 수 있으면 최소권한이 의미가 없다 — 그 역할을 태스크에 넘겨 컨테이너 안에서 관리자 권한을 쓸 수 있기 때문이다. `PassRole`도 `ecs-tasks.amazonaws.com`으로만 제한했다.

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

ACM 인증서 검증과 RDS 생성 때문에 10분쯤 걸린다. `aws_acm_certificate_validation`에서 오래 멈춰 있으면 DNS 전파를 기다리는 중이다.

**파라미터를 먼저 등록해야 한다(§2).** 태스크 정의가 `/show-gi/prod/*`를 `secrets`로 참조하므로, 없으면 태스크가 시작조차 못 한다.

## 2. 환경변수 등록 (한 번, 값이 바뀔 때마다)

배포용 값은 **SSM Parameter Store**에 둔다. ECS 태스크 정의가 `secrets`로 참조해 컨테이너에 직접 주입하므로, 값이 어느 디스크에도 남지 않고 로그에도 찍히지 않는다.

Secrets Manager를 쓰지 않은 이유는 단순하다 — 우리에게 필요한 건 로테이션이 아니라 보관이고, Parameter Store의 표준 파라미터는 무료다.

```sh
P=/show-gi/prod

# 비밀 아님
aws ssm put-parameter --name $P/SITE_ADDRESS --type String --value show-gi.com --overwrite
aws ssm put-parameter --name $P/ACME_EMAIL   --type String --value '<주소>'      --overwrite
aws ssm put-parameter --name $P/REGISTRY     --type String \
  --value 058264445568.dkr.ecr.ap-northeast-1.amazonaws.com --overwrite
aws ssm put-parameter --name $P/IMAGE_TAG    --type String --value latest --overwrite

# 비밀 — SecureString으로. 셸 히스토리에 남지 않게 값은 파일이나 stdin으로 넣는다
aws ssm put-parameter --name $P/POSTGRES_PASSWORD --type SecureString \
  --value "$(openssl rand -base64 24)" --overwrite
aws ssm put-parameter --name $P/SESSION_SECRET --type SecureString \
  --value "$(openssl rand -base64 32)" --overwrite
aws ssm put-parameter --name $P/GOOGLE_CLIENT_ID     --type String       --value '<id>'     --overwrite
aws ssm put-parameter --name $P/GOOGLE_CLIENT_SECRET --type SecureString --value '<secret>' --overwrite
```

> `POSTGRES_PASSWORD`를 반드시 넣는다. 안 넣으면 compose의 기본값(`showgi`)이 쓰이는데 그건 **퍼블릭 레포에 적혀 있는 값**이다. 5432는 루프백에만 열려 있지만, 기본값으로 운영하지 않는다.

## 3. 배포

**사람이 서버에 들어가지 않는다.** main에 머지되면 GitHub Actions가 이미지를 굽고, 새 태스크 정의 리비전을 등록해 ECS 서비스를 굴린다.

```
main 머지 → 이미지 빌드(arm64) → ECR push → 태스크 정의 새 리비전 → 롤링 배포 → 안정될 때까지 대기
```

**배포 스크립트가 없다.** 예전 EC2 구성에서는 60줄짜리 셸이 체크아웃·비밀 로드·ECR 로그인·pull·헬스체크를 손으로 했는데, 그 일이 전부 ECS의 기본 동작으로 대체됐다. 직접 쓴 것만 직접 유지보수해야 한다.

| 예전에 스크립트가 하던 일       | 지금                           |
| ------------------------------- | ------------------------------ |
| Parameter Store → 셸 → 컨테이너 | 태스크 정의의 `secrets`        |
| ECR 로그인                      | 실행 역할이 처리               |
| 헬스체크 루프                   | ALB 타깃 그룹 헬스체크         |
| 실패 시 수동 복구               | 배포 서킷 브레이커가 자동 롤백 |
| 인증서 발급·보관                | ACM                            |

### 되돌리기

```sh
aws ecs list-task-definitions --family-prefix show-gi --sort DESC --max-items 5 \
  --region ap-northeast-1 --profile show-gi

aws ecs update-service --cluster show-gi --service show-gi \
  --task-definition show-gi:<리비전> --region ap-northeast-1 --profile show-gi
```

### 들여다보기

```sh
aws logs tail /ecs/show-gi --follow --region ap-northeast-1 --profile show-gi

# 컨테이너 안에서 셸. SSH도 배스천도 없다
aws ecs execute-command --cluster show-gi --container api \
  --interactive --command /bin/sh --task <task-id> \
  --region ap-northeast-1 --profile show-gi
```

## 4. 스키마 변경

**DDL은 배포가 하지 않는다.** 사람이 DB 클라이언트로 직접 넣는다.

배포 스크립트가 테이블을 바꾸기 시작하면 되돌릴 수 없는 변경이 자동으로 실행된다 — 컬럼 삭제 한 줄이 머지되는 순간 데이터가 사라지고, 그때 롤백할 수 있는 것은 코드뿐이다. 스키마는 코드보다 수명이 길고, 그래서 사람이 본다.

`apps/server/internal/store/migrations/*.sql`이 **정본**이다. 손으로 넣더라도 넣은 것과 같은 내용이 레포에 있어야 한다 — 새 환경을 세울 때, 그리고 코드 생성기가 읽을 때 이 파일들이 기준이 된다.

### 절차 — 다섯 줄

|     |                                                                                                                |
| --- | -------------------------------------------------------------------------------------------------------------- |
| 1   | 질의를 `apps/server/internal/store/migrations/NNN_이름.sql` 에 넣고 **PR로 올린다.** 실행 기록이 곧 히스토리다 |
| 2   | **실행은 DB 클라이언트로 직접 한다** (DataGrip 등, [아래](#스키마를-넣는-법--노트북에서-직접) 접속 정보)       |
| 3   | 그 PR에는 **`migration` 라벨**이 붙는다 — 경로를 보고 자동으로 붙으므로 손댈 것이 없다                         |
| 4   | **PR 본문에 실행해야 할 파일명을 적는다.** 파일명만이면 된다                                                   |
| 5   | 순서는 **스키마 먼저, 머지 나중**                                                                              |

번호는 **적용 순서**다. 파일 이름이 곧 정렬 순서이므로 별도의 순서표를 두지 않는다.

4번이 필요한 이유는, PR을 나중에 다시 볼 때 **"이 변경이 DB를 건드렸는가"를 diff로 찾게 하지 않기 위해서**다. 라벨이 있다는 것과 무엇을 돌려야 했는지는 다른 정보다.

> **어느 마이그레이션이 적용됐는지 기록하는 곳이 없다.** 지금은 파일이 몇 개뿐이라 사람이 기억하면 되고, 이미 적용된 것을 다시 돌리면 시끄럽게 실패하므로(`relation ... already exists`) 사고로 이어지지는 않는다. 파일이 늘면 `schema_migrations` 테이블이 필요하다. **[미확정]**

### 컨테이너 안에서 돌리지 않는다 — 넣지 말 것

**지금 그런 코드는 없다.** 확인한 자리는 넷이고, 새로 넣지 않는 한 계속 없다.

| 자리                           | 상태                                                                 |
| ------------------------------ | -------------------------------------------------------------------- |
| `apps/server/Dockerfile`       | `CMD ["api", …]` — 바이너리 직행. 진입 스크립트도 `.sql` 복사도 없다 |
| `cmd/api/main.go`              | DB는 `Open` → `Ping` 뿐이다                                          |
| `docker-compose.yml` 의 db     | `/docker-entrypoint-initdb.d` 를 **안 마운트한다** (아래)            |
| `.github/workflows/server.yml` | CI 테스트용 DB에만 적용한다. 프로덕션과 무관                         |

기동 스크립트나 배포 파이프가 마이그레이션을 실행하는 방식은 **앞으로도 쓰지 않는다.**

> **`/docker-entrypoint-initdb.d` 는 우리가 켠 것이 아니라 postgres 이미지의 기본 동작이다.** `docker-entrypoint.sh` 가 거기 있는 `.sql`·`.sh` 를 알파벳 순으로 실행한다. 그래서 「로컬 편하게 하자」고 마이그레이션을 걸어두기 쉬운 자리인데, **데이터 디렉터리가 비어 있을 때만 돈다**(`if [ -z "$DATABASE_ALREADY_EXISTS" ]`).
>
> 새로 clone한 사람은 스키마가 들어가고 **기존 볼륨을 가진 사람은 안 들어가는데 에러도 안 난다.** 「내 컴퓨터에선 되는데」가 정확히 그렇게 만들어진다.

- 되돌릴 수 없는 변경이 **아무도 안 보는 사이에** 실행된다. 컬럼 삭제 한 줄이 머지되는 순간 데이터가 사라지고, 그때 롤백할 수 있는 것은 코드뿐이다
- 태스크가 여러 개면 **같은 DDL이 동시에 여러 번** 돈다
- 실패하면 기동 실패로 나타나서, 스키마 문제인지 앱 문제인지가 로그에서 갈리지 않는다

아래의 일회용 ECS 태스크는 **접근이 막혔을 때의 대비책**으로만 남긴다. 평소 경로가 아니다.

## 엔진이 떴는지 보는 법

`/healthz` 는 **엔진이 없어도 200이다.** 여기서 실패를 내면 ECS가 태스크를 죽이고 재시작을 반복해 사이트 전체가 내려가기 때문이다. 대신 필드로 드러낸다.

```sh
curl -s https://show-gi.com/healthz
# {"ok":true,"engine":true}    ← engine 이 false 면 사이트는 살아 있고 대국만 안 된다
```

배포 워크플로가 마지막에 이 값을 확인하고 false면 실패시킨다. 손으로 볼 때도 여기부터 본다.

## 지표와 알람 보는 법

**지표는 api 컨테이너가 stdout 으로 내는 EMF 한 줄에서 나온다**([§90](../docs/journal/82-100.md)). CloudWatch 가 로그에서 뽑아 `show-gi` 이름 공간에 넣으므로, 만들 자원이 따로 없고 **태스크 정의의 `ENVIRONMENT` 가 그 스위치**다.

> **켜지는 순서가 셋이다.** ① 관리자가 정책 버전을 올린다(아래) ② `terraform apply` — 알람이 생기고 `ENVIRONMENT` 가 든 새 리비전이 등록된다 ③ **다음 배포** — 서비스는 `task_definition` 변경을 무시하므로(`lifecycle.ignore_changes`) apply 만으로는 도는 태스크가 안 바뀐다. CI가 최신 리비전을 `describe` 해서 이미지만 갈아 끼우므로, main 에 무엇이든 머지되거나 `Images` 워크플로를 다시 돌리면 그때부터 지표가 올라온다.
>
> 그래서 **② 직후에는 알람 셋 중 ALB 것만 데이터를 받는다.** 5xx 와 풀 대기는 `notBreaching` 이라 조용히 `OK` 로 있는다 — 그게 정상이다.

```sh
# 지표가 실제로 올라오나 — 콘솔 대신 CLI로
aws cloudwatch list-metrics --namespace show-gi --profile show-gi

# 값을 볼 때는 get-metric-data 다. get-metric-statistics 는 운영자 정책에 없다
# (알람용으로 GetMetricData·ListMetrics 만 줬다 — journal §90)

# 그 줄의 원본(EMF)을 로그에서 본다
aws logs filter-log-events --log-group-name /ecs/show-gi   --filter-pattern '{ $._aws.CloudWatchMetrics[0].Namespace = "show-gi" }'   --max-items 1 --profile show-gi
```

알람이 언제 울렸는지는 이력으로 본다. **정책 버전을 다시 게시하기 전에는 이 조회만 `AccessDenied` 다**(06-status §7).

```sh
aws cloudwatch describe-alarm-history --alarm-name show-gi-no-healthy-target \
  --history-item-type StateUpdate --max-items 5 --profile show-gi --region ap-northeast-1
```

**요청 하나를 되짚을 때는 `request_id` 로 찾는다.** 응답 헤더(`X-Request-Id`)에 실려 나가므로 「이 화면이 이상하다」와 함께 그 값을 받을 수 있다.

```sh
aws logs filter-log-events --log-group-name /ecs/show-gi   --filter-pattern '{ $.request_id = "1a2b3c4d5e6f7a8b" }' --profile show-gi
```

라벨까지 붙은 숫자를 보려면 컨테이너 안에서 텍스트 표면을 읽는다. **밖에서는 안 닿는다** — Caddy 가 `/ws`·`/api`·`/healthz` 만 프록시한다.

```sh
# --container web 이다. api 이미지는 debian-slim + ca-certificates·libgomp1 뿐이라
# wget 도 curl 도 없다(apps/server/Dockerfile). web 은 caddy-alpine 이라 busybox wget 이
# 있고, host 네트워크라 같은 localhost:8080 에 닿는다.
aws ecs execute-command --cluster show-gi --task <task-id> --container web \
  --interactive --command 'wget -qO- localhost:8080/metrics' --profile show-gi
```

알람은 셋이다. **메일을 받으려면 `terraform.tfvars` 에 `alarm_email` 을 적고 apply 한 뒤, 확인 메일의 링크를 한 번 눌러야 한다** — 누르기 전에는 구독이 `pending` 이라 알람이 울려도 안 온다.

| 알람                        | 언제                              | 무엇을 보나                                                                                                |
| --------------------------- | --------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `show-gi-no-healthy-target` | 정상 타깃이 5분 동안 없다         | 사이트가 내려갔다. EMF 와 무관하게 AWS 가 늘 내는 지표다. 5분인 것은 정상 배포·스팟 회수가 그보다 짧아서다 |
| `show-gi-5xx`               | 5분 안에 5xx 나 panic 1건 이상    | 우리 버그. 같은 시각의 `request_id` 로 로그를 찾는다                                                       |
| `show-gi-engine-pool-wait`  | 풀 대기 p95 가 10분 동안 3초 초과 | `ENGINE_POOL_SIZE` 를 올릴 자리다. **임계치는 아직 감이다**                                                |

## 운영자 정책에 로그 읽기·알람 권한 올리기

**운영자는 자기 정책을 못 고친다**(권한 상승 방지). 관리자 자격으로 한 번 올려야 한다.

지금 올려야 하는 것이 둘이다 — 로그 읽기(`logs:GetLogEvents` 등)와 **알람·SNS**(`cloudwatch:PutMetricAlarm`·`sns:*`, [§90](../docs/journal/82-100.md)). 올리기 전에는 `infra/alarms.tf` 의 apply 가 `AccessDenied` 로 끝난다.

```sh
# 버전은 5개가 상한이라, 차면 오래된 것을 지우면서 올린다
aws iam create-policy-version \
  --policy-arn arn:aws:iam::058264445568:policy/show-gi-operator \
  --policy-document file://infra/iam-policy.json \
  --set-as-default --profile <관리자 프로파일>
```

### 스키마를 넣는 법 — 노트북에서 직접

**RDS에 노트북에서 바로 붙는다.** `publicly_accessible = true` 이고, 들어올 수 있는 것은 보안그룹에 등록된 주소(`admin_cidr`)와 태스크 보안그룹뿐이다.

접속 정보를 한 번에 뽑는다. **접속 문자열의 비밀번호는 URL 인코딩되어 있으므로**(`rds.tf` 가 `urlencode` 를 건다) GUI 클라이언트에 넣을 때는 디코딩된 값이 필요하다.

```sh
aws ssm get-parameter --name /show-gi/prod/DATABASE_URL --with-decryption \
  --profile show-gi --region ap-northeast-1 --query Parameter.Value --output text \
| python3 -c "
import sys, urllib.parse as u
p = u.urlsplit(sys.stdin.read().strip())
print('Host    ', p.hostname); print('Port    ', p.port)
print('Database', p.path.lstrip('/')); print('User    ', p.username)
print('Password', u.unquote(p.password))
"
```

`psql` 은 접속 문자열을 그대로 받는다(libpq가 디코딩한다).

```sh
psql "$(aws ssm get-parameter --name /show-gi/prod/DATABASE_URL --with-decryption \
  --profile show-gi --region ap-northeast-1 --query Parameter.Value --output text)" \
  -v ON_ERROR_STOP=1 -f apps/server/internal/store/migrations/002_anonymous_games.sql
```

> **`psql` 이 없어도 된다.** GUI 클라이언트(DataGrip 등)로 붙어 `.sql` 파일을 그대로 실행하면 같은 일이다. SSL은 **필수**다 — 서버가 `rds.force_ssl` 로 강제하므로 끄면 거부당한다.

> **공인 IP가 바뀌면 못 붙는다.** `infra/terraform.tfvars` 의 `admin_cidr` 을 고치고 apply 한다(보안그룹 규칙 하나라 수십 초). 그 파일은 `.gitignore` 가 막는다 — **퍼블릭 레포라 IP를 커밋하면 그대로 공개된다.**

### 그래도 남겨두는 길 — 일회용 태스크

위 통로가 막혔을 때(집 밖에서 작업, IP를 아직 등록 안 함) 쓴다. **다만 조회에는 못 쓴다** — 결과가 CloudWatch로만 나가는데 운영자 정책에 로그 읽기 권한이 없다([상태 문서](../docs/06-status.md) §7). 넣을 수는 있고 볼 수는 없다.

**ECS Exec으로 앱 컨테이너에 들어가는 방법도 있지만 `session-manager-plugin` 설치가 필요하고, 그 설치에는 sudo가 든다.** 아래 방법은 아무것도 안 깔고 되며, 실제로 초기 스키마를 이렇게 넣었다.

`show-gi-migrate` 태스크 정의가 이미 등록돼 있다 — postgres 이미지에 `DATABASE_URL`만 주입된 것이고, 명령은 실행할 때마다 덮어쓴다.

> **이쪽은 Fargate로 남긴다**(아래 `--launch-type FARGATE`). 앱 태스크는 EC2로 옮겼지만, 인스턴스가 앱 태스크 하나에 맞춰진 t4g.small 한 대라 **마이그레이션 태스크를 얹을 자리가 없다** — EC2로 바꾸면 배치가 안 돼서 영원히 `PROVISIONING` 에 머문다. 어차피 몇 초 도는 일회용이라 Fargate 쪽이 값도 거의 0이고, 태스크 정의가 갈려 있으므로 앱을 옮긴 것이 여기에 영향을 주지 않는다.

```sh
SUBNETS=$(aws ec2 describe-subnets --profile show-gi --region ap-northeast-1 \
  --filters "Name=default-for-az,Values=true" --query 'Subnets[].SubnetId' --output text | tr '\t' ',')
SG=$(aws ec2 describe-security-groups --profile show-gi --region ap-northeast-1 \
  --filters "Name=group-name,Values=show-gi-task" --query 'SecurityGroups[0].GroupId' --output text)
# **브랜치 이름을 쓴다.** 「스키마를 먼저, 배포를 나중에」(아래 순서)를 지키려면
# 아직 main에 없는 파일을 받아야 하고, main을 가리키면 그 시점에 404다.
URL='https://raw.githubusercontent.com/jovid18/show-gi/<브랜치>/apps/server/internal/store/migrations/002_something.sql'

aws ecs run-task --cluster show-gi --task-definition show-gi-migrate --launch-type FARGATE \
  --network-configuration "awsvpcConfiguration={subnets=[$SUBNETS],securityGroups=[$SG],assignPublicIp=ENABLED}" \
  --overrides "{\"containerOverrides\":[{\"name\":\"psql\",\"command\":[\"sh\",\"-c\",\"wget -qO /tmp/s.sql '$URL' && psql \\\"\$DATABASE_URL\\\" -v ON_ERROR_STOP=1 -f /tmp/s.sql\"]}]}" \
  --profile show-gi --region ap-northeast-1 --query 'tasks[0].taskArn' --output text
```

SQL을 **퍼블릭 레포의 raw URL에서 받아오는 것**이 요점이다 — 파일을 컨테이너에 넣을 방법을 따로 만들 필요가 없다. 레포가 퍼블릭이라 자격증명도 필요 없다.

결과는 종료 코드로 본다. `ON_ERROR_STOP=1` 때문에 **한 문장이라도 실패하면 0이 아니다.**

```sh
aws ecs describe-tasks --cluster show-gi --tasks <task-arn> \
  --query 'tasks[0].containers[0].exitCode' --region ap-northeast-1 --profile show-gi
```

> `show-gi-migrate` 태스크 정의는 **Terraform 밖에서 등록됐다.** 새 환경을 세울 때는 다시 등록해야 한다 — `docs/06-status.md` §7의 부채 목록에 있다.

조회만 할 때도 같은 방법을 쓴다. 명령의 `wget … && psql -f` 자리를 `psql "$DATABASE_URL" -c '…'`로 바꾸면 된다.

### 순서

스키마를 **먼저**, 배포를 **나중에** 한다. 새 코드가 없는 컬럼을 읽으면 즉시 터지지만, 옛 코드가 새 컬럼을 모르는 것은 아무 일도 아니다.

컬럼을 지울 때는 반대다 — 그 컬럼을 안 쓰는 코드를 먼저 배포하고, 며칠 두고 보다가 지운다.

## 5. 확인

```sh
curl -sI https://show-gi.com | head -3          # 200 + HSTS 헤더
curl -s  https://show-gi.com/healthz            # {"ok":true}
docker compose logs -f web                      # 인증서 발급 로그
```

---

## 자주 물리는 곳

**태스크가 시작하자마자 죽는다.** 대부분 비밀 주입 실패다. `secrets`에 적힌 파라미터가 하나라도 없으면 ECS는 컨테이너를 띄우지 못하고, 그 실패는 애플리케이션 로그가 아니라 **태스크 중지 이유**에 남는다.

```sh
aws ecs describe-tasks --cluster show-gi --tasks <task-id> \
  --query 'tasks[0].{stopped:stoppedReason,containers:containers[].reason}' \
  --region ap-northeast-1 --profile show-gi
```

**타깃이 unhealthy다.** 헬스체크는 `/healthz`인데 이 경로는 web(Caddy)이 api로 프록시한다. 즉 **api가 죽으면 unhealthy가 된다** — 의도한 것이다. 어느 컨테이너가 문제인지는 로그 스트림 접두어(`web` / `api`)로 갈린다.

```sh
aws logs tail /ecs/show-gi --follow --region ap-northeast-1 --profile show-gi
```

**인증서가 안 나온다.** ACM은 DNS 검증이라 Route53에 검증 레코드가 들어가야 한다. Terraform이 자동으로 넣지만, 호스팅 존이 둘이면 엉뚱한 존에 들어가 영원히 검증되지 않는다 — 도메인 등록 시 자동 생성된 존 하나만 있어야 한다.

**WebSocket이 몇 분 뒤 끊긴다.** ALB의 `idle_timeout`이다. 900초로 올려뒀는데, 그보다 오래 생각하는 대국이 있으면 더 올린다.

**스키마를 고쳤는데 반영이 안 된다.** DDL은 배포가 하지 않는다. 사람이 넣는다(§4).

**엔진이 안 뜬다.** `fairy-stockfish`는 데비안에서 `/usr/games`에 깔리고 그 경로는 기본 PATH에 없다. Dockerfile이 PATH를 넣어주고 있으니, 직접 실행해볼 때만 주의하면 된다.

**로컬에서 8080이 안 잡힌다.** `../shogi` 프로젝트 컨테이너가 쓰고 있다. `cd ../shogi && docker compose down`.

---

## 비용과 정리

**해커톤이 끝나고 상시 가동으로 바꿨다**(2026-08-17). 그래서 표를 주 단위에서 **월 단위**로 옮겼다 — 이제 「대회 기간의 비용」이 아니라 「계속 나가는 비용」이다.

|                                     | 월 (추정)     |
| ----------------------------------- | ------------- |
| EC2 t4g.small **스팟** 1대 (상시)   | **~$5**       |
| EBS gp3 30 GiB (그 인스턴스의 루트) | ~$3           |
| ALB                                 | ~$18          |
| RDS db.t4g.micro + 20 GB            | ~$15          |
| ECR, 로그, Parameter Store          | $1 미만       |
| **합계**                            | **~$42 / 월** |

**컴퓨트가 더 이상 가장 큰 항목이 아니다.** 한때 Fargate 4 vCPU / 8 GiB로 월 약 $115였고 그것 때문에 서비스를 0으로 내려 뒀는데, 스팟 한 대로 옮기면서 **컴퓨트가 $8**이 됐다. 지금 표의 8할은 **ALB와 RDS**다(약 $33) — 더 줄일 곳을 찾는다면 컴퓨트가 아니라 그 둘이다.

> **ALB를 없애는 것은 간단하지 않다.** TLS 종료·ACM 자동 갱신·WebSocket 유지가 거기 얹혀 있어서, 빼면 Caddy가 인증서를 다시 맡고 그 인증서를 스팟 인스턴스의 휘발성 디스크에 두게 된다 — 회수될 때마다 재발급이고 Let's Encrypt 한도에 걸린다([alb.tf](../infra/alb.tf) 머리말이 그 이야기다).

**정리할 때는 통째로 지운다.** 켜둔 채 잊으면 위 금액이 계속 나간다.

```sh
cd infra && terraform destroy      # 전부 지운다
```

컴퓨트만 잠깐 끄려면 **`desired_capacity` 를 0으로 내린다** — 태스크가 아니라 인스턴스다. `desired_count` 만 0으로 내리면 인스턴스는 그대로 돌면서 태스크만 없어져서, 돈은 그대로 나가고 사이트만 죽는다.

```sh
# infra/ec2.tf 의 min_size·max_size·desired_capacity 를 0으로 고치고
cd infra && terraform apply
```

> **`aws autoscaling set-desired-capacity` 로 내리지 않는다.** `desired_capacity` 는 terraform 이 주인이라(ec2.tf의 `lifecycle` 주석) 다음 apply 가 코드 값으로 되돌린다 — CLI로 내린 것은 조용히 다시 켜진다.
