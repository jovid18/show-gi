package shogi

import (
	"errors"
	"strings"
	"testing"
)

func mustPos(t *testing.T, sfen string) Position {
	t.Helper()
	pos, err := ParseSFEN(sfen)
	if err != nil {
		t.Fatalf("ParseSFEN(%q): %v", sfen, err)
	}
	return pos
}

func mustMove(t *testing.T, usi string) Move {
	t.Helper()
	m, err := ParseUSIMove(usi)
	if err != nil {
		t.Fatalf("ParseUSIMove(%q): %v", usi, err)
	}
	return m
}

// wantReason 은 수가 불법이고 그 사유가 want 인지 확인한다.
// 문구가 아니라 코드로 본다 — 문구는 언제든 다듬을 수 있고, 판정은 그러면 안 된다.
func wantReason(t *testing.T, err error, want Reason, ctx string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: 불법수가 허용됨", ctx)
	}
	var ime *IllegalMoveError
	if !errors.As(err, &ime) {
		t.Fatalf("%s: *IllegalMoveError 기대, got %T (%v)", ctx, err, err)
	}
	if ime.Reason != want {
		t.Fatalf("%s: 사유 %q 기대, got %q", ctx, reasonNames[want], reasonNames[ime.Reason])
	}
}

func TestStartPositionLegalMoveCount(t *testing.T) {
	pos := StartPosition()
	moves := pos.LegalMoves()
	if len(moves) != 30 {
		var ss []string
		for _, m := range moves {
			ss = append(ss, m.USI())
		}
		t.Fatalf("초기국면 합법수 = %d (기대 30): %s", len(moves), strings.Join(ss, " "))
	}
	// 후수도 대칭으로 30수
	after := pos.Apply(mustMove(t, "7g7f"))
	if n := len(after.LegalMoves()); n != 30 {
		t.Fatalf("▲7六歩 이후 후수 합법수 = %d (기대 30)", n)
	}
}

func TestSFENRoundtrip(t *testing.T) {
	cases := []string{
		StartSFEN,
		"lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL w - 2",
		"3lkl3/3p1p3/9/4L4/9/9/9/9/4K4 b P 1",
		"k8/4+P4/9/9/9/2+B6/9/9/4K4 w R2Pbg3p 42",
	}
	for _, sfen := range cases {
		pos := mustPos(t, sfen)
		if got := pos.SFEN(); got != sfen {
			t.Errorf("SFEN 왕복 불일치:\n  in  %s\n  out %s", sfen, got)
		}
	}
}

func TestUSIMoveRoundtrip(t *testing.T) {
	for _, usi := range []string{"7g7f", "8h2b+", "P*5e", "N*3c", "1a1b", "9i9h"} {
		m := mustMove(t, usi)
		if got := m.USI(); got != usi {
			t.Errorf("USI 왕복: %q → %q", usi, got)
		}
	}
	for _, bad := range []string{"", "7g", "0a1b", "7j7f", "K*5e", "X*5e", "7g7f++"} {
		if _, err := ParseUSIMove(bad); err == nil {
			t.Errorf("ParseUSIMove(%q): 에러 기대", bad)
		}
	}
}

func TestApplyCaptureToHand(t *testing.T) {
	// 선수 飛가 후수 と금(+p)을 잡으면 持ち駒에는 원래 모습(歩)으로 들어간다
	pos := mustPos(t, "k8/9/4+p4/9/4R4/9/9/9/4K4 b - 1")
	np := pos.Apply(mustMove(t, "5e5c"))
	if np.Hands[Black][Pawn] != 1 {
		t.Fatalf("잡은 と금이 歩로 持ち駒에 안 들어감: %v", np.Hands)
	}
	if np.Board[SquareOf(5, 3)] != MakePiece(Rook, Black) {
		t.Fatalf("飛가 5三에 없음")
	}
	if np.Turn != White || np.MoveNum != 2 {
		t.Fatalf("수번/수번호 갱신 오류: %v %v", np.Turn, np.MoveNum)
	}
}

