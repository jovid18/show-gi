#!/usr/bin/env bash
# 판독을 재는 그림 하나를 픽스처로 앉힌다. 절차 전체는 apps/server/README.md.
#
#   fixture.sh <그림> '<고친 국면>'
#
# 두 번째 인자는 확인 화면에서 판을 다 고치고 「この局面を解析する」를 누른 뒤의
# **주소 그대로**여도 되고 SFEN 한 줄이어도 된다. 주소에서 `s=` 를 꺼내는 것이 이
# 스크립트가 하는 일의 절반이다 — 손으로 풀면 `+`(成)가 공백이 되어 조용히 틀린다.
#
#   fixture.sh ~/Desktop/shot.png 'http://localhost:5173/explore?s=ln1gkg1nl%2F…'
#   fixture.sh ~/Desktop/shot.png 'ln1gkg1nl/3s3+b1/… b B2p 1'
#
# **라벨을 룰 엔진에 물어본다.** 성립하지 않는 판은 픽스처로 안 앉힌다 — 틀린 라벨은
# 없는 라벨보다 나쁘다(측정이 조용히 나빠 보인다). 그래서 서버가 떠 있어야 한다.
#
# 그림도 라벨도 커밋되지 않는다(.gitignore). 남의 앱 화면과 방송 캡처이고 이 레포는
# 퍼블릭이다 — floodgate 기보와 같은 규약이다.
set -euo pipefail

cd "$(dirname "$0")"
images=images
api=${SHOWGI_API:-http://localhost:8080}

if [ $# -lt 2 ]; then
  sed -n '2,17p' "$0" | sed 's/^# \{0,1\}//'
  exit 2
fi
src=$1
label=$2

[ -f "$src" ] || {
  echo "그림이 없다: $src" >&2
  exit 1
}

# 주소로 왔으면 `s=` 를 꺼낸다. 쿼리 순서를 안 가정한다 — `m=` 이 앞에 올 수 있다.
case $label in
*'?'*|*'&s='*|*'/explore'*)
  label=$(printf '%s' "$label" | sed -n 's/.*[?&]s=\([^&]*\).*/\1/p')
  [ -n "$label" ] || {
    echo "주소에 s= 가 없다. 확인 화면에서 「この局面を解析する」를 누른 뒤의 주소여야 한다" >&2
    exit 1
  }
  # percent 를 푼다. `+` 를 공백으로 읽지 않는 것이 요점이다 — 폼 인코딩이 아니라
  # 주소이고, `2b3c+` 의 `+` 는 成을 뜻한다(routes/const.ts 와 같은 이유).
  label=$(printf '%b' "${label//%/\\x}")
  ;;
esac

# 룰 엔진에 물어본다. 사유가 하나라도 있으면 앉히지 않는다.
check=$(curl -sS --max-time 10 -X POST "$api/api/position/check" \
  -H 'Content-Type: application/json' \
  --data "$(printf '{"sfen":%s}' "$(printf '%s' "$label" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))')")") || {
  echo "서버에 못 물었다. docker compose up -d api 로 띄운 뒤 다시 해 본다" >&2
  exit 1
}

faults=$(printf '%s' "$check" | python3 -c '
import json, sys
r = json.load(sys.stdin)
if "faults" not in r:
    print("BAD " + r.get("message", "국면을 읽지 못했다")); raise SystemExit
for f in r["faults"]:
    print("BAD " + f["message"])
for w in r["warnings"]:
    print("WARN " + w)
')

if printf '%s\n' "$faults" | grep -q '^BAD '; then
  printf '%s\n' "$faults" | sed -n 's/^BAD /  거절: /p' >&2
  echo "라벨이 성립하지 않는다. 확인 화면에서 사유가 0이 될 때까지 고친 뒤 다시 해 본다" >&2
  exit 1
fi
# 경고는 거절이 아니다. 駒台가 잘려 나간 그림은 40장이 안 되는 것이 정상이다.
printf '%s\n' "$faults" | sed -n 's/^WARN /  경고: /p'

# 번호는 이어서 매긴다. 이름 순이 곧 표의 줄 순서라, 회차 둘을 나란히 놓고 읽으려면
# 그 순서가 고정돼야 한다(floodgate 의 seed 와 같은 이유).
mkdir -p "$images"
next=$(printf '%02d' $(($(ls "$images" 2>/dev/null | sed -n 's/^\([0-9]\{2\}\)-.*/\1/p' | sort -n | tail -1 | sed 's/^0*//' | grep . || echo 0) + 1)))
slug=${3:-$(basename "$src" | tr '[:upper:]' '[:lower:]' | sed 's/\.[^.]*$//; s/[^a-z0-9]\{1,\}/-/g; s/^-//; s/-$//')}
name="$next-${slug:-shot}"
ext=${src##*.}

cp "$src" "$images/$name.$ext"
printf '%s\n' "$label" > "$images/$name.sfen"

echo "앉혔다: $images/$name.$ext"
echo "라벨  : $label"
echo
echo "재는 것:"
echo "  SHOWGI_MEASURE=1 SHOWGI_OPENAI_KEY=… go test ./internal/boardread/ -run MeasureBoardRead -v -timeout 20m"
