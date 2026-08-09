package game

import (
	"context"
	"errors"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

type stubMulti struct {
	res usi.SearchResult
	err error
	k   int // 마지막으로 요청받은 MultiPV
}

func (s *stubMulti) SearchMultiPV(_ context.Context, _ string, _ []string, _, multiPV int) (usi.SearchResult, error) {
	s.k = multiPV
	return s.res, s.err
}

// line 은 상대(수번 측) 관점 cp로 후보 하나를 만든다.
func line(move string, engineCp int) usi.SearchLine {
	return usi.SearchLine{Depth: 12, MultiPV: 1, Move: move, ScoreCp: engineCp}
}

func chooseFrom(t *testing.T, best string, lines ...usi.SearchLine) string {
	t.Helper()
	s := &stubMulti{res: usi.SearchResult{Best: best, Lines: lines}}
	o := NewAdaptiveOpponent(s, 12, DefaultBand)
	got, err := o.Choose(t.Context(), shogi.StartSFEN, nil)
	if err != nil {
		t.Fatalf("Choose: %v", err)
	}
	if s.k != CandidateK {
		t.Errorf("MultiPV %d 을 요청해야 한다: %d", CandidateK, s.k)
	}
	return got
}

// 밴드에 드는 후보가 있으면 **최선수를 두지 않는다.** 그게 이 상대의 전부다.
func TestAdaptivePicksTheBandNotTheBest(t *testing.T) {
	// 엔진 관점 cp를 뒤집으면 플레이어 관점이다. 밴드는 플레이어 +100~+300.
	got := chooseFrom(t, "7g7f",
		line("7g7f", 400),  // 플레이어 −400. 상대에게 최선이고 밴드 밖
		line("2g2f", -200), // 플레이어 +200. 밴드 안 ← 이걸 골라야 한다
		line("6g6f", -900), // 플레이어 +900. 너무 양보했다
	)
	if got != "2g2f" {
		t.Errorf("밴드 안의 수를 골라야 한다: %q", got)
	}
}

// **지고 있을 때는 저절로 최선을 다한다.**
//
// 후보가 전부 밴드 위(플레이어가 이미 크게 유리)면 거리를 최소화하는 것이 곧
// 플레이어 우세를 가장 줄이는 수 = 엔진의 최선수다. 규칙을 따로 두지 않아도 나온다.
func TestAdaptiveFightsBackWhenLosing(t *testing.T) {
	got := chooseFrom(t, "7g7f",
		line("7g7f", -1500), // 플레이어 +1500 — 이 중 제일 낫다
		line("2g2f", -2200),
		line("6g6f", -3000),
	)
	if got != "7g7f" {
		t.Errorf("불리할 때는 최선수를 둬야 한다: %q", got)
	}
}

// **이기고 있을 때만 양보한다.**
//
// 여기서 최선수로 물러서면 조절이 가장 필요한 자리에서 초심자를 그대로 뭉갠다.
func TestAdaptiveEasesOffWhenWinning(t *testing.T) {
	got := chooseFrom(t, "7g7f",
		line("7g7f", 3000), // 플레이어 −3000. 최선수
		line("2g2f", 2200),
		line("6g6f", 1500), // 플레이어 −1500. 제일 너그럽다 ← 이걸 골라야 한다
	)
	if got != "6g6f" {
		t.Errorf("유리할 때는 가장 너그러운 수를 골라야 한다: %q", got)
	}
}

// 「던지지 않는다」 — 밴드에 아무리 잘 맞아도 駒를 그냥 주는 수는 안 고른다.
//
// 이 필터는 **엔진이 필요 없다.** 룰 엔진만으로 된다.
func TestAdaptiveNeverThrowsAPiece(t *testing.T) {
	// ▲7六歩 뒤 後手 차례. △8八角成(2b8h+)은 角을 7九銀에게 그냥 준다.
	s := &stubMulti{res: usi.SearchResult{
		Best: "3c3d",
		Lines: []usi.SearchLine{
			line("3c3d", 400),   // 플레이어 −400, 밴드 밖
			line("2b8h+", -200), // 플레이어 +200, 밴드 한복판 — 하지만 角을 던진다
		},
	}}
	o := NewAdaptiveOpponent(s, 12, DefaultBand)
	got, err := o.Choose(t.Context(), shogi.StartSFEN, []string{"7g7f"})
	if err != nil {
		t.Fatalf("Choose: %v", err)
	}
	if got == "2b8h+" {
		t.Error("밴드에 맞는다고 角을 던졌다 — 그게 「던지는」 것이다")
	}

	// 개입이 タダ捨て라고 부르는 것과 **같은 정의**여야 한다. 갈리면 화면이 가르친 것을
	// 컴퓨터가 바로 어긴다.
	pos, m, err := replay(shogi.StartSFEN, []string{"7g7f", "2b8h+"})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !MoveFeatures(pos, m).HangsPiece() {
		t.Fatal("테스트 전제가 깨졌다 — △8八角成은 タダ捨て다")
	}
}

// 전부 걸러지면 최선수로 물러선다. 약화는 부가 기능이고 대국이 본체다.
func TestAdaptiveFallsBackToBest(t *testing.T) {
	if got := chooseFrom(t, "7g7f"); got != "7g7f" {
		t.Errorf("후보가 없으면 최선수다: %q", got)
	}

	// 판을 못 읽어도 마찬가지다
	s := &stubMulti{res: usi.SearchResult{Best: "7g7f", Lines: []usi.SearchLine{line("2g2f", -200)}}}
	o := NewAdaptiveOpponent(s, 12, DefaultBand)
	if got, err := o.Choose(t.Context(), "not a sfen", nil); err != nil || got != "7g7f" {
		t.Errorf("깨진 국면에서 %q, %v — 최선수로 물러서야 한다", got, err)
	}
}

func TestAdaptivePropagatesSearchFailure(t *testing.T) {
	o := NewAdaptiveOpponent(&stubMulti{err: errors.New("boom")}, 12, DefaultBand)
	if _, err := o.Choose(t.Context(), shogi.StartSFEN, nil); err == nil {
		t.Fatal("탐색 실패가 전달되지 않음")
	}
	// 수가 하나도 없으면 조용히 빈 문자열을 돌려주지 않는다 — 세션이 그걸 두려 한다
	o2 := NewAdaptiveOpponent(&stubMulti{res: usi.SearchResult{}}, 12, DefaultBand)
	if _, err := o2.Choose(t.Context(), shogi.StartSFEN, nil); err == nil {
		t.Fatal("빈 결과가 에러가 아니다")
	}
}

func TestBandDistance(t *testing.T) {
	b := Band{LoCp: 100, HiCp: 300}
	for _, tc := range []struct{ cp, want int }{
		{100, 0}, {200, 0}, {300, 0},
		{99, 1}, {0, 100}, {-500, 600},
		{301, 1}, {800, 500},
	} {
		if got := b.distance(tc.cp); got != tc.want {
			t.Errorf("distance(%d) = %d, want %d", tc.cp, got, tc.want)
		}
	}
}