func TestNifu(t *testing.T) {
	pos := mustPos(t, "k8/9/9/9/9/9/4P4/9/4K4 b P 1")
	wantReason(t, pos.ValidateMove(mustMove(t, "P*5e")), ReasonNifu, "P*5e")

	// 다른 줄은 합법
	if err := pos.ValidateMove(mustMove(t, "P*4e")); err != nil {
		t.Fatalf("4筋 투입은 합법이어야 함: %v", err)
	}
	// 승격한 歩(と금)가 있는 줄은 二歩가 아님
	pos2 := mustPos(t, "k8/9/4+P4/9/9/9/9/9/4K4 b P 1")
	if err := pos2.ValidateMove(mustMove(t, "P*5e")); err != nil {
		t.Fatalf("と금이 있는 줄의 歩 투입은 합법: %v", err)
	}
}

func TestDeadPieceDrops(t *testing.T) {
	pos := mustPos(t, "k8/9/9/9/9/9/9/9/4K4 b PLN 1")
	for _, usi := range []string{"P*5a", "L*5a", "N*5a", "N*5b"} {
		wantReason(t, pos.ValidateMove(mustMove(t, usi)), ReasonDeadPieceDrop, usi)
	}
	for _, usi := range []string{"P*5b", "L*5b", "N*5c"} {
		if err := pos.ValidateMove(mustMove(t, usi)); err != nil {
			t.Errorf("%s: 합법이어야 함: %v", usi, err)
		}
	}
}

func TestPinnedPieceCannotExposeKing(t *testing.T) {
	// 후수 香가 5筋에서 선수 玉을 노림. 5五의 金은 핀 상태.
	pos := mustPos(t, "4l4/9/9/9/4G4/9/9/9/4K4 b - 1")
	wantReason(t, pos.ValidateMove(mustMove(t, "5e4e")), ReasonLeavesKingInCheck, "5e4e")

	// 같은 줄에서 전진은 합법
	if err := pos.ValidateMove(mustMove(t, "5e5d")); err != nil {
		t.Fatalf("핀 유지 전진은 합법: %v", err)
	}
}

func TestMustRespondToCheck(t *testing.T) {
	// 후수 飛가 王手. 무관한 수는 王手放置.
	pos := mustPos(t, "k3r4/9/9/9/9/9/9/9/P3K4 b - 1")
	if !pos.InCheck(Black) {
		t.Fatal("테스트 국면 오류: 선수가 王手를 받고 있어야 함")
	}
	wantReason(t, pos.ValidateMove(mustMove(t, "9i9h")), ReasonMustResolveCheck, "9i9h")

	// 玉이 피하면 합법
	if err := pos.ValidateMove(mustMove(t, "5i4i")); err != nil {
		t.Fatalf("玉 피신은 합법: %v", err)
	}
}

func TestUchifuzume(t *testing.T) {
	// 桂 지원 하의 歩 투입 외통 = 打ち歩詰め (불법).
	// (桂는 5二를 지키지만 5一 玉은 노리지 않으므로 투입 전 국면이 합법)
	pos := mustPos(t, "3lkl3/3p1p3/9/3N5/9/9/9/9/4K4 b P 1")
	if pos.InCheck(White) {
		t.Fatal("테스트 국면 오류: 투입 전부터 王手")
	}
	wantReason(t, pos.ValidateMove(mustMove(t, "P*5b")), ReasonUchifuzume, "P*5b")

	// 지원 桂가 없으면 玉이 잡을 수 있으므로 합법
	pos2 := mustPos(t, "3lkl3/3p1p3/9/9/9/9/9/9/4K4 b P 1")
	if err := pos2.ValidateMove(mustMove(t, "P*5b")); err != nil {
		t.Fatalf("잡을 수 있는 歩 투입 王手는 합법: %v", err)
	}
	// 歩를 '움직여서' 만드는 외통(突き歩詰め)은 합법
	pos3 := mustPos(t, "3lkl3/3p1p3/4P4/3N5/9/9/9/9/4K4 b - 1")
	if err := pos3.ValidateMove(mustMove(t, "5c5b")); err != nil {
		t.Fatalf("突き歩詰め는 합법이어야 함: %v", err)
	}
}

