---
name: self-review
description: Review the current branch (or a given PR/branch) against its requirement source, the show-gi invariants in CLAUDE.md, and the project docs; reports findings filtered by a Claude adversarial verifier plus a Codex cross-review. Never edits code. User-invoked only (/self-review).
disable-model-invocation: true
---

# Self Review (show-gi)

브랜치 diff(또는 PR) 하나를 입력으로 받아 검증된 findings를 낸다. 근거는 요구사항 정본, `CLAUDE.md`, `docs/`다. 이 하네스는 진단만 한다. 어떤 에이전트도 코드를 고치지 않는다.

인자 없이 실행하면(`/self-review`) 현재 브랜치를 리뷰한다. base는 열린 PR이 있으면 `origin/<baseRefName>`, 없으면 `origin/main`이다.

## 실행 모드: 서브 에이전트 + 파일 핸드오프

각 축을 `Agent` 도구로 스폰한다. `subagent_type`은 `.claude/agents/`의 `cr-*` 타입이고 병렬 실행은 `run_in_background: true`다. 에이전트끼리는 통신하지 않는다. 핸드오프는 전부 워크스페이스 파일이고, 순서는 오케스트레이터(너)가 스폰 시점으로 맞춘다.

에이전트는 네 대화를 물려받지 않는다. 모든 스폰 프롬프트에 target·`root`·워크스페이스 절대 경로를 담는다. 부분 재실행이면 `rerun` 지시와 기존 산출물 경로도 담는다. 모델은 에이전트 정의의 frontmatter를 따르고 스폰 시 `model`로 덮어쓰지 않는다.

외부 조회(`gh`)는 전부 네가 Phase 1에서 해서 파일로 스테이징한다. 에이전트는 `gh`를 쓰지 않는다.

**에이전트와 산출물** (워크스페이스 `_workspace/self-review/<target>/` 기준):

| 에이전트                   | 맡는 것                                                       | 스폰 조건         | 산출물                      |
| -------------------------- | ------------------------------------------------------------- | ----------------- | --------------------------- |
| `cr-requirements-analyst`  | 요구사항 정본 구조화                                          | 항상              | `01_requirements.md`        |
| `cr-requirements-reviewer` | 요구사항 ↔ 구현 정합성                                        | `01` 완료 후      | `02_requirements_review.md` |
| `cr-server-reviewer`       | Go 서버 컨벤션·spec 정합·스키마·마이그레이션                  | server 파일 ≥ 1   | `03_server_review.md`       |
| `cr-web-reviewer`          | 웹 컨벤션·spec 정합·린트 warn·훅                              | web 파일 ≥ 1      | `04_web_review.md`          |
| `cr-logic-reviewer`        | 재현 가능한 버그와 회귀                                       | 코드 파일 ≥ 1     | `05_logic_review.md`        |
| `cr-invariants-reviewer`   | CLAUDE.md 「이 프로젝트의 요점」·언어·비밀                    | 항상              | `06_invariants_review.md`   |
| `cr-docs-reviewer`         | 문서·주석 규칙과 문서 간 모순                                 | 항상              | `07_docs_review.md`         |
| `cr-verifier`              | 적대적 검증(기각 전 스틸맨). Claude 축만 검증하는 최종 게이트 | `02`–`07` 완료 후 | `08_verified_findings.md`   |
| `cr-codex-reviewer`        | 다른 모델의 독립 교차 리뷰(`codex exec review`, 순수 diff)    | 전제 충족 시      | `09_codex_review.md`        |

불변식 축과 문서 축은 non-code diff에서도 돈다. 문서만 바꾼 PR에서도 문서 모순과 비밀 유출은 생긴다.

**Claude 축과 교차 축은 서로 독립된 두 리스트다.** Codex는 다른 모델의 제2 의견이므로 Claude verifier로 거르지 않는다. 거르면 교차 리뷰의 의미가 사라진다. Phase 3에서 둘을 대조해 agreement map을 만든다.

