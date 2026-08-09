#!/bin/sh
# 테스트용 가짜 USI 엔진. 정해진 응답만 돌려준다.
#
# 진짜 엔진을 테스트에 쓰면 두 가지가 깨진다 — 결과가 버전마다 달라지고, CI에 엔진을
# 설치해야 한다. 여기서 검증하는 것은 엔진의 실력이 아니라 **우리 쪽 프로토콜 처리**다.
while read -r line; do
  case "$line" in
    usi)
      echo "id name FakeEngine 1.0"
      echo "id author test"
      echo "option name Skill Level type spin default 20 min -20 max 20"
      echo "option name MultiPV type spin default 1 min 1 max 5"
      echo "option name USI_Variant type combo default shogi var shogi var minishogi"
      echo "usiok"
      ;;
    isready)
      echo "readyok"
      ;;
    "go infinite")
      # stop 이 올 때까지 info 를 흘리고, USI 규격대로 stop 에 bestmove 로 답한다.
      (while :; do
        echo "info depth 1 score cp 10 nodes 1 pv 7g7f"
        sleep 0.05
      done) &
      loop=$!
      while read -r c; do
        case "$c" in
          stop) break ;;
          quit)
            kill "$loop" 2>/dev/null
            exit 0
            ;;
        esac
      done
      kill "$loop" 2>/dev/null
      echo "bestmove 7g7f"
      ;;
    "go mate infinite")
      echo "info string mate search"
      echo "checkmate G*5b"
      ;;
    "go mate nomate")
      echo "checkmate nomate"
      ;;
    "go mate timeout")
      echo "checkmate timeout"
      ;;
    "go deaf")
      # stop 을 무시하는 엔진. 취소가 재기동으로 떨어지는 경로를 만든다.
      while :; do sleep 0.2; done
      ;;
    go*)
      # 깊이 2단계 × 후보 2개. 2g2f 는 얕게 보면 +12 인데 깊게 보면 -5 —
      # 개입 판정이 읽어야 하는 "얕은 평가와 깊은 평가의 격차"가 여기 들어 있다.
      echo "info depth 1 seldepth 1 multipv 1 score cp 31 nodes 100 pv 7g7f"
      echo "info depth 1 multipv 2 score cp 12 nodes 100 pv 2g2f 8c8d 7g7f"
      echo "info depth 2 seldepth 2 multipv 1 score cp 42 nodes 200 pv 7g7f 3c3d"
      echo "info depth 2 multipv 2 score cp -5 nodes 200 pv 2g2f 8c8d 2f2e"
      echo "bestmove 7g7f ponder 3c3d"
      ;;
    die)
      exit 1
      ;;
    quit)
      exit 0
      ;;
  esac
done