func TestMandatoryPromotion(t *testing.T) {
	// 歩가 1단으로: 반드시 승격
	pos := mustPos(t, "k8/4P4/9/9/9/9/9/9/4K4 b - 1")
	var from5b []Move
	for _, m := range pos.LegalMoves() {
		if !m.IsDrop() && m.From == int8(SquareOf(5, 2)) {
			from5b = append(from5b, m)
		}
	}
	if len(from5b) != 1 || !from5b[0].Promote {
		t.Fatalf("5二 歩의 합법수는 승격 1개여야 함: %v", from5b)
	}
	wantReason(t, pos.ValidateMove(mustMove(t, "5b5a")), ReasonMustPromote, "5b5a")

	// 桂가 2단으로: 반드시 승격
	pos2 := mustPos(t, "k8/9/9/3N5/9/9/9/9/4K4 b - 1")
	for _, m := range pos2.LegalMoves() {
		if !m.IsDrop() && m.From == int8(SquareOf(6, 4)) && !m.Promote {
			t.Fatalf("桂의 2단 진입 미승격이 허용됨: %s", m.USI())
		}
	}
}

func TestPromotionZone(t *testing.T) {
	// 적진 밖→밖 이동은 승격 불가
	pos := mustPos(t, "k8/9/9/9/4R4/9/9/9/4K4 b - 1")
	wantReason(t, pos.ValidateMove(mustMove(t, "5e5d+")), ReasonOutsidePromoZone, "5e5d+")

	// 적진 진입 시 승격 가능
	if err := pos.ValidateMove(mustMove(t, "5e5c+")); err != nil {
		t.Fatalf("적진 진입 승격은 합법: %v", err)
	}
	// 적진에서 나올 때도 승격 가능
	pos2 := mustPos(t, "k8/9/4R4/9/9/9/9/9/4K4 b - 1")
	if err := pos2.ValidateMove(mustMove(t, "5c5d+")); err != nil {
		t.Fatalf("적진 이탈 승격은 합법: %v", err)
	}
}

func TestAtamakinCheckmate(t *testing.T) {
	// 頭金: 歩 지원 하의 金 = 詰み
	pos := mustPos(t, "4k4/4G4/4P4/9/9/9/9/9/4K4 w - 1")
	if !pos.IsCheckmate() {
		t.Fatal("頭金 외통이 詰み로 판정되지 않음")
	}
	// 金 '투입'으로 만드는 외통은 합법 (打ち歩詰め는 歩만 해당)
	pos2 := mustPos(t, "4k4/9/4P4/9/9/9/9/9/4K4 b G 1")
	if err := pos2.ValidateMove(mustMove(t, "G*5b")); err != nil {
		t.Fatalf("金 투입 외통은 합법: %v", err)
	}
	after := pos2.Apply(mustMove(t, "G*5b"))
	if !after.IsCheckmate() {
		t.Fatal("金 투입 후 詰み여야 함")
	}
	if !after.NoLegalMoves() {
		t.Fatal("詰み 국면에서 합법수가 있음")
	}
}

func TestRepetitionKey(t *testing.T) {
	a := mustPos(t, "lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1")
	b := mustPos(t, "lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 55")
	if a.RepetitionKey() != b.RepetitionKey() {
		t.Fatal("수 번호만 다른 국면의 RepetitionKey가 달라짐")
	}
	c := mustPos(t, "lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL w - 1")
	if a.RepetitionKey() == c.RepetitionKey() {
		t.Fatal("수번이 다른데 RepetitionKey가 같음")
	}
}