**공유 지식코어**: `review-criteria` 스킬(권위 체인·심각도·Never Review·finding 형식). finding을 만들거나 검증하는 에이전트는 frontmatter `skills`로 스폰 시 프리로드한다(analyst 제외). 스폰 프롬프트에 로드 지시를 넣지 않는다.

## Phase 0: 컨텍스트 확인

`<target>`은 `pr-<번호>` 또는 브랜치명을 kebab으로 바꾼 것이다(`feat/self-review` → `feat-self-review`). 재실행 의미는 여기서만 정하고 스폰 프롬프트로 전달한다.

- **워크스페이스 없음** → 초기 실행.
- **있음 + 부분 재실행 요청**(「문서 축만 다시」, 「재검증만」) → 해당 에이전트만 재스폰한다. 프롬프트에 `rerun: full`과 기존 산출물 경로를 담는다. `full`은 기존 판정을 참고하지 않고 처음부터 다시 판정한다는 뜻이다.
- **있음 + 새 diff** → `tree/`가 있으면 `git worktree remove --force <workspace>/tree`로 먼저 해제한다. 워크스페이스를 `<target>_prev/`로 옮기고(이미 있으면 지우고 옮긴다) Phase 1부터 초기 실행처럼 진행한다.

## Phase 1: 스테이징 (오케스트레이터 단독)

`<workspace>/00_input/`에 만든다.

1. **대상 확정** — `git fetch origin`을 먼저 실행한다(실패하면 1회 재시도).
   - 무인자: `gh pr view --json number,title,body,baseRefName,headRefName`로 열린 PR을 찾는다. 있으면 base=`origin/<baseRefName>`, PR 제목·본문은 정본 체인에 쓴다. 없으면 base=`origin/main`. head=`HEAD`.
   - PR 번호·URL: `gh pr view <n>`으로 base를 정하고 `git fetch origin pull/<n>/head` 후 head=`FETCH_HEAD`의 SHA.
   - 다른 브랜치: base=`origin/main`, head=그 브랜치(로컬에 없으면 `origin/<branch>`).
   - 즉시 리뷰 불가로 보고하고 끝내는 경우는 셋이다. 무인자인데 현재 브랜치가 `main`, diff가 비어 있음, 재시도 후에도 fetch 실패. fetch 실패면 사용자에게 `! git fetch origin`을 직접 실행하고 다시 부르라고 안내한다. 낡은 `origin/*`로 진행하면 남의 커밋이 diff에 섞인다.
   - 커밋되지 않은 변경이 있으면 리뷰하지 않는다는 것을 `meta.md`에 적는다. diff는 `<base>...<head>` 커밋만 본다.
2. **리뷰 트리 확정** — head가 현재 `HEAD`와 같으면 `root`는 레포 루트다. 다르면 `git worktree add --detach <workspace>/tree <head>`로 체크아웃하고 `root`는 그 절대 경로다. 에이전트는 Read·Grep·라인 확정을 `root` 기준으로 한다. `tree/`는 부분 재실행을 위해 보존한다.
3. **diff 스테이징** — `diff.patch`(`git diff <base>...<head>`), `changed-files.txt`(`--name-status`), `commits.txt`(`git log <base>..<head>`).
4. **`scope.md`** — 변경 파일을 분류한다. 파일 수는 손으로 세지 말고 `grep -c`·`wc -l`로 낸다.
   - server: `apps/server/**`(생성물 `internal/store/db/**`는 「생성물」 줄에 따로 적는다)
   - web: `apps/web/**`
   - infra: `infra/**`, `deploy/**`, `docker-compose*.yml`, `.github/**`
   - docs: `docs/**`, `*.md`(레포 어디든), `.claude/**`
   - other: 나머지(`tools/**`, 루트 설정 등)
   - 「코드 파일」은 server + web + `tools/**`의 소스다. logic 축의 대조 목록이 된다.
