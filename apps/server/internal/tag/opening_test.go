package tag

import (
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// tradedBishops 는 角交換이 끝난 국면을 만든다 — 양쪽이 持ち駒로 角을 하나씩 들고
// 판에는 角도 馬도 없다.
func tradedBishops(ss ...square) shogi.Position {
	pos := place(shogi.Black, ss...)
	pos.Hands[shogi.Black][shogi.Bishop] = 1
	pos.Hands[shogi.White][shogi.Bishop] = 1
	return pos
}

// 빈 판은 角換わり가 아니다. 「판에 角이 없다」만 보면 駒를 몇 개 놓은 국면이 전부
// 角換わり가 된다 — 없는 것과 交換된 것을 구별하지 못한다. 실제로 그렇게 떴다.
func TestAnEmptyBoardIsNotABishopTrade(t *testing.T) {
	if bishopsTraded(place(shogi.Black)) {
		t.Error("빈 판이 角換わり로 읽혔다")
	}
	// 持ち駒에 한쪽만 있어도 交換이 아니다.
	half := place(shogi.Black)
	half.Hands[shogi.Black][shogi.Bishop] = 1
	if bishopsTraded(half) {
		t.Error("한쪽만 角을 든 국면이 角換わり로 읽혔다")
	}
	if !bishopsTraded(tradedBishops()) {
		t.Error("양쪽이 角을 들었는데 角換わり가 아니라고 한다")
	}
}

// 판에 角이 남아 있으면 交換이 끝난 것이 아니다. 馬(성한 角)도 센다.
func TestABishopOnTheBoardMeansNoTrade(t *testing.T) {
	for _, pt := range []shogi.PieceType{shogi.Bishop, shogi.PromBishop} {
		pos := tradedBishops(square{5, 5, pt})
		if bishopsTraded(pos) {
			t.Errorf("판에 %v 가 있는데 角換わり로 읽혔다", pt)
		}
	}
}

// 좁은 것이 먼저다. 角交換振り飛車는 角換わり이면서 振り飛車라, 순서가 뒤집히면
// 언제나 角換わり로 먼저 걸려 영원히 안 나온다.
func TestOpeningPrefersTheNarrowerName(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mine     []string
		theirs   []string
		wantCode string
	}{
		{"振っていない", nil, nil, "kaku_gawari"},
		{"내가 振った", []string{"2h6h"}, nil, "kakukan_furibisha"},
		{"양쪽이 振った", []string{"2h6h"}, []string{"8b4b"}, "kakukan_furibisha"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Detect(Input{
				Pos: tradedBishops(), Color: shogi.Black,
				PlayerMoves: tc.mine, OpponentMoves: tc.theirs,
			})
			// 戦型은 전법과 다른 축이라 둘이 함께 나온다. 戦型 칸만 본다.
			opening, ok := ofKind(got, KindOpening)
			if !ok || opening.Code != tc.wantCode {
				t.Fatalf("%s 를 기대했는데 %v", tc.wantCode, codes(got))
			}
		})
	}
}

// 相振り飛車는 양쪽이 다 振ったときだけ. 角은 交換되지 않은 국면으로 재서
// 角交換振り飛車와 섞이지 않게 한다.
func TestAiFuribishaNeedsBothSidesToSwing(t *testing.T) {
	both := Detect(Input{
		Pos: place(shogi.Black), Color: shogi.Black,
		PlayerMoves: []string{"2h6h"}, OpponentMoves: []string{"8b4b"},
	})
	if want := []string{"shiken_bisha", "ai_furibisha"}; len(both) != 2 ||
		both[0].Code != want[0] || both[1].Code != want[1] {
		t.Fatalf("%v 를 기대했는데 %v", want, codes(both))
	}

	// 상대가 振っていない
	one := Detect(Input{
		Pos: place(shogi.Black), Color: shogi.Black,
		PlayerMoves: []string{"2h6h"}, OpponentMoves: []string{"3c3d"},
	})
	if len(one) != 1 || one[0].Code != "shiken_bisha" {
		t.Errorf("상대가 안 振ったのに %v", codes(one))
	}
}

// 袖飛車·右四間飛車는 振り飛車가 아니다. 飛를 옮기지만 居飛車系라, 相振り飛車를
// 셀 때 그 둘을 세면 「양쪽이 振った」가 거짓이 된다.
func TestIbishaFamilyDoesNotCountAsFuribisha(t *testing.T) {
	for _, usi := range []string{"2h3h", "2h4h"} {
		got := Detect(Input{
			Pos: place(shogi.Black), Color: shogi.Black,
			PlayerMoves: []string{usi}, OpponentMoves: []string{"8b4b"},
		})
		for _, tg := range got {
			if tg.Code == "ai_furibisha" {
				t.Errorf("%s 는 振り飛車가 아닌데 相振り飛車가 떴다", usi)
			}
		}
	}
}

// ofKind 는 그 축의 태그를 꺼낸다. 축마다 최대 하나다.
func ofKind(tags []Tag, k Kind) (Tag, bool) {
	for _, tg := range tags {
		if tg.Kind == k {
			return tg, true
		}
	}
	return Tag{}, false
}
