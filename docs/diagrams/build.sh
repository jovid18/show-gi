#!/bin/sh
# docs/diagrams/*.drawio 를 README 가 물고 있는 docs/images/*.svg 로 내보낸다.
#
# 한 파일에 두 테마를 담지 않는다. 왜 가르는지는 split-theme.py 의 doc 에 있다.
# --embed-diagram 이 원본 XML 을 SVG 안에 넣으므로, 내보낸 파일도 draw.io 로 다시 열린다.
set -eu
cd "$(dirname "$0")"

command -v drawio >/dev/null || {
  echo "drawio CLI 가 없다: brew install --cask drawio" >&2
  exit 1
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

for f in *.drawio; do
  base="${f%.drawio}"
  drawio -x -f svg --theme auto --embed-diagram --embed-svg-fonts false -b 12 \
    --disable-update -o "$tmp/$base.svg" "$f" >/dev/null
  python3 split-theme.py "$tmp/$base.svg" "../images/$base.svg" "../images/$base.dark.svg"
  echo "$f -> ../images/$base.svg + $base.dark.svg"
done
