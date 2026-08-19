package handicap

import (
	"fmt"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// ja 는 실패 문장을 읽을 수 있게 하려고만 있다 — `shogi.PieceType` 에 String() 이 없다.
func ja(t shogi.PieceType) string {
	switch t {
	case shogi.Lance:
		return "香"
	case shogi.Knight:
		return "桂"
	case shogi.Bishop:
		return "角"
	case shogi.Rook:
		return "飛"
	default:
		return fmt.Sprintf("종류%d", t)
	}
}

// TestSFENIsTheHandicapItSaysItIs 는 표의 SFEN이 이름과 맞는지를 **판에서** 확인한다.
//
// 이 표는 손으로 적은 문자열이라 한 글자만 틀려도 이름과 다른 판이 뜨고, 그 판은 파싱도
// 되고 대국도 돌아서 **아무 테스트도 안 잡는다.** 그래서 「무엇이 빠졌나」를 세는 쪽으로 본다.
func TestSFENIsTheHandicapItSaysItIs(t *testing.T) {
	// 上手(後手)에서 빠져야 하는 駒. 落とす 것이 늘어나는 쪽으로 쌓인다.
	want := map[string][]shogi.PieceType{
		"kyoochi":     {shogi.Lance},
		"kakuochi":    {shogi.Bishop},
		"hishaochi":   {shogi.Rook},
		"hikyoochi":   {shogi.Rook, shogi.Lance},
		"nimaiochi":   {shogi.Rook, shogi.Bishop},
		"yonmaiochi":  {shogi.Rook, shogi.Bishop, shogi.Lance, shogi.Lance},
		"rokumaiochi": {shogi.Rook, shogi.Bishop, shogi.Lance, shogi.Lance, shogi.Knight, shogi.Knight},
	}
	if len(want) != len(All()) {
		t.Fatalf("표에 %d개인데 확인은 %d개다", len(All()), len(want))
	}

	start := shogi.StartPosition()
	for _, h := range All() {
		missing, ok := want[h.ID]
		if !ok {
			t.Errorf("%s: 확인할 목록이 없다", h.ID)
			continue
		}

		pos, err := shogi.ParseSFEN(h.SFEN)
		if err != nil {
			t.Errorf("%s: %v", h.ID, err)
			continue
		}

		// **下手부터 둔다.** 駒落ち에 先手/後手가 없고, 이 값이 뒤집히면 사람이 上手를
		// 잡은 채로 판이 열린다.
		if pos.Turn != shogi.Black {
			t.Errorf("%s: 手番이 %v다. 下手(Black)여야 한다", h.ID, pos.Turn)
		}
		if pos.Hands != start.Hands {
			t.Errorf("%s: 持ち駒가 있다. 駒落ち는 판에서 빼는 것이다", h.ID)
		}

		// 빠진 駒를 세운 판과 평수를 비교한다.
		gone := map[shogi.PieceType]int{}
		for sq := range pos.Board {
			s, p := start.Board[sq], pos.Board[sq]
			switch {
			case s == p:
			case p.Empty() && s.Color() == shogi.White:
				gone[s.Type()]++
			default:
				t.Errorf("%s: %d번 칸이 평수와 다르다(%s → %s)", h.ID, sq, ja(s.Type()), ja(p.Type()))
			}
		}
		expect := map[shogi.PieceType]int{}
		for _, pt := range missing {
			expect[pt]++
		}
		if len(gone) != len(expect) {
			t.Errorf("%s: 빠진 종류가 %d가지다. %d가지여야 한다", h.ID, len(gone), len(expect))
			continue
		}
		for pt, n := range expect {
			if gone[pt] != n {
				t.Errorf("%s: %s 가 %d개 빠졌다. %d개여야 한다", h.ID, ja(pt), gone[pt], n)
			}
		}

		// 판이 실제로 둘 수 있는 국면인가. 시작 국면이라 詰み도 王手도 아니어야 한다.
		if pos.InCheck(shogi.Black) || pos.InCheck(shogi.White) {
			t.Errorf("%s: 시작 국면인데 王手다", h.ID)
		}
		if len(pos.LegalMoves()) == 0 {
			t.Errorf("%s: 둘 수 있는 수가 없다", h.ID)
		}
	}
}

// TestLeftLanceIsTheOneThatGoes 는 落とす 香가 **上手의 왼쪽**인지 본다.
//
// 좌우를 바꿔 적어도 위 테스트는 통과한다(빠진 종류와 개수가 같다). 上手는 판을 반대에서
// 보므로 **1筋이 왼쪽**이고, 手合割의 香落ち는 그 香다.
func TestLeftLanceIsTheOneThatGoes(t *testing.T) {
	for _, id := range []string{"kyoochi", "hikyoochi"} {
		h, ok := Find(id)
		if !ok {
			t.Fatalf("%s: 표에 없다", id)
		}
		pos, err := shogi.ParseSFEN(h.SFEN)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if !pos.Board[shogi.SquareOf(1, 1)].Empty() {
			t.Errorf("%s: 1一이 비어 있어야 한다 — 上手의 左香다", id)
		}
		if p := pos.Board[shogi.SquareOf(9, 1)]; p.Empty() {
			t.Errorf("%s: 9一의 香까지 빠졌다", id)
		}
	}
}

func TestFindAndOfRoundTrip(t *testing.T) {
	for _, h := range All() {
		got, ok := Find(h.ID)
		if !ok || got.ID != h.ID {
			t.Errorf("Find(%q) = %v, %v", h.ID, got.ID, ok)
		}
		if back, ok := Of(h.SFEN); !ok || back.ID != h.ID {
			t.Errorf("Of(%q) = %v, %v", h.ID, back.ID, ok)
		}
		if got := NameOf(h.SFEN); got != h.Name {
			t.Errorf("NameOf(%s) = %q, want %q", h.ID, got, h.Name)
		}
		if got := BaselineCp(h.SFEN); got != h.BaselineCp {
			t.Errorf("BaselineCp(%s) = %d, want %d", h.ID, got, h.BaselineCp)
		}
	}
}

// TestOfIgnoresMoveNumber 는 手数가 달라도 같은 手合으로 붙는지 본다.
//
// **왕복이 실제로 일어난다** — 되짚기가 `Position.SFEN()` 으로 다시 적은 문자열을 쓰고
// (server/review.go), 이어하는 판은 `games.start_sfen` 에 적힌 것을 쓴다.
func TestOfIgnoresMoveNumber(t *testing.T) {
	h, _ := Find("nimaiochi")
	pos, err := shogi.ParseSFEN(h.SFEN)
	if err != nil {
		t.Fatal(err)
	}
	pos.MoveNum = 41
	if got, ok := Of(pos.SFEN()); !ok || got.ID != h.ID {
		t.Errorf("Of(%q) = %v, %v", pos.SFEN(), got.ID, ok)
	}
}

// TestHirateIsNotInTheTable 는 平手가 「手合割 없음」으로 답하는지 본다. 표에 넣는 날
// 기준점이 0에서 91(실측)로 옮겨 가고, 그러면 지금까지의 상수 측정이 다른 기준의 것이 된다
// (패키지 주석).
func TestHirateIsNotInTheTable(t *testing.T) {
	for _, sfen := range []string{"", shogi.StartSFEN} {
		if h, ok := Of(sfen); ok {
			t.Errorf("Of(%q) = %s, 없어야 한다", sfen, h.ID)
		}
		if got := BaselineCp(sfen); got != 0 {
			t.Errorf("BaselineCp(%q) = %d, want 0", sfen, got)
		}
		if got := NameOf(sfen); got != "" {
			t.Errorf("NameOf(%q) = %q, want \"\"", sfen, got)
		}
	}
	if _, ok := Find("hirate"); ok {
		t.Error(`Find("hirate") 가 찾았다`)
	}
	if _, ok := Find(""); ok {
		t.Error(`Find("") 가 찾았다`)
	}
}

func TestBaselineCpForFlipsForUwate(t *testing.T) {
	h, _ := Find("rokumaiochi")
	if got := BaselineCpFor(h.SFEN, shogi.Black); got != h.BaselineCp {
		t.Errorf("下手 관점 = %d, want %d", got, h.BaselineCp)
	}
	if got := BaselineCpFor(h.SFEN, shogi.White); got != -h.BaselineCp {
		t.Errorf("上手 관점 = %d, want %d", got, -h.BaselineCp)
	}
	// 平手는 어느 관점에서도 0이다 — 뒤집을 것이 없다.
	if got := BaselineCpFor("", shogi.White); got != 0 {
		t.Errorf("平手 上手 관점 = %d, want 0", got)
	}
}

// TestBaselineRisesWithTheHandicap 은 표의 순서와 기준점의 순서가 같은지 본다.
//
// 값을 다시 재서 옮기는 날(baseline_measure_test.go) 한 줄만 고치면 순서가 깨지고, 그러면
// 화면의 목록이 「조금 접는 것」부터가 아니게 된다. 飛車落ち와 角落ち만 예외다 —
// 실측이 741·756으로 사실상 같고, 그 둘의 순서는 手合割의 관례가 정한다.
func TestBaselineRisesWithTheHandicap(t *testing.T) {
	prev := 0
	for _, h := range All() {
		if h.BaselineCp <= 0 {
			t.Errorf("%s: 기준점이 %d다. 駒落ち는 下手가 유리하다", h.ID, h.BaselineCp)
		}
		if h.ID == "hishaochi" {
			continue // 角落ち와 6cp 차이라 순서를 못 세운다
		}
		if h.BaselineCp < prev {
			t.Errorf("%s: 기준점 %d가 앞 항목 %d보다 작다", h.ID, h.BaselineCp, prev)
		}
		prev = h.BaselineCp
	}
}
