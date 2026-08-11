package tag

import (
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// **穴熊는 좌표의 근거가 다른 유일한 囲い다.**
//
// 위키백과 본문에 배치 서술이 없어 그림밖에 없고, 그림을 읽으면 좌표가 깨진다.
// 그래서 다른 계층으로 확인한다 — [04-llm.md §4](../../../../docs/04-llm.md)의 신뢰 계층 1,
// **「수순·국면은 자체 엔진으로 재검증」**이다.
//
// 이 테스트가 平手에서 **합법수만으로** 穴熊에 도달한다. 좌표가 틀렸으면 도중에 반칙이
// 나거나 마지막에 태그가 안 뜨므로, 사람이 옮겨 적은 문장보다 강한 근거가 된다.
func TestAnagumaIsReachableFromTheStart(t *testing.T) {
	// 先手가 居飛車穴熊를 짜고, 後手는 방해하지 않고 다른 쪽에서 手를 쓴다.
	//
	//	香9九→9八 로 隅を空け、玉が7八→8八→9九 と入る。銀8八·金7九로 덮는다.
	moves := []string{
		"7g7f", "3c3d", // ▲7六歩  △3四歩
		"5i6h", "8c8d", // ▲6八玉  △8四歩
		"6h7h", "4a3b", // ▲7八玉  △3二金
		"8h7g", "7a6b", // ▲7七角  △6二銀   ← 角が8八を空ける
		"7h8h", "6c6d", // ▲8八玉  △6四歩
		"9i9h", "5c5d", // ▲9八香  △5四歩   ← 香が9九を空ける = 「穴」
		"8h9i", "4c4d", // ▲9九玉  △4四歩   ← 玉が隅に入る
		"7i8h", "2b3c", // ▲8八銀  △3三角
		"6i7i", "3a2b", // ▲7九金  △2二銀   ← 3枚穴熊が完成する
	}

	pos := shogi.StartPosition()
	for i, usi := range moves {
		m, err := shogi.ParseUSIMove(usi)
		if err != nil {
			t.Fatalf("%d수 %s: 파싱 실패 %v", i+1, usi, err)
		}
		if err := pos.ValidateMove(m); err != nil {
			t.Fatalf("%d수 %s: 반칙 — %v", i+1, usi, err)
		}
		pos = pos.Apply(m)
	}

	// 도달한 국면: ln1gk2nl/1r1s2gs1/p1p3bpp/1p1pppp2/9/2P6/PPBPPPPPP/LS5R1/KNG2GSNL b - 19
	for _, want := range []struct {
		file, rank int
		pt         shogi.PieceType
	}{
		{9, 9, shogi.King}, {8, 8, shogi.Silver}, {7, 9, shogi.Gold},
	} {
		sq := shogi.SquareOf(want.file, want.rank)
		if got := pos.Board[sq]; got != shogi.MakePiece(want.pt, shogi.Black) {
			t.Fatalf("%d%d 에 %v 가 없다 (got %v) — 좌표가 틀렸다", want.file, want.rank, want.pt, got)
		}
	}

	got := Detect(Input{Pos: pos, Color: shogi.Black, PlayerMoves: blackMoves(moves)})

	// 居飛車도 함께 떠야 한다 — 飛를 끝까지 振らずに囲った 판이다.
	if want := []string{"ibisha_anaguma", "ibisha"}; len(got) != 2 ||
		got[0].Code != want[0] || got[1].Code != want[1] {
		t.Fatalf("%v 를 기대했는데 %v", want, codes(got))
	}
}

// blackMoves 는 짝수 인덱스(先手)만 골라낸다.
func blackMoves(all []string) []string {
	var out []string
	for i, m := range all {
		if i%2 == 0 {
			out = append(out, m)
		}
	}
	return out
}

// 振り飛車穴熊도 같은 방법으로 확인한다 — 玉이 반대쪽 隅(1九)에 들어간다.
//
// 좌우를 뒤집은 자리라 `squareFor` 의 거울(後手용 180° 회전)로는 안 나온다. 그래서
// 정의를 따로 적었고, 그 좌표를 여기서 룰 엔진에 다시 물어 확인한다.
func TestFuribishaAnagumaIsReachableFromTheStart(t *testing.T) {
	moves := []string{
		"2h6h", "3c3d", // ▲6八飛  △3四歩   ← 四間に振る
		"5i4h", "8c8d", // ▲4八玉  △8四歩
		"4h3h", "4a3b", // ▲3八玉  △3二金
		"3h2h", "7a6b", // ▲2八玉  △6二銀   ← 飛が居た2八に入る
		"1i1h", "6c6d", // ▲1八香  △6四歩   ← 香が1九を空ける
		"2h1i", "5c5d", // ▲1九玉  △5四歩   ← 玉が隅に入る
		"3i2h", "4c4d", // ▲2八銀  △4四歩
		"4i3i", "2b3c", // ▲3九金  △3三角   ← 振り飛車穴熊が完成する
	}

	pos := shogi.StartPosition()
	for i, usi := range moves {
		m, err := shogi.ParseUSIMove(usi)
		if err != nil {
			t.Fatalf("%d수 %s: 파싱 실패 %v", i+1, usi, err)
		}
		if err := pos.ValidateMove(m); err != nil {
			t.Fatalf("%d수 %s: 반칙 — %v", i+1, usi, err)
		}
		pos = pos.Apply(m)
	}

	got := Detect(Input{Pos: pos, Color: shogi.Black, PlayerMoves: blackMoves(moves)})
	if want := []string{"furibisha_anaguma", "shiken_bisha"}; len(got) != 2 ||
		got[0].Code != want[0] || got[1].Code != want[1] {
		t.Fatalf("%v 를 기대했는데 %v (%s)", want, codes(got), pos.SFEN())
	}
}

// 左美濃도 도달로 확인한다. 위키백과가 玉8八만 본문에 적고 金銀은 그림에 두었다.
//
// 순서가 빡빡하다 — 銀이 7七로 가려면 7七歩가 먼저 나가야 하고, 玉이 8八에 서려면
// 角이 8八을 비워야 하고, 金이 6八에 오려면 玉이 그 칸을 지나가 있어야 한다.
// **그 제약을 사람이 세지 않고 룰 엔진이 세게 한다.**
func TestHidariMinoIsReachableFromTheStart(t *testing.T) {
	moves := []string{
		"7g7f", "3c3d", // ▲7六歩  △3四歩   ← 7七を空ける
		"8h6f", "8c8d", // ▲6六角  △8四歩   ← 角が8八を空ける
		"7i7h", "4a3b", // ▲7八銀  △3二金
		"7h7g", "7a6b", // ▲7七銀  △6二銀
		"5i6h", "6c6d", // ▲6八玉  △6四歩
		"6i7h", "5c5d", // ▲7八金  △5四歩
		"6h7i", "4c4d", // ▲7九玉  △4四歩
		"7i8h", "2b3c", // ▲8八玉  △3三角
		"4i5h", "3a2b", // ▲5八金  △2二銀
		"5h6h", "6b5c", // ▲6八金  △5三銀   ← 左美濃が完成する
	}

	pos := shogi.StartPosition()
	for i, usi := range moves {
		m, err := shogi.ParseUSIMove(usi)
		if err != nil {
			t.Fatalf("%d수 %s: 파싱 실패 %v", i+1, usi, err)
		}
		if err := pos.ValidateMove(m); err != nil {
			t.Fatalf("%d수 %s: 반칙 — %v", i+1, usi, err)
		}
		pos = pos.Apply(m)
	}

	got := Detect(Input{Pos: pos, Color: shogi.Black, PlayerMoves: blackMoves(moves)})
	if want := []string{"hidari_mino", "ibisha"}; len(got) != 2 ||
		got[0].Code != want[0] || got[1].Code != want[1] {
		t.Fatalf("%v 를 기대했는데 %v (%s)", want, codes(got), pos.SFEN())
	}
}

// 天守閣美濃는 左美濃에서 玉을 한 段 올린 것이다. 8七에 서려면 8七歩가 먼저 나가야 한다.
func TestTenshukakuMinoIsReachableFromTheStart(t *testing.T) {
	moves := []string{
		"7g7f", "3c3d", "8h6f", "8c8d", "7i7h", "4a3b", "7h7g", "7a6b",
		"5i6h", "6c6d", "6i7h", "5c5d", "6h7i", "4c4d", "7i8h", "2b3c",
		"4i5h", "3a2b", "5h6h", "6b5c",
		"8g8f", "5c6b", // ▲8六歩  △6二銀   ← 8七を空ける
		"8h8g", "6b5c", // ▲8七玉  △5三銀   ← 天守閣美濃が完成する
	}

	pos := shogi.StartPosition()
	for i, usi := range moves {
		m, err := shogi.ParseUSIMove(usi)
		if err != nil {
			t.Fatalf("%d수 %s: 파싱 실패 %v", i+1, usi, err)
		}
		if err := pos.ValidateMove(m); err != nil {
			t.Fatalf("%d수 %s: 반칙 — %v", i+1, usi, err)
		}
		pos = pos.Apply(m)
	}

	got := Detect(Input{Pos: pos, Color: shogi.Black, PlayerMoves: blackMoves(moves)})
	if want := []string{"tenshukaku_mino", "ibisha"}; len(got) != 2 ||
		got[0].Code != want[0] || got[1].Code != want[1] {
		t.Fatalf("%v 를 기대했는데 %v (%s)", want, codes(got), pos.SFEN())
	}
}

// ミレニアム囲い — 桂가 8九를 비워야 玉이 들어간다. 그 순서가 곧 이 囲い의 정의다.
func TestMillenniumIsReachableFromTheStart(t *testing.T) {
	moves := []string{
		"7g7f", "3c3d", // ▲7六歩  △3四歩   ← 7七を空ける
		"8h6f", "8c8d", // ▲6六角  △8四歩   ← 角が7七を通って出る
		"8i7g", "4a3b", // ▲7七桂  △3二金   ← 桂が8九を空ける
		"7i8h", "7a6b", // ▲8八銀  △6二銀   ← 銀が7九を空ける
		"5i6h", "6c6d", // ▲6八玉  △6四歩
		"6h7i", "5c5d", // ▲7九玉  △5四歩
		"7i8i", "4c4d", // ▲8九玉  △4四歩   ← 玉が深く入る
		"6i7i", "2b3c", // ▲7九金  △3三角   ← ミレニアムが完成する
	}

	pos := shogi.StartPosition()
	for i, usi := range moves {
		m, err := shogi.ParseUSIMove(usi)
		if err != nil {
			t.Fatalf("%d수 %s: 파싱 실패 %v", i+1, usi, err)
		}
		if err := pos.ValidateMove(m); err != nil {
			t.Fatalf("%d수 %s: 반칙 — %v", i+1, usi, err)
		}
		pos = pos.Apply(m)
	}

	got := Detect(Input{Pos: pos, Color: shogi.Black, PlayerMoves: blackMoves(moves)})
	if want := []string{"millennium", "ibisha"}; len(got) != 2 ||
		got[0].Code != want[0] || got[1].Code != want[1] {
		t.Fatalf("%v 를 기대했는데 %v (%s)", want, codes(got), pos.SFEN())
	}
}
