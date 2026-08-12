package kifu

import (
	"os"
	"testing"
)

func TestParseKIF(t *testing.T) {
	data, err := os.ReadFile("testdata/sample.kif")
	if err != nil {
		t.Fatal(err)
	}
	g, err := ParseKIF(string(data))
	if err != nil {
		t.Fatal(err)
	}

	if g.Sente != "藤井聡太" {
		t.Errorf("Sente = %q, want 藤井聡太", g.Sente)
	}
	if g.Gote != "渡辺明" {
		t.Errorf("Gote = %q, want 渡辺明", g.Gote)
	}
	if g.Result != ResultSenteWin {
		t.Errorf("Result = %v, want SenteWin (投了 on gote's turn)", g.Result)
	}
	if len(g.Moves) != 13 {
		t.Fatalf("len(Moves) = %d, want 13", len(g.Moves))
	}

	wantMoves := []string{
		"7g7f", // ７六歩(77)
		"3c3d", // ３四歩(33)
		"2g2f", // ２六歩(27)
		"8c8d", // ８四歩(83)
		"2f2e", // ２五歩(26)
		"8d8e", // ８五歩(84)
		"6i7h", // ７八金(69)
		"4a3b", // ３二金(41)
		"2e2d", // ２四歩(25)
		"2c2d", // 同　歩(23)
		"2h2d", // 同　飛(28)
		"P*2c", // ２三歩打
		"2d2f", // ２六飛(24)
	}
	for i, want := range wantMoves {
		if g.Moves[i] != want {
			t.Errorf("Move[%d] = %q, want %q", i, g.Moves[i], want)
		}
	}
}

func TestParseCSA(t *testing.T) {
	data, err := os.ReadFile("testdata/sample.csa")
	if err != nil {
		t.Fatal(err)
	}
	g, err := ParseCSA(string(data))
	if err != nil {
		t.Fatal(err)
	}

	if g.Sente != "Fujii Souta" {
		t.Errorf("Sente = %q, want Fujii Souta", g.Sente)
	}
	if g.Gote != "Watanabe Akira" {
		t.Errorf("Gote = %q, want Watanabe Akira", g.Gote)
	}
	if g.Result != ResultSenteWin {
		t.Errorf("Result = %v, want SenteWin (投了 on gote's turn)", g.Result)
	}
	if len(g.Moves) != 13 {
		t.Fatalf("len(Moves) = %d, want 13", len(g.Moves))
	}

	wantMoves := []string{
		"7g7f", "3c3d", "2g2f", "8c8d",
		"2f2e", "8d8e", "6i7h", "4a3b",
		"2e2d", "2c2d", "2h2d", "P*2c", "2d2f",
	}
	for i, want := range wantMoves {
		if g.Moves[i] != want {
			t.Errorf("Move[%d] = %q, want %q", i, g.Moves[i], want)
		}
	}
}

func TestParseKIF_Promote(t *testing.T) {
	kif := `先手：A
後手：B
   1 ７六歩(77)
   2 ３四歩(33)
   3 ２二角成(88)
   4 投了
`
	g, err := ParseKIF(kif)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Moves) != 3 {
		t.Fatalf("len(Moves) = %d, want 3", len(g.Moves))
	}
	if g.Moves[2] != "8h2b+" {
		t.Errorf("promote move = %q, want 8h2b+", g.Moves[2])
	}
}

func TestParseKIF_Drop(t *testing.T) {
	kif := `先手：A
後手：B
   1 ７六歩(77)
   2 ３四歩(33)
   3 ２二角成(88)
   4 同　銀(31)
   5 ４五角打
   6 投了
`
	g, err := ParseKIF(kif)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Moves) != 5 {
		t.Fatalf("len(Moves) = %d, want 5", len(g.Moves))
	}
	if g.Moves[2] != "8h2b+" {
		t.Errorf("Move[2] = %q, want 8h2b+", g.Moves[2])
	}
	if g.Moves[3] != "3a2b" {
		t.Errorf("Move[3] = %q, want 3a2b", g.Moves[3])
	}
	if g.Moves[4] != "B*4e" {
		t.Errorf("Move[4] = %q, want B*4e", g.Moves[4])
	}
}