func TestPromotedPieceMoves(t *testing.T) {
	// 馬(승격 角)는 대각 슬라이드 + 십자 한 칸
	pos := mustPos(t, "k8/9/9/9/4+B4/9/9/9/4K4 b - 1")
	targets := map[string]bool{}
	for _, m := range pos.LegalMoves() {
		if !m.IsDrop() && m.From == int8(SquareOf(5, 5)) {
			targets[m.USI()] = true
		}
	}
	for _, want := range []string{"5e5d", "5e5f", "5e4e", "5e6e", "5e1a", "5e9a"} {
		if !targets[want] {
			t.Errorf("馬의 수 %s 누락 (targets=%v)", want, targets)
		}
	}
	// 龍(승격 飛)는 십자 슬라이드 + 대각 한 칸
	pos2 := mustPos(t, "k8/9/9/9/4+R4/9/9/9/4K4 b - 1")
	targets2 := map[string]bool{}
	for _, m := range pos2.LegalMoves() {
		if !m.IsDrop() && m.From == int8(SquareOf(5, 5)) {
			targets2[m.USI()] = true
		}
	}
	for _, want := range []string{"5e4d", "5e6d", "5e4f", "5e6f", "5e5a", "5e1e"} {
		if !targets2[want] {
			t.Errorf("龍의 수 %s 누락", want)
		}
	}
}

func TestInventoryExcess(t *testing.T) {
	if got := StartPosition().InventoryExcess(); len(got) != 0 {
		t.Fatalf("초기국면에 초과 말이 있음: %v", got)
	}
	// 飛가 3장 — 밖에서 들어온 SFEN이 판을 잘못 옮긴 경우
	pos := mustPos(t, "k8/9/9/9/4R4/9/2R6/2R6/4K4 b - 1")
	got := pos.InventoryExcess()
	if got[Rook] != 1 {
		t.Fatalf("飛 초과 1 기대, got %v", got)
	}
}

func TestMoveJa(t *testing.T) {
	// 角換わりの出だし。同 と 成 が両方出る最短の並び。
	got, err := StartPosition().LineJa([]string{"7g7f", "3c3d", "8h2b+", "3a2b"})
	if err != nil {
		t.Fatalf("LineJa: %v", err)
	}
	want := []string{"▲7六歩", "△3四歩", "▲2二角成", "△同銀"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d수째: %q 기대, got %q", i+1, want[i], got[i])
		}
	}

	// 불법수가 섞이면 에러로 끊긴다 — 표기는 합법 수순에만 붙인다
	if _, err := StartPosition().LineJa([]string{"7g7f", "7f7e"}); err == nil {
		t.Fatal("후수 차례에 선수 수를 두었는데 통과함")
	}
}

// TestReasonTextIsComplete 는 사유 코드마다 로그용 이름과 사용자용 일본어 문구가
// 빠짐없이 있는지, 그리고 문구에 한글이 섞이지 않았는지 본다.
//
// 화면에 한글이 한 글자라도 나가면 그 자리에서 "번역이 덜 된 앱"이 된다.
// 사람 눈으로 지키는 규칙은 결국 새므로 여기서 기계가 막는다.
func TestReasonTextIsComplete(t *testing.T) {
	const last = ReasonLeavesKingInCheck

	if len(reasonMessages) != int(last)+1 {
		t.Fatalf("사유 %d개인데 일본어 문구는 %d개 — 새 사유에 문구를 안 붙였다",
			int(last)+1, len(reasonMessages))
	}
	if len(reasonNames) != int(last)+1 {
		t.Fatalf("사유 %d개인데 이름은 %d개", int(last)+1, len(reasonNames))
	}

	for r := ReasonUnknown; r <= last; r++ {
		msg, ok := reasonMessages[r]
		if !ok || msg == "" {
			t.Errorf("사유 %d: 일본어 문구 없음", int(r))
			continue
		}
		if hasHangul(msg) {
			t.Errorf("사유 %d의 문구에 한글: %q", int(r), msg)
		}
		if name, ok := reasonNames[r]; !ok || name == "" {
			t.Errorf("사유 %d: 이름 없음", int(r))
		} else if hasHangul(name) {
			t.Errorf("사유 %d의 이름에 한글: %q", int(r), name)
		}
	}

	// Message()는 등록되지 않은 사유에도 무언가를 돌려줘야 한다 — 화면이 비면 안 된다.
	e := &IllegalMoveError{Reason: Reason(9999), Move: Move{From: -1, To: 40, Drop: Pawn}}
	if e.Message() == "" {
		t.Error("알 수 없는 사유에서 빈 문구")
	}
	if hasHangul(e.Message()) {
		t.Errorf("기본 문구에 한글: %q", e.Message())
	}
}

