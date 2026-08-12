package game

// 詰み 게이지 — **상대 玉 쪽 하나만 그린다.** 내 玉 쪽은 제지형 개입이 이미 막고 있고,
// 둘을 한 테두리에 그리면 이기는 중인지 지는 중인지가 반대로 읽힌다(01-core.md §7 · §31).

// MateHeatMax 는 게이지 세기의 상한이다. 화면이 단계 수를 알아야 곡선을 나눈다.
const MateHeatMax = 5

// mateHeat 는 詰み 手数를 게이지 세기로 옮긴다. 0이면 詰み이 없다(= 게이지가 꺼진다).
//
// **手数를 그대로 화면에 보내지 않기 위해 여기서 자른다.** 01-core.md §7의 게이지는
// 「수순도 手数도 알려주지 않는다」인데, 手数가 페이로드에 실려 있으면 그리지 않아도 이미
// 알려준 것이다 — 갇힘 힌트의 단계를 서버에서 자르는 것과 같은 자리다(buildHint).
//
// 구간은 §7의 「11→7→5→3→1」이 그대로다. solver 의 DepthLimit 이 11이라 手数는 홀수
// 1·3·5·7·9·11 중 하나로만 오지만, 그 한계는 환경변수라 짝수·더 큰 값에도 답이 있어야 한다.
func mateHeat(plies int) int {
	switch {
	case plies <= 0:
		return 0
	case plies <= 1:
		return 5
	case plies <= 3:
		return 4
	case plies <= 5:
		return 3
	case plies <= 7:
		return 2
	default:
		return 1
	}
}
