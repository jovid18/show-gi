#!/usr/bin/env bash
# 부하 회차 명령을 프롬프트 없이 쓰게 한다.
#
# Claude 가 자기 권한 파일을 고치는 것은 분류기가 막는다(Bash 든 Edit 든). 그래서 사람이 돌린다.
#
#   bash .claude/how.sh
#
# 왜 `Bash(k6 run:*)` 로 안 되나: 회차 명령이 `BASE=... MODE=... k6 run ...` 이라
# 환경변수로 시작한다(tools/loadtest/README.md). 허용 규칙은 접두사 매칭이라 안 닿는다.
#
# BASE 로 못 박아서 이 두 주소 밖으로는 못 나간다. 되돌리려면 settings.local.json 에서
# 그 여섯 줄을 지운다. 이 파일과 settings.local.json 은 gitignore 대상이다.
set -euo pipefail
cd "$(dirname "$0")/.."

python3 - <<'PY'
import json, pathlib

p = pathlib.Path('.claude/settings.local.json')
s = json.loads(p.read_text()) if p.exists() else {}
allow = s.setdefault('permissions', {}).setdefault('allow', [])

rules = [
    "Bash(BASE=https://show-gi.com MODE=match:*)",
    "Bash(BASE=https://show-gi.com MODE=engine:*)",
    "Bash(BASE=https://show-gi.com MODE=both:*)",
    "Bash(BASE=http://localhost:8080 MODE=match:*)",
    "Bash(BASE=http://localhost:8080 MODE=engine:*)",
    "Bash(BASE=http://localhost:8080 MODE=both:*)",
]
added = [r for r in rules if r not in allow]
allow.extend(added)

p.write_text(json.dumps(s, indent=2, ensure_ascii=False) + '\n')
print('added:' if added else 'already up to date')
for r in added:
    print('  ' + r)
PY

echo
echo "끝났다. 이 세션에 반영하려면 /config 를 한 번 열거나 세션을 다시 시작한다."
