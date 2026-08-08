# 현재 상태 — 어디까지 왔고 무엇이 막혀 있나

**이 문서는 사실 기록이다.** 설계는 다른 문서에 있고, 여기에는 "지금 실제로 어떤 상태인가"만 적는다. 상태가 바뀌면 같은 PR에서 갱신한다. 오래된 상태 문서는 없는 것보다 나쁘다.

- 마지막 갱신: 2026-08-08
- 마감: **2026-08-15 15:00**

---

## 1. 한 줄

**설계와 인프라 코드는 다 썼고, 아직 한 번도 AWS에 올라간 적이 없다.** `terraform apply`와 PR #2 머지가 다음 관문이다.

---

## 2. 레포

|        |                                                                                     |
| ------ | ----------------------------------------------------------------------------------- |
| 브랜치 | `feat/infra` — main보다 8커밋 앞                                                    |
| PR     | [#2](https://github.com/jovid18/show-gi/pull/2) **열림**, mergeable, 체크 전부 통과 |
| main   | `chore: set up monorepo, tooling, and CI` (PR #1 머지됨)                            |

`main`은 룰셋으로 보호되어 있다 — PR 필수, force push·삭제 금지, `server`/`web` 체크 통과 필요, squash 머지만.

> 필수 체크의 이름은 워크플로 이름(`Server`/`Web`)이 아니라 **job 이름(`server`/`web`)**이다. 잘못 넣으면 존재하지 않는 체크를 기다리며 PR이 영원히 머지 불가가 된다.

---

## 3. AWS 실제 상태

계정 `058264445568`, 리전 `ap-northeast-1`(도쿄), 프로파일 `show-gi`.

### 손으로 만든 것 (부트스트랩 — 완료)

Terraform으로 관리하지 않는다. state를 담을 자원을 state로 관리할 수 없고, IAM은 자기 자신을 만들 수 없기 때문이다.

| 자원                                                     | 상태                            |
| -------------------------------------------------------- | ------------------------------- |
| IAM 사용자 `show-gi-operator`                            | ✅                              |
| 관리형 정책 `show-gi-operator`                           | ✅ **v4** (ECS·ELB·ACM 반영본)  |
| S3 `show-gi-terraform-state-058264445568`                | ✅ 버전 관리·암호화·퍼블릭 차단 |
| DynamoDB `show-gi-terraform-lock`                        | ✅                              |
| 도메인 `show-gi.com` + 호스팅 존 `Z00883671JMPHNULHF2GS` | ✅ 등록 완료                    |

### Terraform이 관리할 것 (아직 **아무것도 없음**)

`terraform state list`가 비어 있다. ECS·ALB·ACM·RDS·ECR·OIDC 전부 코드만 있고 생성되지 않았다.

```
terraform -chdir=infra plan   →   40 to add, 0 to change, 0 to destroy
```

### 아직 등록 안 된 파라미터

`/show-gi/prod/*`가 **비어 있다.** ECS 태스크가 이 값들을 읽으므로 apply 전후에 넣어야 첫 배포가 뜬다.

| 이름                   | 타입         | 누가                               |
| ---------------------- | ------------ | ---------------------------------- |
| `DATABASE_URL`         | SecureString | **Terraform이 자동 생성** (rds.tf) |
| `SESSION_SECRET`       | SecureString | 아무나 — `openssl rand -base64 32` |
| `ORCA_API_KEY`         | SecureString | **사람** — OrcaRouter에서 발급     |
| `GOOGLE_CLIENT_ID`     | String       | **사람** — GCP 콘솔                |
| `GOOGLE_CLIENT_SECRET` | SecureString | **사람**                           |

> 태스크 정의가 이 다섯 개를 `secrets`로 참조한다. **없으면 태스크가 시작하지 못한다** — 값을 비워두는 것과 파라미터가 없는 것은 다르다. 아직 발급 못 받은 것은 빈 문자열로라도 넣어둔다.

---

## 4. 검증된 것 (실제로 돌려봄)

| 항목                     | 증거                                             |
| ------------------------ | ------------------------------------------------ |
| 웹·API 이미지 빌드       | 두 이미지 모두 로컬에서 빌드 성공                |
| 엔진이 컨테이너에서 동작 | `fairy-stockfish`가 USI 핸드셰이크 응답          |
| 스키마 적용              | pgvector 컨테이너에 9개 테이블 생성              |
| API 헬스체크             | `GET /healthz` → `{"ok":true}`, POST는 405       |
| 좌표 변환                | 81칸 전부 왕복 테스트 통과                       |
| Terraform                | `validate` 통과, `plan` 40개 생성                |
| IAM 최소권한             | 서울 리전 조회 거부, 계정 전체 S3 목록 거부 확인 |
| CI                       | `server`·`web`·CodeQL 전부 통과                  |

## 5. **검증 안 된 것** ← 새 세션이 먼저 볼 곳

전부 `apply` 또는 머지 이후에만 확인 가능하다. 여기서 문제가 나올 가능성이 높다.

| 항목                  | 왜 아직 모르나                                                  | 터지면 어디를 보나                                                                                                                                       |
| --------------------- | --------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **OIDC 신뢰관계**     | `images.yml`이 main 푸시에만 돌아 머지 전에는 실행 자체가 안 됨 | 에러가 `Not authorized to perform sts:AssumeRoleWithWebIdentity`로만 나온다. `sub` 조건이 `repo:jovid18/show-gi:ref:refs/heads/main`과 정확히 일치하는지 |
| **ECS 첫 배포**       | 태스크가 실제로 뜬 적 없음                                      | `aws logs tail /ecs/show-gi`. 비밀 주입 실패는 태스크가 시작조차 못 하는 형태로 나타난다                                                                 |
| **ACM 발급**          | 검증 레코드가 Route53에 들어가야 완료됨                         | apply가 `aws_acm_certificate_validation`에서 멈춰 있으면 DNS 전파 대기 중이다                                                                            |
| **RDS + pgvector**    | 인스턴스 생성 전                                                | `CREATE EXTENSION vector` 가 되는지. RDS PostgreSQL 15.2+ 지원이지만 실측 안 함                                                                          |
| **ALB 헬스체크 경로** | `/healthz`가 web(Caddy)→api로 프록시되는 경로를 실제로 안 타봄  | 타깃이 unhealthy면 Caddy는 살고 api가 죽은 상태일 수 있다                                                                                                |
| **ECR pull**          | 태스크 실행 역할의 권한을 실제로 안 써봄                        |                                                                                                                                                          |

---

## 6. 다음 단계 (순서가 중요)

```
① 파라미터 5개 등록          없으면 태스크가 못 뜬다
② terraform apply            40개 생성. ACM 검증 + RDS 때문에 10분쯤
③ 스키마 수동 적용            001_init.sql — DDL은 배포가 하지 않는다
④ PR #2 머지                 → images.yml 첫 실행 → ECS 배포
⑤ https://show-gi.com 확인
```

**②를 ④보다 먼저 한다.** `images.yml`의 배포 job이 ECS 서비스를 찾는데, 서비스가 없으면 실패해서 CI가 빨개진다.

절차는 [deploy/README.md](../deploy/README.md).

---

## 7. 열린 결정

| 결정                        | 상태                                                                                                                                                              |
| --------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **엔진을 YaneuraOu로 교체** | 빌드 가능함을 실측했다(아래). 아직 반영 안 함 — `apps/server/Dockerfile` 한 파일이고 `ENGINE_CMD`로 분리돼 있어 나중에 갈아끼워도 된다                            |
| **Terraform → OpenTofu**    | 지금 1.5.7이라 S3 네이티브 잠금을 못 써서 DynamoDB 테이블을 따로 둔다. 1.11+ 또는 OpenTofu로 가면 테이블이 사라진다. 미결                                         |
| **DB 접근 계층**            | ORM 미정. sqlc 제안 — 스키마(SQL)가 코드를 생성하는 방향이라 CHECK 제약·부분 인덱스·`vector` 타입을 그대로 쓸 수 있다. GORM AutoMigrate는 이 셋을 표현하지 못한다 |
| **RAG 코퍼스 출처**         | 어느 공개 정석 파일을 쓸지, 위키백과 인용 범위                                                                                                                    |

---

## 8. 알아두면 시간 아끼는 것

**엔진 빌드 실측** (arm64 컨테이너, 2026-08-08)

| 조합                      | 결과                                                          |
| ------------------------- | ------------------------------------------------------------- |
| `TARGET_CPU=ARMV8` + g++  | ✅ **33초**, USI 응답 확인 (`YaneuraOu NNUE 9.70git 64ARMV8`) |
| `ARMV8_DOTPROD` + g++     | ❌ NEON 인트린식 컴파일 에러                                  |
| `ARMV8_DOTPROD` + clang++ | ❌ 링커(lld) 없음                                             |

즉 **YaneuraOu는 arm64에서 문제없이 빌드된다.** dotprod는 NNUE 추론 속도 최적화라 없어도 된다. 남은 것은 평가함수 파일(水匠, ~60MB)을 이미지에 굽는 일.

**로컬 8080 포트 충돌**

`../shogi` 프로젝트의 `shogi-guide` 컨테이너가 8080을 잡고 있으면 `docker compose up`이 실패한다. `cd ../shogi && docker compose down`으로 내린다.

**무중단 배포는 아직 성립하지 않는다**

ECS가 롤링 배포와 헬스체크를 해주지만, **대국 세션이 서버 메모리에 있어서 배포하면 진행 중인 대국이 끊긴다.** 세션을 DB로 빼기 전에는 어떤 인프라를 써도 같다. 데모 녹화 중에는 main에 머지하지 않는 것으로 우회한다.

**비용**

상시 가동 시 주 **~$37**(Fargate $26 + ALB $4 + RDS $6). **대회가 끝나면 `terraform destroy`** — 안 지우면 월 $150이 계속 나간다.
