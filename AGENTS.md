# AGENTS.md

## 공통 규칙

작업 전에 [CLAUDE.md](CLAUDE.md)를 끝까지 읽는다. 아키텍처, 검증, Git, 언어, 문서, 보안 규칙은
Codex에도 동일하게 적용된다. 공통 규칙은 그 파일에서 관리하고 여기에 복사하지 않는다.
Claude 제품이나 도구를 지칭하는 경우를 제외하면, 문서의 작업 주체인 Claude는 현재 코딩 에이전트로 읽는다.

## 세션 시작

1. `CLAUDE.md`를 읽고, 루트에 `WORKTREE.md`가 있으면 작업 범위와 다른 워크트리가 맡은 파일을 확인한다.
2. `git status --short`와 `git branch --show-current`로 기존 변경과 현재 브랜치를 확인한다.
   기존 변경을 덮어쓰지 않는다. 브랜치와 워크트리는 공통 가이드의 규칙을 따른다.
3. [docs/06-status.md](docs/06-status.md)의 §1과 §5를 읽어 구현 상태와 다음 작업을 확인한다.
   상세 설계가 필요하면 [docs/README.md](docs/README.md)에서 해당 문서를 찾는다.
4. 수정할 디렉터리의 README와 추가 `AGENTS.md`가 있으면 읽는다. 서버 테스트의 DB·엔진 요구 사항은
   [apps/server/README.md](apps/server/README.md)에서 확인한다.

설치와 새 세션 확인 절차는 [docs/codex.md](docs/codex.md)에 있다.

## 레포 스킬

Codex는 `.agents/skills`에서 아래 스킬을 찾는다. 각 디렉터리는 `.claude/skills`의 원본을 가리키는
상대 심볼릭 링크다. 스킬을 수정할 때는 원본을 고치고, Codex용 복사본을 만들지 않는다.

| 요청                           | Codex 스킬   | 원본                                |
| ------------------------------ | ------------ | ----------------------------------- |
| PR 생성                        | `$create-pr` | `.claude/skills/create-pr/SKILL.md` |
| 병렬 작업용 워크트리 생성·정리 | `$worktree`  | `.claude/skills/worktree/SKILL.md`  |
| 프로덕션 플레이테스트          | `$playtest`  | `.claude/skills/playtest/SKILL.md`  |

이 레포의 PR에는 `$create-pr`를 쓴다. 회사 템플릿인 `git-workflow:create-pull-request`는 쓰지 않는다.
공통 가이드나 스킬의 `/create-pr`, `/worktree`, `/playtest`는 위 Codex 호출로 바꿔 읽는다.

## Claude 전용 지시 적용

- `Agent`와 `SendMessage`는 현재 세션의 하위 에이전트 생성·메시지 도구로 대체한다.
  호출 형식은 현재 도구에 맞추되 스킬의 실행 순서와 격리 조건을 지킨다.
- 플레이테스트에서 세 에이전트를 동시에 시작하라는 지시는 세 작업을 모두 시작한 뒤 결과를 기다리라는 뜻이다.
  하위 에이전트 도구가 없으면 세 판을 실행했다고 보고하지 말고 제약을 알린다.
- `<scratchpad>`가 필요한 작업은 `mktemp -d`로 레포 밖에 임시 디렉터리를 만든다.
- 워크트리를 인계할 때 Codex 세션은 `codex`, Claude Code 세션은 `claude` 실행 명령을 안내한다.
- `.claude/settings.local.json`과 `.claude/how.sh`는 Claude Code의 로컬 설정이다.
  Codex 권한 설정으로 사용하거나 실행하지 않는다. 권한과 사용 가능한 도구는 현재 Codex 세션의 설정을 따른다.

## 작업 완료

`CLAUDE.md`의 검증 절차를 따른다. 실행한 검사와 환경 문제로 건너뛴 검사를 구분해서 보고한다.
DB·엔진 테스트가 생략된 결과를 전체 테스트 통과로 보고하지 않는다.
공통 가이드대로 `main` 커밋·푸시, force push, PR 머지는 하지 않는다.
