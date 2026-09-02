package kifu

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// ki2Of 는 수순을 원위치 없는 표기로 다시 적는다. 화면이 쓰는 렌더러 그대로다.
func ki2Of(t *testing.T, g ParsedGame) string {
	t.Helper()
	pos, err := shogi.ParseSFEN(g.StartSFEN)
	if err != nil {
		t.Fatal(err)
	}
	ja, err := pos.LineJa(g.Moves)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(ja, "\n")
}

// 이 왕복이 성립해야 KI2 가 LLM 없이 읽힌다. 렌더러(MoveJa)는 원위치를 안 적고 수식어만
// 붙이므로, 되읽으려면 룰 엔진이 출발칸을 되찾아야 한다(shogi.ResolveOrigin).
func TestRenderedNotationReadsBack(t *testing.T) {
	data, err := os.ReadFile("testdata/sample.kif")
	if err != nil {
		t.Fatal(err)
	}
	want, err := ParseKIF(string(data))
	if err != nil {
		t.Fatal(err)
	}

	got, err := ParseKI2(ki2Of(t, want))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Moves) != len(want.Moves) {
		t.Fatalf("len(Moves) = %d, want %d", len(got.Moves), len(want.Moves))
	}
	for i := range want.Moves {
		if got.Moves[i] != want.Moves[i] {
			t.Errorf("move %d = %q, want %q", i+1, got.Moves[i], want.Moves[i])
		}
	}
}

// 같은 왕복을 실 코퍼스 전부로 건다. 수식어가 실제로 갈리는 국면(金 둘·銀 둘·龍의 寄)은
// 13手짜리 표본에 거의 안 나온다.
func TestRenderedNotationReadsBack_Floodgate(t *testing.T) {
	files, err := os.ReadDir("testdata/floodgate")
	if err != nil {
		t.Skip("no floodgate testdata")
	}
	games, plies := 0, 0
	for _, f := range files {
		data, err := os.ReadFile("testdata/floodgate/" + f.Name())
		if err != nil {
			t.Fatal(err)
		}
		want, err := ParseCSA(string(data))
		if err != nil || len(want.Moves) == 0 {
			continue
		}
		got, err := ParseKI2(ki2Of(t, want))
		if err != nil {
			t.Errorf("%s: %v", f.Name(), err)
			continue
		}
		if len(got.Moves) != len(want.Moves) {
			t.Errorf("%s: len(Moves) = %d, want %d", f.Name(), len(got.Moves), len(want.Moves))
			continue
		}
		for i := range want.Moves {
			if got.Moves[i] != want.Moves[i] {
				t.Errorf("%s: move %d = %q, want %q", f.Name(), i+1, got.Moves[i], want.Moves[i])
				break
			}
		}
		games++
		plies += len(want.Moves)
	}
	t.Logf("round-tripped %d games, %d moves", games, plies)
	if games == 0 {
		t.Fatal("no game round-tripped")
	}
}

// 수식어가 안 좁히면 고르지 않는다. 골라 버리면 그 뒤가 통째로 다른 판이 되는데
// 합법수라 ValidateMove 도 안 잡는다.
func TestAmbiguousNotationIsRefused(t *testing.T) {
	// 6八金과 4八金이 둘 다 5八로 갈 수 있다. 「5八金」만으로는 어느 쪽인지 안 정해진다.
	pos, err := shogi.ParseSFEN("8k/9/9/9/9/9/9/3G1G3/K8 b - 1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseKIFMove("5八金", pos, -1); err == nil {
		t.Fatal("parsed an ambiguous move; it must refuse instead of guessing")
	}
	// 수식어가 붙으면 갈린다. 先手는 筋 번호가 작을수록 오른쪽이다.
	m, err := parseKIFMove("5八金右", pos, -1)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.USI(); got != "4h5h" {
		t.Errorf("USI = %q, want 4h5h", got)
	}
}

// 수식어가 成 앞에 오는 표기. 이걸 못 읽으면 승격이 조용히 빠지고, 남는 수가 합법수라
// ValidateMove 도 안 잡는다.
func TestModifierBeforePromotion(t *testing.T) {
	// 7三銀과 5三銀이 둘 다 6二로 갈 수 있고, 6二는 成れる 자리다.
	pos, err := shogi.ParseSFEN("8k/9/2S1S4/9/9/9/9/9/K8 b - 1")
	if err != nil {
		t.Fatal(err)
	}
	m, err := parseKIFMove("6二銀右成", pos, -1)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Promote {
		t.Error("Promote = false; the 成 after the modifier was dropped")
	}
}

