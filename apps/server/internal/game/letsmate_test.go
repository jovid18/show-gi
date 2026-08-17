package game

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// `lets_mate` 의 배선을 본다 — 「상대가 나를 詰ます」를 엔진에 어떻게 묻고, **언제 안 말하는가**.
//
// solver 없이 돈다. 여기서 확인할 것은 詰み 판정의 정확도가 아니라 **어느 국면을 묻고 그 답을
// 어떻게 쓰는가**라서, 진짜 엔진을 쓰면 오히려 그 구분이 안 보인다(journal §40).

// perLineMate 는 **수순마다 다른 답**을 주는 solver다.
//
// `scriptedMate` 로는 이걸 못 본다 — 그쪽은 어느 국면에나 같은 手数를 주므로 「둔 수 뒤에는
// 詰み, 최선수 뒤에는 없음」을 표현할 수 없고, 그 구분이 정확히 이 카테고리의 조건이다.
type perLineMate struct {
	// plies 는 수순(공백으로 이은 USI)마다의 詰み 手数다. 없는 수순은 詰み 없음.
	plies map[string]int
	err   error
	asked [][]string
}

func (m *perLineMate) SearchMate(_ context.Context, _ string, moves []string) (usi.MateResult, error) {
	m.asked = append(m.asked, slices.Clone(moves))
	if m.err != nil {
		return usi.MateResult{}, m.err
	}
	n := m.plies[strings.Join(moves, " ")]
	if n == 0 {
		return usi.MateResult{Proven: true}, nil
	}
	// 내용은 안 본다 — 길이가 手数다. 합법이 아니어도 되는 것은 이 테스트가 수순을 판에
	// 놓지 않기 때문이고, 놓는 쪽(refutationLine)은 아래 별도 테스트가 본다.
	return usi.MateResult{Moves: make([]string, n), Proven: true}, nil
}

// mateSearcher 는 착수 후 국면이 「수번 측이 詰ます」로 나오는 탐색 결과다.
//
// 그것이 `opponentMate` 의 게이트다 — 이 값이 없으면 solver를 아예 안 부른다.
func mateSearcher(mateIn int) usi.SearchResult {
	return usi.SearchResult{Best: "7g7f", ScoreCp: -3000, IsMate: true, MateIn: mateIn}
}

func TestOpponentMateNeedsBothConditions(t *testing.T) {
	const played, best = "3c3d", "7g7f"
	// 사람이 한 수 두었다고 본다. 착수 전 수순은 비어 있다.
	moves := []string{played}

	cases := []struct {
		name  string
		gate  int            // 탐색이 본 mate. 0이면 게이트가 안 열린다
		mates map[string]int // solver 가 詰み을 주는 수순
		want  int
		why   string
	}{
		{
			name:  "둔 수 뒤에만 詰み — 말해도 된다",
			gate:  3,
			mates: map[string]int{played: 3},
			want:  3,
		},
		{
			name:  "최선수 뒤에도 詰み — 이미 진 국면이라 안 말한다",
			gate:  3,
			mates: map[string]int{played: 3, best: 5},
			want:  0,
			why:   "이미 詰んでいた 국면에 「この手で詰まされます」를 내보내면 그 수의 죄가 아닌 것을 죄라고 가르친다",
		},
		{
			name:  "탐색은 詰み을 봤는데 solver 는 증명 못 함 — 안 말한다",
			gate:  13,
			mates: nil,
			want:  0,
			why:   "증명 없이는 手数를 말할 수 없다. 실측에 13手 한 건이 이랬다",
		},
		{
			name:  "탐색이 詰み을 안 봤다 — solver 를 아예 안 부른다",
			gate:  0,
			mates: map[string]int{played: 3},
			want:  0,
			why:   "게이트가 공짜 신호라서 평범한 수에 비용이 0인 것이 이 설계의 요점이다",
		},
		{
			name:  "내 詰み이 남은 것(MateIn<0)은 반대 방향이다",
			gate:  -3,
			mates: map[string]int{played: 3},
			want:  0,
			why:   "부호가 반대면 「상대가 詰まされる」이고 그건 missed_mate 쪽 신호다",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mate := &perLineMate{plies: tc.mates}
			a := &engineAnalyst{mate: mate}
			got := a.opponentMate(t.Context(), shogi.StartSFEN, moves, nil, best, mateSearcher(tc.gate))
			if len(got) != tc.want {
				t.Errorf("手数 = %d, want %d\n  %s", len(got), tc.want, tc.why)
			}
			if tc.gate <= 0 && len(mate.asked) > 0 {
				t.Errorf("게이트가 닫혔는데 solver 를 %d번 불렀다 — 평범한 수에 비용이 붙는다", len(mate.asked))
			}
		})
	}
}

