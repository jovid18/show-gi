package game

import (
	"context"
	"errors"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/skill"
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
	got, err := o.Choose(t.Context(), shogi.StartSFEN, nil, skill.Unknown)
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

// **이미 지고 있어도 최선수로 버티지 않는다 — 한 칸씩 양보한다.**
//
// 후보가 전부 밴드 위(플레이어가 이미 크게 유리)라 절대 좌표가 뜻을 잃는 자리다. 여기서
// 거리를 최소화하면 「+300으로 되돌려라」가 되어 최선수가 뽑히고, **조절이 가장 필요한
// 자리에서 조절이 꺼진다.** 한 판이 298手가 되고 사람이 못 끝낸다(06-status.md §55).
func TestAdaptiveKeepsConcedingWhenAlreadyLost(t *testing.T) {
	got := chooseFrom(t, "7g7f",
		line("7g7f", -1500), // 플레이어 +1500 — 상대의 최선수
		line("2g2f", -2200), // 최소 양보 ← 이걸 골라야 한다
		line("6g6f", -3000), // 더 많이 양보한다
	)
	if got != "2g2f" {
		t.Errorf("최소 양보를 골라야 한다: %q", got)
	}
}

// **잘 두는 사람에게는 그 자리에서도 버틴다.** 실력 추정이 바닥을 기준점 아래로 내리므로
// 최선수가 다시 후보가 된다 — 조절의 손잡이가 하나로 들어온다는 것이 이 테스트다.
func TestConcedingFollowsTheSkillEstimate(t *testing.T) {
	// 플레이어 관점 +1500(최선) · +1700 · +2200. 바닥이 낙폭에 따라 1300 → 1600 → 1900 으로
	// 오르므로 셋이 각각 다른 수를 고른다.
	candidates := []usi.SearchLine{
		line("7g7f", -1500), // 최선수
		line("2g2f", -1700),
		line("6g6f", -2200),
	}
	for _, tc := range []struct {
		name string
		sk   skill.Estimate
		want string
	}{
		{"매 수 최선이면 버틴다", ready(0), "7g7f"},
		{"기준선에서는 최소 양보", skill.Unknown, "2g2f"},
		{"매 수 블런더면 더 양보한다", ready(1), "6g6f"},
	} {
		if got := chooseWith(t, tc.sk, "7g7f", candidates...); got != tc.want {
			t.Errorf("%s: %q 기대, got %q (shift=%dcp)", tc.name, tc.want, got, skillShift(tc.sk))
		}
	}
}

// 바닥을 넘는 후보가 하나도 없으면 **가장 많이 양보하는 것**을 고른다. 여기서 최선수로
// 물러서면 후보 간격이 밴드 폭보다 넓은 국면마다 조절이 조용히 꺼진다.
func TestConcedesEvenWhenNoCandidateClearsTheFloor(t *testing.T) {
	got := chooseFrom(t, "7g7f",
		line("7g7f", -1500), // 플레이어 +1500 — 최선수
		line("2g2f", -1520), // 바닥(+1600)에 못 미치지만 그나마 양보다 ← 이것
	)
	if got != "2g2f" {
		t.Errorf("바닥을 못 넘어도 양보해야 한다: %q", got)
	}
}

// 詰み 줄은 밴드의 자가 아니다. `ScoreCp` 가 환산값이라 기준점을 판 밖으로 끌고 간다.
func TestBandIgnoresMateLines(t *testing.T) {
	s := &stubMulti{res: usi.SearchResult{
		Best: "7g7f",
		Lines: []usi.SearchLine{
			line("7g7f", 200),  // 플레이어 −200
			line("2g2f", -200), // 플레이어 +200 — 기본 밴드 안 ← 이것
			{Depth: 12, MultiPV: 3, Move: "6g6f", ScoreCp: -usi.MateCp, IsMate: true, MateIn: -3},
		},
	}}
	o := NewAdaptiveOpponent(s, 12, DefaultBand)
	got, err := o.Choose(t.Context(), shogi.StartSFEN, nil, skill.Unknown)
	if err != nil {
		t.Fatalf("Choose: %v", err)
	}
	if got != "2g2f" {
		t.Errorf("詰み 줄이 기준점을 흔들었다: %q", got)
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
	got, err := o.Choose(t.Context(), shogi.StartSFEN, []string{"7g7f"}, skill.Unknown)
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
	if got, err := o.Choose(t.Context(), "not a sfen", nil, skill.Unknown); err != nil || got != "7g7f" {
		t.Errorf("깨진 국면에서 %q, %v — 최선수로 물러서야 한다", got, err)
	}
}

func TestAdaptivePropagatesSearchFailure(t *testing.T) {
	o := NewAdaptiveOpponent(&stubMulti{err: errors.New("boom")}, 12, DefaultBand)
	if _, err := o.Choose(t.Context(), shogi.StartSFEN, nil, skill.Unknown); err == nil {
		t.Fatal("탐색 실패가 전달되지 않음")
	}
	// 수가 하나도 없으면 조용히 빈 문자열을 돌려주지 않는다 — 세션이 그걸 두려 한다
	o2 := NewAdaptiveOpponent(&stubMulti{res: usi.SearchResult{}}, 12, DefaultBand)
	if _, err := o2.Choose(t.Context(), shogi.StartSFEN, nil, skill.Unknown); err == nil {
		t.Fatal("빈 결과가 에러가 아니다")
	}
}

// 넘치는 쪽이 부족한 쪽보다 늘 앞이다. 이 순서가 곧 「도움은 넘쳐서 틀리는 편이 낫다」다.
func TestConcedePrefersOvershootToNoConcession(t *testing.T) {
	opts := []option{{move: "a", playerCp: 331}, {move: "b", playerCp: 814}}

	// 바닥이 331과 814 사이 — 「양보 0」이 더 가깝지만 고르지 않는다.
	if got := concede(opts, 400); got != "b" {
		t.Errorf("바닥을 넘는 최소 양보를 골라야 한다: %q", got)
	}
	// 바닥이 둘 다 아래면 최소 양보다.
	if got := concede(opts, 100); got != "a" {
		t.Errorf("둘 다 바닥을 넘으면 낮은 쪽이다: %q", got)
	}
	// 아무것도 못 넘으면 가장 많이 양보한다.
	if got := concede(opts, 2000); got != "b" {
		t.Errorf("바닥을 못 넘으면 가장 너그러운 수다: %q", got)
	}
}

// ─── 실력 추정이 밴드를 옮긴다 (06-status.md §47) ───────────────────────────

// ready 는 표본이 충분한 추정치를 만든다. 값만 밖에서 정한다 —
// 어떻게 그 값에 이르는지는 skill 쪽 테스트가 본다.
func ready(loss float64) skill.Estimate {
	return skill.Estimate{Loss: loss, Samples: skill.MinSamples}
}

func chooseWith(t *testing.T, sk skill.Estimate, best string, lines ...usi.SearchLine) string {
	t.Helper()
	s := &stubMulti{res: usi.SearchResult{Best: best, Lines: lines}}
	o := NewAdaptiveOpponent(s, 12, DefaultBand)
	got, err := o.Choose(t.Context(), shogi.StartSFEN, nil, sk)
	if err != nil {
		t.Fatalf("Choose: %v", err)
	}
	return got
}

// **같은 후보에서 다른 수가 나온다.** 헤매는 사람에게는 더 양보하고, 잘 두는 사람에게는
// 이기려 든다 — 이 두 줄이 「적응형이 적응하는 대상이 사람이 아니었다」를 닫는다(§21 ①).
func TestBandFollowsHowMuchThePlayerIsStruggling(t *testing.T) {
	// 플레이어 관점으로 −200(최선) · +200(기본 밴드 안) · +600(크게 양보).
	candidates := []usi.SearchLine{line("7g7f", 200), line("2g2f", -200), line("6g6f", -600)}

	for _, tc := range []struct {
		name string
		sk   skill.Estimate
		want string
	}{
		{"모르는 채로는 기준선", skill.Unknown, "2g2f"},
		{"매 수 블런더면 가장 너그러운 수", ready(1), "6g6f"},
		{"매 수 최선이면 이기려 든다", ready(0), "7g7f"},
	} {
		if got := chooseWith(t, tc.sk, "7g7f", candidates...); got != tc.want {
			t.Errorf("%s: %q 기대, got %q (shift=%dcp)", tc.name, tc.want, got, skillShift(tc.sk))
		}
	}
}

// 표본이 모자라면 안 움직인다. 첫 수 몇 개로 강함이 흔들리면 사람이 알아차리기 전에
// 상대가 딴사람이 된다.
func TestBandHoldsUntilEnoughMoves(t *testing.T) {
	early := skill.Estimate{Loss: 1, Samples: skill.MinSamples - 1}
	if got := skillShift(early); got != 0 {
		t.Errorf("표본 %d개로 밴드를 %dcp 옮겼다", early.Samples, got)
	}
}

// **양보는 밴드까지다.** 아무리 헤매도 駒를 그냥 주는 수는 안 고른다 — 화면이
// 「取り返せない場所」라고 가르친 수를 상대가 두면 방금 배운 것이 무너진다(§16).
func TestEasingOffNeverThrowsAPiece(t *testing.T) {
	// ▲7六歩 뒤 後手 차례. △8八角成은 角을 그냥 준다 — 밴드가 어디로 가든 후보가 아니다.
	s := &stubMulti{res: usi.SearchResult{
		Best: "3c3d",
		Lines: []usi.SearchLine{
			line("3c3d", 400),   // 플레이어 −400
			line("2b8h+", -600), // 플레이어 +600 = 옮긴 밴드 한복판. 하지만 角을 던진다
		},
	}}
	o := NewAdaptiveOpponent(s, 12, DefaultBand)
	got, err := o.Choose(t.Context(), shogi.StartSFEN, []string{"7g7f"}, ready(1))
	if err != nil {
		t.Fatalf("Choose: %v", err)
	}
	if got == "2b8h+" {
		t.Error("헤매는 사람에게 角을 던져 줬다 — 그건 봐주는 것이 아니라 던지는 것이다")
	}
}

// 화면이 말하는 강함과 상대가 겨냥하는 강함이 **같은 숫자에서 나온다**(§31의 실패 방지).
func TestStrengthStepTracksTheShift(t *testing.T) {
	for _, tc := range []struct {
		loss float64
		want int
	}{
		{1, 1},   // 매 수 블런더 — 가장 너그럽다
		{0.5, 3}, // 기준선
		{0, 5},   // 매 수 최선 — 최선수 쪽
	} {
		if got := strengthStep(skillShift(ready(tc.loss))); got != tc.want {
			t.Errorf("낙폭 %.1f: 단계 %d 기대, got %d", tc.loss, tc.want, got)
		}
	}
	// 추정기가 꺼져 있거나 표본이 모자랄 때도 눈금은 한복판이다
	if got := strengthStep(skillShift(skill.Unknown)); got != 3 {
		t.Errorf("모르는 상태의 단계는 3이어야 한다: %d", got)
	}
}
