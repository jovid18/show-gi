package game

// 詰み 게이지. 상대 玉 쪽 하나만 그린다. 내 玉 쪽은 제지형 개입이 이미 막고 있고,
// 둘을 한 테두리에 그리면 이기는 중인지 지는 중인지가 반대로 읽힌다
// (01-core.md §7 · journal §31).

// MateHeatMax 는 게이지 세기의 상한이다. 화면이 단계 수를 알아야 곡선을 나눈다.
const MateHeatMax = 5

// MateChasePlies 는 상대가 조절을 멈추고 최선으로 버티는 詰み 거리다.
//
// 사람이 이 안에서 詰み을 걸고 있으면 상대는 밴드를 보지 않는다. 연습이 성립하려면
// 저항이 정직해야 한다(01-core.md §6).
//
// 끄는 것은 약화뿐이다. 후보 생성도 두 안전 필터도 그대로다.
//
// 값이 게이지의 세기 2와 같은 선이다(아래 mateHeat). 눈금이 둘이면 테두리의 불꽃과
// 상대의 태도가 어긋나 보인다.
//
// [미확정] 7은 사람이 고른 값이고 아직 실측이 없다(journal §76).
const MateChasePlies = 7

// mateHeat 는 詰み 手数를 게이지 세기로 옮긴다. 0이면 詰み이 없다(= 게이지가 꺼진다).
//
// 手数를 그대로 화면에 보내지 않기 위해 여기서 자른다. 페이로드에 실려 있으면 그리지
// 않아도 이미 알려준 것이다(01-core.md §7, buildHint 와 같은 자리).
//
// 구간은 01-core.md §7의 표 그대로다. solver 의 DepthLimit 이 11이라 手数는 홀수로만
// 오지만, 그 한계는 환경변수라 짝수·더 큰 값에도 답이 있어야 한다.
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
