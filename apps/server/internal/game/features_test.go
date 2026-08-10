package game

import (
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

func featuresAfter(t *testing.T, startSFEN string, usis ...string) intervene.Features {
	t.Helper()
	pos, m, err := replay(startSFEN, usis)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	return MoveFeatures(pos, m)
}

// 문서에 적힌 재현 수순 그대로다(06-status.md §13). 프로덕션에서 실제로 걸린 수라
// 여기가 틀리면 화면에 나가는 이유가 틀린다.
func TestHangingBishopIsSeen(t *testing.T) {
	// ▲7六歩 △3四歩 ▲3三角成 — 角을 아무도 지켜주지 않는 3三에 던진다
	f := featuresAfter(t, shogi.StartSFEN, "7g7f", "3c3d", "8h3c+")

	if !f.Known {
		t.Fatal("판을 못 읽었다")
	}
	if !f.LandsAttacked {
		t.Error("3三은 2二角이 노리는 칸이다")
	}
	if f.LandsDefended {
		t.Error("3三을 지키는 先手 駒는 없다")
	}
	if f.CapturedValue != 0 {
		t.Errorf("△3四歩로 비어 있는 칸이다. CapturedValue=%d", f.CapturedValue)
	}
	if f.MovedValue <= f.CapturedValue {
		t.Errorf("馬(%d)가 딴 것(%d)보다 싸게 나왔다", f.MovedValue, f.CapturedValue)
	}
	// 馬는 3三에서 4二를 지나 5一玉까지 닿는다 — **王手이면서 タダ捨て인 수**다.
	if !f.GivesCheck {
		t.Error("3三馬는 4二를 지나 5一玉에 닿는다")
	}

	// 그래서 이 수는 두 카테고리에 걸린다. 눈으로 확인할 수 있는 쪽을 말해야 한다 —
	// 「王手에 계속이 없다」보다 「그 駒가 그냥 잡힌다」가 초심자에게 배울 것이 된다.
	v := intervene.Judge(intervene.Input{BestCp: 0, AfterCp: -1600, Features: f})
	if v.Category != intervene.CategoryHangsPiece {
		t.Errorf("カテゴリ %q 기대, got %q", intervene.CategoryHangsPiece, v.Category)
	}
}

// 성했으면 성한 값으로 센다 — 馬를 角 값으로 세면 손익이 틀어진다.
func TestPromotionCountsAsThePromotedPiece(t *testing.T) {
	plain := featuresAfter(t, shogi.StartSFEN, "7g7f", "3c3d", "8h3c")
	prom := featuresAfter(t, shogi.StartSFEN, "7g7f", "3c3d", "8h3c+")
	if prom.MovedValue <= plain.MovedValue {
		t.Errorf("馬(%d)가 角(%d)보다 싸다", prom.MovedValue, plain.MovedValue)
	}
}

// 딴 駒의 값이 실제로 잡힌다. 이게 0으로 새면 「駒는 땄는데」 카테고리가 통째로 죽는다.
func TestCaptureValueIsRead(t *testing.T) {
	// ▲7六歩 △3四歩 ▲2二角成 — 角으로 角을 딴다
	f := featuresAfter(t, shogi.StartSFEN, "7g7f", "3c3d", "8h2b+")
	if f.CapturedValue != pieceValue[shogi.Bishop] {
		t.Errorf("角을 땄는데 CapturedValue=%d (기대 %d)", f.CapturedValue, pieceValue[shogi.Bishop])
	}
}

// 王手와 「되딸 수 있는가」가 같이 나온다. 打의 경우도 반상 이동과 같은 길로 간다.
func TestDroppedGoldGivesSupportedCheck(t *testing.T) {
	// 6一銀 · 5一玉 · 5三歩. ▲5二金打는 王手이고 6一銀이 딸 수 있지만,
	// 5三歩가 되딴다 — 그래서 タダ捨て가 아니다.
	f := featuresAfter(t, "3sk4/9/4P4/9/9/9/9/9/8K b G 1", "G*5b")

	if !f.GivesCheck {
		t.Error("5二金은 王手다")
	}
	if !f.LandsAttacked {
		t.Error("6一銀이 5二를 딸 수 있다")
	}
	if !f.LandsDefended {
		t.Error("銀이 딴 뒤 5三歩가 되딴다")
	}
	if f.MovedValue != pieceValue[shogi.Gold] {
		t.Errorf("金 값이 아니다: %d", f.MovedValue)
	}
}

// **利き이 아니라 합법수로 센다.**
//
// 핀에 묶인 駒는 노리기만 하고 못 딴다. 그걸 「잡힌다」고 세면 화면이
// 「その駒は取り返せない場所に置かれています」라고 **거짓을 단언**하고,
// 초심자에게는 그것을 검증할 수단이 없다 — 이 제품이 피하려는 바로 그 실패다.
func TestPinnedAttackerCannotHangAPiece(t *testing.T) {
	// 5三 飛(後手)는 5一玉과 5九飛 사이에 묶여 있다. 4三을 노리지만 움직이면 玉이 잡힌다
	const sfen = "4k4/9/4r4/9/9/9/9/9/4R3K b G 1"
	f := featuresAfter(t, sfen, "G*4c")

	if f.LandsAttacked {
		t.Error("핀에 묶여 못 움직이는 飛를 「딸 수 있다」로 셌다")
	}
	if got := intervene.Judge(intervene.Input{BestCp: 0, AfterCp: -1600, Features: f}).Category; got == intervene.CategoryHangsPiece {
		t.Error("아무도 못 잡는 駒를 タダ捨て라고 했다")
	}
}

// 詰み 국면에서는 상대에게 합법수가 하나도 없다 — 노리는 것과 딸 수 있는 것의 차이가
// 제일 크게 벌어지는 자리다.
func TestNothingCapturesWhenItIsCheckmate(t *testing.T) {
	// 4k4/9/4P4/9/9/9/9/9/8K b G 1 의 1手詰め. 5一玉이 5二를 노리지만 5三歩 때문에 못 딴다
	f := featuresAfter(t, "4k4/9/4P4/9/9/9/9/9/8K b G 1", "G*5b")

	if !f.GivesCheck {
		t.Fatal("테스트 전제가 깨졌다 — 5二金은 王手다")
	}
	if f.LandsAttacked {
		t.Error("詰み인데 5二를 딸 수 있다고 했다")
	}
}

// 평시의 수는 玉 주변을 안 건드린다. 여기가 0이 아니면 어떤 수를 둬도
// 「玉이 열렸다」가 붙어 설명이 통째로 못 미덥게 된다.
func TestQuietMoveDoesNotDisturbTheKing(t *testing.T) {
	f := featuresAfter(t, shogi.StartSFEN, "7g7f")
	if f.ShieldLoss != 0 || f.ThreatGain != 0 {
		t.Errorf("▲7六歩가 玉 주변을 흔들었다: ShieldLoss=%d ThreatGain=%d", f.ShieldLoss, f.ThreatGain)
	}
}

// 玉을 지키던 駒가 앞으로 나가면 방어 利き이 준다.
func TestGuardSteppingForwardCostsShield(t *testing.T) {
	// ▲4八金 — 4九金이 5九玉의 뒷줄에서 앞으로 나간다
	f := featuresAfter(t, shogi.StartSFEN, "4i4h")
	if f.ShieldLoss <= 0 {
		t.Errorf("4九金이 나갔는데 玉 주변 방어가 안 줄었다: ShieldLoss=%d", f.ShieldLoss)
	}
}

// **玉 자신이 움직이는 수는 반대로 나온다.** 착수 전 자리를 계속 보면
// 「빈 칸 주변이 허술해졌다」가 되어 玉을 옮기는 정상적인 수마다 이유가 붙는다.
func TestKingMovingIsMeasuredFromItsNewSquare(t *testing.T) {
	f := featuresAfter(t, shogi.StartSFEN, "5i5h")
	if f.ShieldLoss > 0 {
		t.Errorf("玉이 자기 진영 안으로 한 칸 들어갔는데 허술해졌다고 나왔다: ShieldLoss=%d", f.ShieldLoss)
	}
}

// 판을 못 읽으면 Known=false 로 남고 카테고리만 other 가 된다. **판정은 계속된다.**
func TestReplayRejectsGarbage(t *testing.T) {
	if _, _, err := replay(shogi.StartSFEN, []string{"nonsense"}); err == nil {
		t.Error("깨진 USI를 받아들였다")
	}
	if _, _, err := replay("not a sfen", []string{"7g7f"}); err == nil {
		t.Error("깨진 SFEN을 받아들였다")
	}
}

// UnpromotedOnly 는 **틀린 설명을 막는 한 칸**이다(08-playtest.md §8).
//
// 이동이 최선수와 같고 成 여부만 다를 때만 참이어야 한다. 넓으면 정상적인 不成까지
// 「成れます」로 몰고, 좁으면 다시 greedy_capture 로 떨어져 잡는 것을 문제라고 말한다.
func TestUnpromotedOnly(t *testing.T) {
	must := func(u string) shogi.Move {
		t.Helper()
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			t.Fatalf("%s: %v", u, err)
		}
		return m
	}

	for _, tc := range []struct {
		name   string
		played string
		best   string
		want   bool
	}{
		// 실제로 물린 그 수다. 기보 103手.
		{"같은 이동, 成만 다르다", "7c8d", "7c8d+", true},
		// 이미 성한 수는 이 카테고리가 아니다.
		{"성해서 뒀으면 아니다", "7c8d+", "7c8d+", false},
		// 최선수가 不成이면 「成れます」가 거짓이 된다.
		{"최선수가 不成이면 아니다", "7c8d", "7c8d", false},
		// 도착 칸이 다르면 이동 자체가 다르다 — 成이 이유일 수 없다.
		{"도착이 다르면 아니다", "7c8d", "7c7d+", false},
		{"출발이 다르면 아니다", "7c8d", "9c8d+", false},
		// 打은 성할 수 없다. 어느 쪽이든 이 카테고리가 아니다.
		{"둔 수가 打이면 아니다", "S*8d", "7c8d+", false},
		{"최선수가 打이면 아니다", "7c8d", "S*8d", false},
		// 최선수를 못 구했을 때 지어내지 않는다.
		{"최선수가 없으면 아니다", "7c8d", "", false},
		{"최선수를 못 읽으면 아니다", "7c8d", "resign", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := UnpromotedOnly(must(tc.played), tc.best); got != tc.want {
				t.Fatalf("%v, want %v", got, tc.want)
			}
		})
	}
}