func TestParseUSI(t *testing.T) {
	want := []string{"7g7f", "3c3d", "8h2b+"}
	for _, in := range []string{
		"position startpos moves 7g7f 3c3d 8h2b+",
		"startpos moves 7g7f 3c3d 8h2b+",
		"7g7f 3c3d 8h2b+",
		"7g7f\n3c3d\n8h2b+\n",
	} {
		g, err := ParseUSI(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if len(g.Moves) != len(want) {
			t.Fatalf("%q: len(Moves) = %d, want %d", in, len(g.Moves), len(want))
		}
		for i := range want {
			if g.Moves[i] != want[i] {
				t.Errorf("%q: move %d = %q, want %q", in, i+1, g.Moves[i], want[i])
			}
		}
	}
}

// USI 는 사람이 쓰는 표기가 아니라서, 모르는 낱말은 「이 텍스트는 USI 가 아니다」다.
// 건너뛰면 남의 기보를 반쯤 읽는다.
func TestParseUSIRefusesUnknownWords(t *testing.T) {
	if _, err := ParseUSI("7g7f 3c3d ７六歩"); err == nil {
		t.Fatal("parsed a non-USI word")
	}
}

func TestParseUSITakesAStartSFEN(t *testing.T) {
	sfen := "lnsgkgsn1/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL w - 1"
	g, err := ParseUSI("position sfen " + sfen + " moves 3c3d")
	if err != nil {
		t.Fatal(err)
	}
	if g.StartSFEN != sfen {
		t.Errorf("StartSFEN = %q, want %q", g.StartSFEN, sfen)
	}
}

// 手合割 줄을 안 읽으면 駒落ち 기보가 平手 위에서 읽히다가 엉뚱한 手数에 반칙으로 죽는다.
func TestHandicapHeaderSetsTheStart(t *testing.T) {
	kif := "手合割：香落ち\n先手：下手\n後手：上手\n   1 ３四歩(33)\n   2 ７六歩(77)\n"
	g, err := ParseKIF(kif)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(g.StartSFEN, "lnsgkgsn1/") {
		t.Errorf("StartSFEN = %q, want the 香落ち position", g.StartSFEN)
	}
	// 上手가 먼저 둔다. 平手 위에서 읽었다면 첫 수가 반칙이라 여기 못 온다.
	if len(g.Moves) != 2 || g.Moves[0] != "3c3d" {
		t.Errorf("Moves = %v, want the uwate to move first", g.Moves)
	}
}

// 모르는 手合을 平手로 읽으면 첫 수부터 반칙이 되고, 그 오류가 手合 때문이라는 것을
// 아무도 못 본다.
func TestUnknownHandicapIsRefused(t *testing.T) {
	if _, err := ParseKIF("手合割：八枚落ち\n   1 ３四歩(33)\n"); err == nil {
		t.Fatal("accepted a handicap that is not in the table")
	}
}

func TestReadPicksTheNotation(t *testing.T) {
	kif, err := os.ReadFile("testdata/sample.kif")
	if err != nil {
		t.Fatal(err)
	}
	csa, err := os.ReadFile("testdata/sample.csa")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		in   string
		want Notation
	}{
		{"kif", string(kif), NotationKIF},
		{"csa", string(csa), NotationCSA},
		{"usi", "position startpos moves 7g7f 3c3d", NotationUSI},
		{"ki2", "▲7六歩 △3四歩 ▲2六歩", NotationKI2},
		{"plain", "７六歩 ３四歩 ２六歩", NotationPlain},
	} {
		g, got, err := Read(tc.in)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: notation = %q, want %q", tc.name, got, tc.want)
		}
		if len(g.Moves) == 0 {
			t.Errorf("%s: no moves", tc.name)
		}
	}
}

func TestReadRefusesJunk(t *testing.T) {
	if _, _, err := Read("これは棋譜ではありません"); !errors.Is(err, ErrNoMoves) {
		t.Fatalf("err = %v, want ErrNoMoves", err)
	}
}

// 몇 手目에서 깨졌는지가 화면에 나간다. 문구에서 번호를 다시 뽑는 코드는 오류 문구를
// 고치는 날 조용히 낡는다.
func TestReadSaysWhichMoveBroke(t *testing.T) {
	_, _, err := Read("▲7六歩 △3四歩 ▲9九玉")
	var me *MoveError
	if !errors.As(err, &me) {
		t.Fatalf("err = %v, want a *MoveError", err)
	}
	if me.Ply != 3 {
		t.Errorf("Ply = %d, want 3", me.Ply)
	}
}