func hasHangul(s string) bool {
	for _, r := range s {
		switch {
		case r >= 0xAC00 && r <= 0xD7A3, // 한글 음절
			r >= 0x1100 && r <= 0x11FF, // 자모
			r >= 0x3130 && r <= 0x318F: // 호환 자모
			return true
		}
	}
	return false
}

// AttackCount 는 IsAttacked 가 못 하는 「몇 개인가」를 답한다. 玉 주변의 攻め와 守り를
// 견주는 데 쓰므로, 여기서 세다 말면 위에서 「지키던 말이 하나 줄었다」가 안 보인다.
func TestAttackCount(t *testing.T) {
	// 5五에 선수 金 둘이 5六·4六에서 닿고, 후수 飛가 5一에서 세로로 닿는다.
	pos := mustPos(t, "4r4/9/9/9/9/4GG3/9/9/4K4 b - 1")

	if got := pos.AttackCount(SquareOf(5, 5), Black); got != 2 {
		t.Errorf("5五를 노리는 선수 말은 金 둘이다: %d", got)
	}
	if got := pos.AttackCount(SquareOf(5, 5), White); got != 1 {
		t.Errorf("5五를 노리는 후수 말은 飛 하나다: %d", got)
	}

	// 자기 말이 있는 칸도 센다. 방어 利き을 세는 것이 이 함수의 용도라,
	// 지키는 말 위의 利き을 빼면 셀 것이 없어진다.
	if got := pos.AttackCount(SquareOf(5, 6), Black); got != 1 {
		t.Errorf("5六金은 4六金이 지킨다: %d", got)
	}

	// 노는 칸은 0. IsAttacked 와 답이 갈리면 둘 중 하나가 틀린 것이다.
	for _, sq := range []int{SquareOf(1, 1), SquareOf(9, 9), SquareOf(5, 5)} {
		for _, c := range []Color{Black, White} {
			if (pos.AttackCount(sq, c) > 0) != pos.IsAttacked(sq, c) {
				t.Errorf("sq=%d %v: AttackCount와 IsAttacked가 갈린다", sq, c)
			}
		}
	}
}

func TestNeighbors8ClipsAtTheEdge(t *testing.T) {
	if got := len(Neighbors8(SquareOf(5, 5))); got != 8 {
		t.Errorf("한가운데는 8칸이다: %d", got)
	}
	if got := len(Neighbors8(SquareOf(9, 1))); got != 3 {
		t.Errorf("모서리는 3칸이다: %d", got)
	}
	if got := len(Neighbors8(SquareOf(5, 1))); got != 5 {
		t.Errorf("가장자리는 5칸이다: %d", got)
	}
	// 자기 자신은 안 들어간다 — 玉 자신의 칸은 「주변」이 아니다
	for _, sq := range Neighbors8(SquareOf(5, 5)) {
		if sq == SquareOf(5, 5) {
			t.Error("자기 칸이 이웃에 들어갔다")
		}
	}
}
