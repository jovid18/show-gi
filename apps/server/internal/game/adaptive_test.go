package game

import (
	"context"
	"errors"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/handicap"
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
// 자리에서 조절이 꺼진다.** 한 판이 298手가 되고 사람이 못 끝낸다(journal §55).
func TestAdaptiveKeepsConcedingWhenAlreadyLost(t *testing.T) {
	got := chooseFrom(t, "7g7f",
		line("7g7f", -1500), // 플레이어 +1500 — 상대의 최선수
		line("2g2f", -1700), // 목표 양보 구간(+1600~+1800) 안 ← 이걸 골라야 한다
		line("6g6f", -3000), // 더 많이 양보한다
	)
	if got != "2g2f" {
		t.Errorf("최소 양보를 골라야 한다: %q", got)
	}
}

// **잘 두는 사람에게는 그 자리에서도 버틴다.** 실력 추정이 바닥을 기준점 아래로 내리므로
// 최선수가 다시 후보가 된다 — 조절의 손잡이가 하나로 들어온다는 것이 이 테스트다.
func TestConcedingFollowsTheSkillEstimate(t *testing.T) {
	// 플레이어 관점 +1500(최선) · +1700 · +2000. 밴드가 낙폭에 따라 1300~1500 →
	// 1600~1800 → 1900~2100 으로
	// 오르므로 셋이 각각 다른 수를 고른다.
	candidates := []usi.SearchLine{
		line("7g7f", -1500), // 최선수
		line("2g2f", -1700),
		line("6g6f", -2000),
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

// 밴드 안의 후보가 없으면 가장 가까운 안전한 후보를 고른다. 둘 다 아래라면 더 양보한 쪽이다.
func TestPicksTheClosestSafeMoveBelowTheBand(t *testing.T) {
	got := chooseFrom(t, "7g7f",
		line("7g7f", -1500), // 플레이어 +1500 — 최선수
		line("2g2f", -1520), // 밴드(+1600~+1800) 아래지만 더 가깝다 ← 이것
	)
	if got != "2g2f" {
		t.Errorf("밴드 아래의 가까운 수를 골라야 한다: %q", got)
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

// **이기고 있을 때도 안전 상한 안에서는 양보한다.**
func TestAdaptiveEasesOffWhenWinning(t *testing.T) {
	got := chooseFrom(t, "7g7f",
		line("7g7f", 3000), // 플레이어 −3000. 최선수
		line("2g2f", 2500), // 500cp 양보. 안전한 후보 중 제일 너그럽다 ← 이것
		line("6g6f", 1500), // 1500cp 양보. 안전 상한 밖
	)
	if got != "2g2f" {
		t.Errorf("유리할 때도 안전 상한 안에서 양보해야 한다: %q", got)
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

// 밴드는 닫힌 구간이다. 안에 없으면 넘치는 수를 무조건 고르지 않고 가까운 안전한 수로 간다.
func TestClosestToBandDoesNotPreferAWildOvershoot(t *testing.T) {
	opts := []option{{move: "a", playerCp: 331}, {move: "b", playerCp: 814}}

	if got := closestToBand(opts, Band{LoCp: 400, HiCp: 600}); got != "a" {
		t.Errorf("밴드에 더 가까운 부족한 수를 골라야 한다: %q", got)
	}
	if got := closestToBand(opts, Band{LoCp: 100, HiCp: 900}); got != "a" {
		t.Errorf("둘 다 밴드 안이면 최소 양보다: %q", got)
	}
	if got := closestToBand(opts, Band{LoCp: 900, HiCp: 1100}); got != "b" {
		t.Errorf("둘 다 밴드 아래면 가까운 쪽이다: %q", got)
	}
}

func TestMaxConcessionIsIndependentOfDifficulty(t *testing.T) {
	// 대국 949의 5手 뒤 실제 국면과 depth 12 · k=10 결과다.
	// ▲2二角成에 △同銀만 馬를 회수한다. △3二銀은 움직인 銀 자체가 잡히지 않아
	// HangsPiece 를 통과하지만, 최선수보다 2891cp 나빠 난이도와 무관하게 탈락해야 한다.
	moves := []string{"7g7f", "7a7b", "2g2f", "3c3d", "8h2b+"}
	lines := []usi.SearchLine{
		line("3a2b", 68),
		line("3a3b", -2823),
		line("8c8d", -2932),
		line("9c9d", -2973),
		line("7c7d", -2990),
		line("8b9b", -2997),
		line("6c6d", -3002),
		line("4a3b", -3021),
		line("5a6b", -3048),
		line("2c2d", -3088),
	}

	for _, loss := range []float64{1, 0.75, 0.5, 0.25, 0} {
		sk := ready(loss)
		s := &stubMulti{res: usi.SearchResult{Best: "3a2b", Lines: lines}}
		o := NewAdaptiveOpponent(s, 12, DefaultBand)
		got, err := o.Choose(t.Context(), shogi.StartSFEN, moves, sk)
		if err != nil {
			t.Fatalf("강함 %d: %v", strengthStep(skillShift(sk)), err)
		}
		if got != "3a2b" {
			t.Errorf("강함 %d가 %q 선택, want 3a2b", strengthStep(skillShift(sk)), got)
		}
	}
}

// ─── 실력 추정이 밴드를 옮긴다 (journal §47) ───────────────────────────

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
	// 플레이어 관점으로 −200(최선) · +200(기본 밴드 안) · +400(가장 너그러운 밴드 안).
	// 최선수 대비 600cp 안에서 세 단계가 모두 갈린다.
	candidates := []usi.SearchLine{line("7g7f", 200), line("2g2f", -200), line("6g6f", -400)}

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

// TestBandFollowsTheHandicapOrigin 은 **핸디캡을 흘린 사람에게 상대가 되돌려 주는지**를 본다.
//
// 二枚落ち(+1490)에서 사람이 +500까지 흘린 자리다. 기준점을 안 옮기면 이 국면이 「구간 위」로
// 읽혀서(500 > 300) 상대가 「지금 형세에서 100~300 더」만 겨냥하고, 그 좌표에서는 상대의
// 최선수가 그대로 뽑힌다 — 조절이 가장 필요한 자리에서 꺼지는 것이다(Choose).
func TestBandFollowsTheHandicapOrigin(t *testing.T) {
	nimai, ok := handicap.Find("nimaiochi")
	if !ok {
		t.Fatal("nimaiochi 가 표에 없다")
	}
	// 下手가 한 수 뒀으므로 지금은 上手(상대) 차례다 — 사람은 下手(Black)로 읽힌다.
	moves := []string{"7g7f"}

	s := &stubMulti{res: usi.SearchResult{Best: "3c3d", Lines: []usi.SearchLine{
		line("3c3d", -500),  // 플레이어 +500. 上手의 최선
		line("8c8d", -900),  // 플레이어 +900
		line("4a3b", -1100), // 플레이어 +1100. 안전 상한(+600) 경계 ← 이걸 골라야 한다
		line("6a5b", -1400), // 플레이어 +1400. 상한 밖이라 걸러진다
	}}}
	o := NewAdaptiveOpponent(s, 12, DefaultBand)
	got, err := o.Choose(t.Context(), nimai.SFEN, moves, skill.Unknown)
	if err != nil {
		t.Fatalf("Choose: %v", err)
	}
	if got != "4a3b" {
		t.Errorf("기준점(+%d)을 향해 가장 많이 되돌리는 안전한 수를 골라야 한다: %q", nimai.BaselineCp, got)
	}

	// **같은 후보를 平手에서 주면 반대로 고른다.** 거기서는 +500이 이미 구간 위라
	// 「조금만 더」가 맞는 뜻이고, 그 차이가 곧 기준점이 하는 일이다.
	flat := &stubMulti{res: s.res}
	o = NewAdaptiveOpponent(flat, 12, DefaultBand)
	got, err = o.Choose(t.Context(), shogi.StartSFEN, moves, skill.Unknown)
	if err != nil {
		t.Fatalf("Choose(平手): %v", err)
	}
	if got == "4a3b" {
		t.Error("平手에서 기준점이 붙었다 — 手合割 판만 옮겨야 한다")
	}
}