func TestParseCSA_Promote(t *testing.T) {
	csa := `N+A
N-B
P1-KY-KE-GI-KI-OU-KI-GI-KE-KY
P2 * -HI *  *  *  *  * -KA *
P3-FU-FU-FU-FU-FU-FU-FU-FU-FU
P4 *  *  *  *  *  *  *  *  *
P5 *  *  *  *  *  *  *  *  *
P6 *  *  *  *  *  *  *  *  *
P7+FU+FU+FU+FU+FU+FU+FU+FU+FU
P8 * +KA *  *  *  *  * +HI *
P9+KY+KE+GI+KI+OU+KI+GI+KE+KY
+
+7776FU
-3334FU
+8822UM
%TORYO
`
	g, err := ParseCSA(csa)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Moves) != 3 {
		t.Fatalf("len(Moves) = %d, want 3", len(g.Moves))
	}
	if g.Moves[2] != "8h2b+" {
		t.Errorf("promote move = %q, want 8h2b+", g.Moves[2])
	}
}

func TestParseCSA_Drop(t *testing.T) {
	csa := `N+A
N-B
P1-KY-KE-GI-KI-OU-KI-GI-KE-KY
P2 * -HI *  *  *  *  * -KA *
P3-FU-FU-FU-FU-FU-FU-FU-FU-FU
P4 *  *  *  *  *  *  *  *  *
P5 *  *  *  *  *  *  *  *  *
P6 *  *  *  *  *  *  *  *  *
P7+FU+FU+FU+FU+FU+FU+FU+FU+FU
P8 * +KA *  *  *  *  * +HI *
P9+KY+KE+GI+KI+OU+KI+GI+KE+KY
+
+7776FU
-3334FU
+8822UM
-3122GI
+0045KA
%TORYO
`
	g, err := ParseCSA(csa)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Moves) != 5 {
		t.Fatalf("len(Moves) = %d, want 5", len(g.Moves))
	}
	if g.Moves[4] != "B*4e" {
		t.Errorf("drop move = %q, want B*4e", g.Moves[4])
	}
}

func TestParseCSA_Floodgate(t *testing.T) {
	files, err := os.ReadDir("testdata/floodgate")
	if err != nil {
		t.Skip("no floodgate testdata")
	}
	// **결과를 모르는 판은 실패가 아니다.** floodgate 실 코퍼스에는 종국 표시가 아예
	// 없는 판(끊긴 대국)이 섞여 있다 — 341판 중 16판이었다. 그걸 에러로 두면 이 테스트가
	// 파서 검사가 아니라 **코퍼스 크기 감지기**가 되어, 기보를 늘릴 때마다 빨개진다.
	//
	// 대신 **센다.** 조용히 넘기면 「파서가 다 읽었다」와 「절반이 결과 없이 지나갔다」가
	// 같은 초록으로 보인다.
	parsed, short, unknown := 0, 0, 0
	for _, f := range files {
		data, err := os.ReadFile("testdata/floodgate/" + f.Name())
		if err != nil {
			t.Fatal(err)
		}
		g, err := ParseCSA(string(data))
		if err != nil {
			t.Errorf("%s: %v", f.Name(), err)
			continue
		}
		parsed++
		if len(g.Moves) < 10 {
			short++
		}
		if g.Result == ResultUnknown {
			unknown++
		}
	}
	t.Logf("parsed %d/%d games — %d under 10 moves, %d without a recorded result", parsed, len(files), short, unknown)

	// 읽히지 않는 판이 하나라도 있으면 위에서 이미 실패했다. 여기서 보는 것은 **비율**이다:
	// 결과 없는 판이 절반을 넘으면 코퍼스가 이상하거나 파서가 종국 표시를 놓치고 있다.
	if parsed > 0 && unknown*2 > parsed {
		t.Errorf("결과를 모르는 판이 %d/%d 로 절반을 넘는다 — 종국 표시를 놓치고 있다", unknown, parsed)
	}
}

func TestParseKIF_Draw(t *testing.T) {
	kif := `先手：A
後手：B
   1 ７六歩(77)
   2 千日手
`
	g, err := ParseKIF(kif)
	if err != nil {
		t.Fatal(err)
	}
	if g.Result != ResultDraw {
		t.Errorf("Result = %v, want Draw", g.Result)
	}
	if len(g.Moves) != 1 {
		t.Errorf("len(Moves) = %d, want 1", len(g.Moves))
	}
}