5. **요구사항 정본 결정 체인** — 위에서부터, 확정되면 멈춘다. 소스와 모드를 `meta.md`에 적는다.
   1. 사용자가 이번 호출에서 준 설명이나 GitHub issue 번호(`gh issue view <n>`) → `mode: user`
   2. PR 본문 → `mode: pr-body`
   3. 리뷰 대상이 현재 워크트리면 루트의 `WORKTREE.md`(gitignore 대상이라 `tree/`에는 없다) → `mode: worktree`. 「건드리지 않는다」 표도 함께 옮긴다. 다른 워크트리가 점유한 파일을 diff가 건드리면 scope creep 후보다
   4. 없음 → `mode: limited`. 커밋 메시지와 diff가 더한 저널 절에서 의도만 추린다. 최종 리포트에 「요구사항 정본 부재 — 규칙 리뷰만 완전 수행」을 반드시 적는다

   원문은 가공하지 않고 `requirements-raw.md`에 저장한다. 1–3 중 여럿이 있으면 확정된 것 아래 소스도 「보조」로 함께 붙인다.

6. **`meta.md`** — target, base/head(SHA 포함), `root`, 워크스페이스 절대 경로, 요구사항 소스·모드, 스테이징 시각, 특이사항.

## Phase 2: 병렬 실행

의존 관계만 지키면 스케줄은 네 판단이다.

1. **1차 병렬 스폰**: analyst + server + web + logic + invariants + docs + codex. 서로 독립이므로 동시에 백그라운드로 스폰한다. 스폰 조건에 맞지 않는 축은 스폰하지 않는다.
   - **codex 전제는 스폰 전에 네가 확인한다**: `.claude/settings.json`의 `sandbox.excludedCommands`에 `"codex *"`가 있고, 단독 Bash 호출 `codex --version`이 성공해야 한다. 아니면 스폰하지 않고 리포트에 사유를 적는다. 인증은 codex 에이전트가 직접 확인한다.
2. analyst 완료 → requirements-reviewer 스폰.
3. Claude 리뷰 축(`02`–`07` 중 스폰한 것) 완료 → verifier 스폰. codex는 Phase 3에서 합류한다. codex 리뷰는 수 분 걸릴 수 있으니 Claude 축이 먼저 끝나면 codex만 기다린다. 각 보고서의 「타 축 발견」은 verifier가 수거해 소유 축 기준으로 검증한다.

실패한 축은 1회 재스폰하고, 다시 실패하면 그 축 없이 진행한다. verifier 스폰 프롬프트와 최종 리포트에 그 축의 부재를 적는다.

## Phase 3: 종합 + 최종 리포트

1. `08_verified_findings.md`, `02`의 커버리지 표, (스폰했다면) `09_codex_review.md`를 읽는다.
2. **Agreement map** — `08`의 CONFIRMED와 `09`의 CDX를 결함의 동일성(파일+증상, 라인 오프셋 차이는 무시)으로 대조한다.
   - 양쪽 일치 → Claude finding에 `— Claude·Codex 양쪽 다 지적` 병기(고신뢰)
   - Codex 단독 → 미검증이므로 단정하지 않고 「참고」로
   - Claude 단독 → 그대로 둔다. Codex가 놓쳤다고 반증이 되지 않는다
   - 애매하면 일치로 단정하지 않는다. 결과를 `10_agreement_map.md`에 저장한다(codex 미수행이면 생략).
3. **성공 기준 점검** — 미충족이면 그 에이전트만 `rerun: full`로 1회 재스폰한다. 그래도 미충족이면 진행하고 부록에 적는다. `02`–`07` 중 하나라도 재스폰했으면 verifier도 `rerun: full`로 재스폰하고 1–2를 다시 한다.
   - 모든 Claude finding에 CONFIRMED/REJECTED 판정이 있는가
   - `02`의 커버리지 표가 R·AC 전 항목을 다루는가
   - 각 보고서의 검토 대장이 `scope.md`의 대조 목록과 맞는가(`wc -l`로 대조). `03`=server, `04`=web, `05`=코드 파일, `06`=전체, `07`=docs + 주석이 바뀐 코드 파일
   - limited 모드면 「정본 부재」 문구가 있는가
   - codex를 스폰했으면 `09`가 있거나 미수행 사유가 적혔는가
   - `08`의 심각도가 `review-criteria` 규칙에 맞는가
