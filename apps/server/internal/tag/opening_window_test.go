package tag

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// 戦法·戦型은 **序盤 분류**다. 시간 경계가 없으면 종반의 우연이 이름을 받는다 —
// floodgate 341판에서 中飛車의 중앙값이 45수, 相振り飛車가 67수, 角換わり가 198수까지
// 나왔다([journal §44](../../../../docs/journal/41-60.md)).
//
// 실제로 한 판에서 같은 쪽이 **15수에 居飛車, 131수에 中飛車**로 떴다. 정답 라벨 없이도
// 틀린 것이 확실한 자리라, 여기 회귀로 박는다.

// padding 은 飛와 무관한 수 n개다. `DetectFormation` 은 좇는 칸에서 出発하지 않는 수를
// 건너뛰므로, 무엇이든 파싱만 되면 자리를 채우는 데 쓸 수 있다.
func padding(n int) []string {
	out := make([]string, 0, n)
	for i := range n {
		out = append(out, fmt.Sprintf("%dg%df", i%9+1, i%9+1))
	}
	return out
}

// **序盤을 넘겨 振った 것은 그 판의 전법이 아니다.**
func TestFormationIgnoresASwingAfterTheOpening(t *testing.T) {
	// 先手의 i번째 수는 2i+1 手째다. 경계 밖 첫 자리는 i=12 (25手째).
	late := append(padding(12), "2h6h")

	if got, ok := DetectFormation(late, shogi.Black); ok {
		t.Errorf("25手째의 転換에 %s 가 붙었다", got.Code)
	}
}

// **경계 바로 안쪽은 붙는다.** 없으면 위 테스트가 「그냥 아무것도 안 붙는다」와 구별되지
// 않는다 — 경계를 재는 테스트는 언제나 양쪽을 함께 짚어야 한다.
func TestFormationTakesASwingInsideTheOpening(t *testing.T) {
	inTime := append(padding(11), "2h6h") // 23手째

	got, ok := DetectFormation(inTime, shogi.Black)
	if !ok || got.Code != "shiken_bisha" {
		t.Errorf("23手째의 四間飛車를 기대했는데 %v (ok=%v)", got.Code, ok)
	}
}

// **경계는 「지금 몇 수째인가」가 아니라 「振った 것이 몇 수째였나」다.**
//
// 이것이 이 설계의 요점이다. 지금 手数로 잘랐으면 12수에 얻은 四間飛車가 25수째에
// 사라지고, 그건 화면에서 이름이 대국 도중에 꺼지는 것으로 보인다.
func TestFormationKeepsItsNameLongAfterTheOpening(t *testing.T) {
	long := append([]string{"2h6h"}, padding(80)...)

	got, ok := DetectFormation(long, shogi.Black)
	if !ok || got.Code != "shiken_bisha" {
		t.Errorf("한 번 얻은 이름이 남아야 한다: %v (ok=%v)", got.Code, ok)
	}
}

// 後手는 i번째 수가 2i+2 手째다. 부호가 틀리면 한쪽에서만 경계가 한 수 어긋나는데,
// 그건 에러가 안 나고 기보를 세어봐야 보인다.
func TestFormationWindowMirrorsForGote(t *testing.T) {
	for _, tc := range []struct {
		name string
		pad  int
		want bool
	}{
		{"24手째는 안쪽", 11, true},  // 2*11+2 = 24
		{"26手째는 바깥", 12, false}, // 2*12+2 = 26
	} {
		t.Run(tc.name, func(t *testing.T) {
			moves := append(padding(tc.pad), "8b4b")

			_, ok := DetectFormation(moves, shogi.White)
			if ok != tc.want {
				t.Errorf("ok=%v 를 기대했는데 %v", tc.want, ok)
			}
		})
	}
}

// 角換わり — **角이 언제 교환됐는지를 상태로는 알 수 없다.** 그래서 이쪽만 「지금
// 手数」로 자르고, 그 대가로 序盤 동안만 뜬다.
func TestKakuGawariOnlyInsideTheOpeningWindow(t *testing.T) {
	// 판에 角도 馬도 없고 양쪽이 하나씩 손에 든 국면 — `bishopsTraded` 가 요구하는 그것.
	pos, err := shogi.ParseSFEN("8k/9/9/9/9/9/9/9/8K b Bb 1")
	if err != nil {
		t.Fatalf("SFEN 파싱 실패: %v", err)
	}

	has := func(moves int) bool {
		in := Input{Pos: pos, Color: shogi.Black, PlayerMoves: padding(moves), OpponentMoves: padding(moves)}
		for _, tg := range Detect(in) {
			if tg.Code == "kaku_gawari" {
				return true
			}
		}
		return false
	}

	if !has(OpeningPlies / 2) {
		t.Errorf("%d手째에는 角換わり가 떠야 한다", OpeningPlies)
	}
	if has(OpeningPlies) {
		t.Errorf("%d手째에 角換わり가 떴다 — 종반의 角交換은 戦型이 아니다", 2*OpeningPlies)
	}
}

// 경계를 넘은 판에서 **아무 이름도 안 남는 것이 아니다.** 囲い는 상태라 그대로 뜬다 —
// 잘라낸 것이 戦法·戦型 축뿐이라는 것을 짚어 둔다.
func TestCastlesAreNotBoundByTheOpeningWindow(t *testing.T) {
	// 片美濃(玉2八·銀3八·金4八)가 선 국면. 手数는 경계를 한참 넘긴다.
	pos, err := shogi.ParseSFEN("8k/9/9/9/9/9/9/5GSK1/9 b - 1")
	if err != nil {
		t.Fatalf("SFEN 파싱 실패: %v", err)
	}

	in := Input{Pos: pos, Color: shogi.Black, PlayerMoves: padding(60), OpponentMoves: padding(60)}

	var codes []string
	for _, tg := range Detect(in) {
		if tg.Kind == KindCastle {
			codes = append(codes, tg.Code)
		}
	}
	if len(codes) == 0 {
		t.Errorf("囲い는 手数와 무관해야 한다 — 아무것도 안 떴다 (%s)", strings.Join(codes, ","))
	}
}