// 어느 국면을 묻는지가 이 카테고리의 전부다. 한 수 어긋난 국면을 물어도 手数는 그럴듯하게
// 나오므로, 무엇을 물었는지를 직접 본다.
func TestOpponentMateAsksTheRightPositions(t *testing.T) {
	before := []string{"7g7f", "3c3d"}
	moves := append(slices.Clone(before), "8h2b")

	mate := &perLineMate{plies: map[string]int{strings.Join(moves, " "): 5}}
	a := &engineAnalyst{mate: mate}
	got := a.opponentMate(t.Context(), shogi.StartSFEN, moves, before, "2g2f", mateSearcher(5))

	if len(got) != 5 {
		t.Fatalf("手数 = %d, want 5", len(got))
	}
	want := [][]string{
		{"7g7f", "3c3d", "8h2b"}, // ① 둔 수 뒤
		{"7g7f", "3c3d", "2g2f"}, // ② 최선수 뒤 — 착수 전 수순 + 최선수다
	}
	if !slices.EqualFunc(mate.asked, want, slices.Equal) {
		t.Errorf("물어본 수순 = %v\n  want %v", mate.asked, want)
	}
}

func TestOpponentMateStaysQuietWhenSolverFails(t *testing.T) {
	a := &engineAnalyst{mate: &perLineMate{err: errors.New("boom")}}
	if got := a.opponentMate(t.Context(), shogi.StartSFEN, []string{"3c3d"}, nil, "7g7f", mateSearcher(3)); got != nil {
		t.Error("solver 가 실패했는데 手数가 나왔다 — 모를 때는 말하지 않는다")
	}

	// solver 가 없는 배포에서도 대국은 돌아야 한다.
	none := &engineAnalyst{mate: nil}
	if got := none.opponentMate(t.Context(), shogi.StartSFEN, []string{"3c3d"}, nil, "7g7f", mateSearcher(3)); got != nil {
		t.Error("solver 가 없는데 手数가 나왔다")
	}
}

// 詰み 수순은 **자르지 않는다.** 자르면 「合の応手가 있는 것 아닌가」로 읽힌다.
func TestMateRefutationIsNotTrimmed(t *testing.T) {
	// 一手詰め. ▲1一飛成 뒤 후수 玉에 詰み이 걸린 국면을 쓰는 대신, 조용한 수가 이어지는
	// 수순으로 「안 자른다」만 본다 — trimRefutation 은 조용한 수에서 1로 자른다.
	// 「1g1f」 뒤는 후수 차례다 — 수순이 그쪽부터 번갈아야 룰 엔진이 안 끊는다.
	pv := []string{"3c3d", "2g2f", "8c8d", "6g6f"}

	trimmed := refutationLine(shogi.StartSFEN, []string{"1g1f"}, pv, RefutationPlies, false).line
	if len(trimmed) != 1 {
		t.Fatalf("자르는 쪽 = %d手, want 1 — 전제가 깨졌다", len(trimmed))
	}

	full := refutationLine(shogi.StartSFEN, []string{"1g1f"}, pv, RefutationPlies, true).line
	if len(full) != len(pv) {
		t.Errorf("안 자르는 쪽 = %d手, want %d", len(full), len(pv))
	}
}

// RefutationPlies(8)가 11手 詰み을 자르면 안 된다. 그 상한은 PV용이고, 詰み의 상한은
// solver 의 DepthLimit(11)이다.
func TestMateRefutationIgnoresThePlyCap(t *testing.T) {
	// 초기 국면에서 서로 조용히 둘 수 있는 11手.
	pv := []string{
		"3c3d", "2g2f", "8c8d", "6g6f", "7c7d",
		"5g5f", "6c6d", "4g4f", "5c5d", "3g3f", "4c4d",
	}
	if len(pv) <= RefutationPlies {
		t.Fatal("전제가 깨졌다 — 상한보다 긴 수순이어야 한다")
	}
	full := refutationLine(shogi.StartSFEN, []string{"1g1f"}, pv, RefutationPlies, true).line
	if len(full) != len(pv) {
		t.Errorf("%d手 짜리가 %d手로 잘렸다 — RefutationPlies 가 詰み에 걸리고 있다", len(pv), len(full))
	}
}

// 순서가 규칙의 일부다. 玉이 죽는 국면에서 駒 이야기를 하면 초심자는 駒를 지키고 다음 수에 詰む.
func TestLetsMateOutranksMaterialCategories(t *testing.T) {
	base := intervene.Features{
		Known:             true,
		OpponentMatePlies: 3,
	}

	hangs := base
	hangs.LandsAttacked, hangs.LandsDefended, hangs.MovedValue = true, false, 8
	if !hangs.HangsPiece() {
		t.Fatal("전제가 깨졌다 — タダ捨て 조건이 성립해야 한다")
	}

	greedy := base
	greedy.CapturedValue, greedy.LandsAttacked = 6, true

	check := base
	check.GivesCheck = true

	for name, f := range map[string]intervene.Features{
		"タダ捨て보다 앞": hangs,
		"駒得보다 앞":   greedy,
		"王手보다 앞":   check,
	} {
		v := intervene.Judge(intervene.Input{BestCp: 30000, AfterCp: -30000, Features: f})
		if v.Category != intervene.CategoryLetsMate {
			t.Errorf("%s: 카테고리 = %s, want lets_mate", name, v.Category)
		}
	}

	// **`unpromoted` 만 앞이다.** 이동이 최선수와 같고 成만 안 한 수라면 고칠 것은 成이고,
	// 그것이 詰み까지 막는다.
	unp := base
	unp.UnpromotedOnly = true
	v := intervene.Judge(intervene.Input{BestCp: 30000, AfterCp: -30000, Features: unp})
	if v.Category != intervene.CategoryUnpromoted {
		t.Errorf("不成: 카테고리 = %s, want unpromoted", v.Category)
	}
}