4. 사용자에게 보고하고 워크스페이스는 보존한다(감사 추적·부분 재실행용).

최종 리포트는 읽는 사람이 다음에 할 행동 순서로 쓴다. 축·단계 같은 파이프라인 구조는 부록에만 둔다.

```markdown
# self-review — <target>

<한 문단. 무엇을 리뷰했고 결론이 무엇인지. 통계·축 이름 없이.>
<limited 모드면 이 문단에 「요구사항 정본 부재 — 규칙 리뷰만 완전 수행」>

## 고쳐야 할 것 (n건) ← 🔴🟡 + 🟢[결함], 무거운 순

**1. {🔴|🟡} <이대로 두면 무슨 일이 생기는가. 결함 이름이 아니라 결과로 쓴다>** <양쪽 다 지적했으면 "— Claude·Codex 양쪽 다 지적">

- **어디**: `path:line`
- **무슨 일**: <원문 인용 포함, 3문장 이내>
- **결과**: <무엇이 깨지는가. 자명하면 생략>
- **고치려면**: <finding의 제안을 그대로 옮긴다. 생략 금지>

**5. 🟢** <한 줄 요약> — `path:line` / <제안>

## 결정이 필요한 것 (n건) ← 판정 불가·[CONFLICT]·[AMBIGUOUS]·Claude와 Codex의 판정이 갈린 항목

**7. <무엇을 정해야 하는지 한 문장>** — <선택지. 양자택일이면 트레이드오프 한 줄>

## 참고 (n건)

머지를 막지 않는 개선 제안과 미검증 항목.

**11. 🟢** <한 줄 요약> — `path:line` / <제안>
**12. (Codex 🟡·미검증)** <한 줄 요약> — `path:line` / <제안>

## 요구사항 반영 상태

| 구현됨 | 부분 구현 | 미구현 |
| ------ | --------- | ------ |
| n      | n         | n      |

← 부분 구현·미구현만 한 줄씩. 전체 표는 `02`에 있다.

## 부록

- 리뷰 범위: 파일 n개 / 정본 소스(mode) / 수행한 축 / Codex 교차: 수행 | 미수행(<사유>)
- 대조: 양쪽 일치 n / Claude 단독 n / Codex 단독 n
- 기각 n건 / 범위 외 관찰 n건 — 상세는 `08`·`09`·`10`
- 항목 번호 ↔ 내부 ID: 1=VF-3, 2=VF-1, …
```

- **섹션은 심각도가 아니라 해야 할 행동으로 나눈다.** 「고쳐야 할 것」은 이번 diff가 만든 결함 전부(🔴·🟡·🟢[결함])다. 「참고」는 🟢[개선]과 미검증 Codex 단독 항목이다. 🟢이라는 이유로 결함을 「참고」에 넣지 않는다.
- **모든 항목 제목에 심각도를 표기한다.** Codex 단독은 🟢이 아니라 Codex의 원 심각도를 `(Codex 🟡·미검증)`으로 쓴다. 「결정이 필요한 것」은 판정이 없으므로 표기하지 않는다.
- **항목 번호는 리포트 전체에서 1부터 연속이다.** 섹션마다 되돌리지 않는다. 사용자가 「n번 고쳐줘」로 지목하는 주소다.
- **같은 결함을 두 번 싣지 않는다.**
- 본문은 독자의 언어로 쓴다. 무엇이 깨지는가, 어디를, 어떻게 고치는가.

## 리뷰 다음

사용자가 「n번 고쳐줘」라고 하면 그때 메인 세션이 고친다. 이 하네스의 에이전트는 고치지 않는다. 고친 뒤 다시 돌리면 Phase 0의 「새 diff」 경로를 탄다.
