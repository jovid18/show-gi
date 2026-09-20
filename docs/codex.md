# Codex로 작업하기

이 레포는 Claude Code와 Codex가 같은 프로젝트 규칙과 스킬을 사용한다.
Codex의 진입 문서는 루트 [AGENTS.md](../AGENTS.md)이고, 공통 규칙은 [CLAUDE.md](../CLAUDE.md)에 있다.

## 시작

Codex CLI를 설치하고 로그인한 뒤 레포 루트에서 실행한다.
앱에서는 이 레포 디렉터리를 프로젝트로 연다.

```sh
codex
```

새로 클론했다면 `pnpm install`로 웹 의존성을 설치하고 공통 가이드의 Git 훅 설정을 적용한다.
앱 실행 방법과 검증 명령은 `CLAUDE.md`를 따른다. 서버 환경은 [apps/server/README.md](../apps/server/README.md)에 있다.

## 지시와 스킬 확인

새 세션에서 다음과 같이 요청하면 초기 구성을 확인할 수 있다.

> 파일은 수정하지 말고 AGENTS.md와 CLAUDE.md를 읽어줘. 현재 브랜치, WORKTREE.md 유무,
> 이 레포의 세 스킬 경로, PR 생성과 머지 규칙을 알려줘.

레포 스킬은 `$create-pr`, `$worktree`, `$playtest`다. `.agents/skills/`의 상대 심볼릭 링크가
`.claude/skills/`의 원본을 가리키므로 전역 설치는 필요 없다.
스킬을 찾지 못하면 레포 디렉터리에서 세션을 열었는지 확인하고 링크를 검사한다.

```sh
ls -l .agents/skills
test -r .agents/skills/create-pr/SKILL.md
test -r .agents/skills/worktree/SKILL.md
test -r .agents/skills/playtest/SKILL.md
```

링크를 고친 뒤에는 새 세션에서 다시 확인한다. 워크트리에서도 같은 상대 경로를 사용한다.
플레이테스트에는 하위 에이전트 셋을 실행할 수 있는 세션과 프로덕션 접근이 필요하다.

## 설정 범위

현재 레포 지시와 스킬 탐색에는 별도 `.codex/config.toml`이 필요하지 않다.
모델, 로그인, 승인 정책과 개인 MCP 연결은 각자의 Codex 환경에서 관리한다.
Claude Code의 `.claude/settings.local.json`과 `.claude/how.sh`는 Codex 설정으로 가져오지 않는다.

Codex는 시작할 때 `AGENTS.md`를 읽는다. 이 레포에서는 그 문서가 `CLAUDE.md` 전체를 읽도록 지시한다.
`CLAUDE.md`를 fallback 파일명으로 추가하더라도 같은 디렉터리의 `AGENTS.md`와 둘 다 자동으로 읽는 방식은 아니다.
탐색 순서와 스킬 위치는 공식 [AGENTS.md 안내](https://developers.openai.com/codex/guides/agents-md)와
[스킬 안내](https://developers.openai.com/codex/skills)를 참고한다.
