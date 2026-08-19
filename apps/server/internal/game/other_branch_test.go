package game

import (
	"context"
	"errors"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// branchStub 은 두 API를 다 만족한다. analyst 가 들고 있는 것은 Searcher 이고
// 갈래 수집은 거기서 MultiSearcher 로 타입 단언을 하므로, 한쪽만 있으면 그 자리가 조용히
// 비어 버린다 — 그 단언 자체도 여기서 확인된다.
type branchStub struct {
	res usi.SearchResult
	err error
	// asked 는 마지막으로 받은 수순. 갈래를 A+B 국면에서 재는지가 여기서 보인다.
	asked []string
	k     int
}

func (s *branchStub) SearchDepth(context.Context, string, []string, int) (usi.SearchResult, error) {
	return usi.SearchResult{}, errors.New("갈래 수집은 이쪽을 안 쓴다")
}

func (s *branchStub) SearchMultiPV(_ context.Context, _ string, moves []string, _, multiPV int) (usi.SearchResult, error) {
	s.asked, s.k = moves, multiPV
	return s.res, s.err
}

// thrownBishopMoves 는 §50의 그 수순이다 — ▲7六歩 △3四歩 ▲3三角成.
var thrownBishopMoves = []string{"7g7f", "3c3d", "8h3c+"}

func pvLine(rank int, cp int, pv ...string) usi.SearchLine {
	return usi.SearchLine{Depth: JudgeDepth, MultiPV: rank, Move: pv[0], ScoreCp: cp, PV: pv}
}

func collect(t *testing.T, res usi.SearchResult) (*branchStub, string, []explain.Branch) {
	t.Helper()
	s := &branchStub{res: res}
	a := &engineAnalyst{search: s, depth: JudgeDepth, level: intervene.Beginner}
	best, branches := a.otherBranches(t.Context(), shogi.StartSFEN, thrownBishopMoves, []string{"2b3c"})
	return s, best, branches
}

// 갈래 하나는 내 수·상대의 응수·그 결말 셋이 다 있을 때만 선다.
func TestOtherBranchesCarriesTheWholeFork(t *testing.T) {
	stub, best, got := collect(t, usi.SearchResult{Lines: []usi.SearchLine{
		pvLine(1, -350, "6g6f", "8b5b"),
		pvLine(2, -600, "2g2f", "3c8h+"),
		pvLine(3, -800, "5g5f", "5a4b"),
	}})

	if best != "△同角" {
		t.Errorf("상대 최선수 = %q, want △同角", best)
	}
	// A+B 국면에서 잰다. 뿌리가 한 수라도 어긋나면 갈래가 다른 국면의 것이 된다.
	if len(stub.asked) != 4 || stub.asked[3] != "2b3c" {
		t.Errorf("탐색한 수순 = %v", stub.asked)
	}
	if stub.k != OtherBranches {
		t.Errorf("MultiPV = %d, want %d", stub.k, OtherBranches)
	}

	want := []explain.Branch{
		{PlayerJa: "▲6六歩", ReplyJa: "△5二飛", Cp: -350},
		{PlayerJa: "▲2六歩", ReplyJa: "△8八角成", Cp: -600},
		{PlayerJa: "▲5六歩", ReplyJa: "△4二玉", Cp: -800},
	}
	if len(got) != len(want) {
		t.Fatalf("갈래 %d개: %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %+v, want %+v", i, got[i], want[i])
		}
	}
}

// 엔진 출력을 믿지 않는다. 못 두는 수가 섞이거나 응수가 없으면 그 줄만 버린다 —
// 반쪽짜리 갈래는 문장에서 곧 거짓이 된다.
func TestOtherBranchesDropsWhatItCannotVerify(t *testing.T) {
	_, best, got := collect(t, usi.SearchResult{Lines: []usi.SearchLine{
		pvLine(1, -350, "6g6f"),         // 응수가 없다
		pvLine(2, -400, "9i8h", "8b5b"), // 香는 옆으로 못 간다
		pvLine(3, -600, "2g2f", "9a9h"), // 상대의 응수가 못 두는 수다
		pvLine(4, -800, "5g5f", "5a4b"), // 이것만 성립한다
	}})

	if best != "△同角" {
		t.Errorf("상대 최선수 = %q", best)
	}
	if len(got) != 1 || got[0].PlayerJa != "▲5六歩" {
		t.Fatalf("성립하지 않는 갈래가 남았다: %+v", got)
	}
}

// 詰み은 cp로 말하지 않는다. 30000은 평가치가 아니라 환산값이다(explain.BranchScoreJa).
func TestOtherBranchesKeepsMateOutOfCp(t *testing.T) {
	_, _, got := collect(t, usi.SearchResult{Lines: []usi.SearchLine{
		{Depth: JudgeDepth, MultiPV: 1, Move: "5g5f", ScoreCp: -usi.MateCp + 5, IsMate: true, MateIn: -5, PV: []string{"5g5f", "5a4b"}},
	}})

	if len(got) != 1 {
		t.Fatalf("갈래 %d개", len(got))
	}
	if got[0].Cp != 0 || got[0].MateIn != -5 {
		t.Errorf("got %+v, want cp=0 mateIn=-5", got[0])
	}
}

// 탐색이 실패해도 상대의 최선수까지는 말할 수 있다 — 그것은 판정이 이미 손에 든 값이다.
func TestOtherBranchesStillNamesTheReplyWhenTheSearchFails(t *testing.T) {
	s := &branchStub{err: errors.New("engine")}
	a := &engineAnalyst{search: s, depth: JudgeDepth, level: intervene.Beginner}

	best, got := a.otherBranches(t.Context(), shogi.StartSFEN, thrownBishopMoves, []string{"2b3c"})
	if best != "△同角" || len(got) != 0 {
		t.Errorf("best=%q branches=%+v", best, got)
	}
}

// 상대의 최선수 자체가 못 두는 수면 아무것도 안 준다. 그 위에 세운 갈래는 전부 거짓이다.
func TestOtherBranchesRefusesAnIllegalReply(t *testing.T) {
	s := &branchStub{}
	a := &engineAnalyst{search: s, depth: JudgeDepth, level: intervene.Beginner}

	best, got := a.otherBranches(t.Context(), shogi.StartSFEN, thrownBishopMoves, []string{"9a9h"})
	if best != "" || len(got) != 0 {
		t.Errorf("best=%q branches=%+v", best, got)
	}
	if s.asked != nil {
		t.Error("검증도 안 된 수 뒤에서 엔진을 불렀다")
	}
}
