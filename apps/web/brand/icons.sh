#!/usr/bin/env bash
# 로고 한 장에서 배포에 나가는 아이콘을 전부 만든다. **결과물은 커밋한다.**
#
# 빌드에 끼우지 않는 이유가 둘이다 — ImageMagick이 CI 이미지에 없고(웹 Dockerfile은
# node:22-alpine + caddy 둘뿐이다), 로고는 바뀌는 것이 아니라 정해지는 것이라 매 빌드마다
# 같은 입력으로 같은 출력을 다시 만들 이유가 없다. 로고를 갈아 끼울 때 사람이 한 번 돌린다.
#
#   brew install imagemagick && apps/web/brand/icons.sh
#
# 일본어를 쓰는 자리는 OG 이미지 하나뿐이고 폰트를 macOS 것으로 못 박는다. 이 스크립트가
# 로컬에서 사람 손으로 도는 것이라 그래도 되고, 없으면 그 자리만 실패한다.
set -euo pipefail

cd "$(dirname "$0")"
src=logo.png
out=../public

# **먼저 정사각으로 맞춘다.** 원본이 위아래로 39px 비어 있어서 그대로 줄이면 아이콘마다
# 마스코트가 조금씩 위로 뜬다 — 16px에서는 그 어긋남이 전체의 3%가 된다.
square=$(mktemp -t showgi-logo).png
magick "$src" -trim +repage -background none -gravity center -extent "%[fx:max(w,h)]x%[fx:max(w,h)]" "$square"

# 로고 고리의 초록. apple-touch-icon 과 maskable 의 바탕이고, **셸의 --brand 와 다른 값이다**
# — 저쪽은 화면 위에서 크림과 견주는 색이고 이쪽은 그림 자신의 색이라 맞추면 로고가 변한다.
ring='#03553e'

# **전부 256색으로 줄여 내보낸다.** 그림이 평면 색과 부드러운 그러데이션 몇 군데뿐이라
# 눈에 보이는 손실이 없는데, 원본 그대로면 512 아이콘 하나가 230KB이고 OG가 500KB다 —
# 링크 미리보기 한 장에 그만큼 쓰는 것은 첫 화면 예산을 통째로 넘긴다.
shrink() { magick "$1" -strip -colors 256 -define png:compression-level=9 "$1"; }

png() {
	magick "$square" -resize "${1}x${1}" "$out/$2"
	shrink "$out/$2"
}

# ── 브라우저 탭 ────────────────────────────────────────
# 16·32·48을 한 파일에 담는다. 32만 넣으면 윈도 작업표시줄이 16으로 줄이면서 뭉갠다.
magick "$square" -resize 48x48 -define icon:auto-resize=48,32,16 "$out/favicon.ico"

# ── 홈 화면 ────────────────────────────────────────────
# **iOS는 투명을 검정으로 채운다.** 그래서 여기만 바탕을 깐다.
magick "$square" -resize 180x180 -background "$ring" -alpha remove -alpha off "$out/apple-touch-icon.png"
shrink "$out/apple-touch-icon.png"

png 192 icon-192.png
png 512 icon-512.png

# maskable 은 안드로이드가 **원·둥근네모 등으로 잘라 낸다.** 안전 영역이 가운데 80%라
# 로고를 그만큼 줄이고 남는 자리를 고리와 같은 초록으로 채운다 — 로고 자신이 초록 원이라
# 이음매가 안 보인다.
magick "$square" -resize 410x410 -background "$ring" -gravity center -extent 512x512 \
	-alpha remove -alpha off "$out/icon-maskable-512.png"
shrink "$out/icon-maskable-512.png"

# 헤더의 마크. 표시 크기가 1.9rem(≈30px)이라 3배까지 받친다.
png 96 logo-96.png

# ── 공유 카드 ──────────────────────────────────────────
# 1200×630. 셸과 같은 크림 바탕에 로고와 이름을 얹는다 — 링크를 받은 사람이 보는 첫 화면이
# 사이트를 열었을 때와 같은 색이어야 한다.
jp='/System/Library/Fonts/ヒラギノ角ゴシック W6.ttc'
magick -size 1200x630 canvas:'#ede7dc' \
	\( "$square" -resize 340x340 \) -gravity west -geometry +110+0 -composite \
	-font Helvetica-Bold -pointsize 92 -fill '#26211b' \
	-gravity northwest -annotate +500+215 'show-gi' \
	-font "$jp" -pointsize 34 -fill '#574f45' \
	-annotate +506+330 '口を出すときを自分で決める将棋の相手' \
	-font "$jp" -pointsize 27 -fill '#2f5d45' \
	-annotate +506+392 '悪手はその場で戻して、理由を説明します' \
	"$out/og.png"
shrink "$out/og.png"

rm -f "$square"
printf 'done:\n'
ls -la "$out"
