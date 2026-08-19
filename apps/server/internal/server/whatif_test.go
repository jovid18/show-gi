package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/archive"
	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// whatifNodeOf 는 DB도 세션도 안 탄다. 엔진과 캐시를 손으로 만들어 넣으면 분기의 정합성을
// 전부 확인할 수 있다 — **여기가 리뷰에서 처음으로 엔진에 매인 자리**라 그 경계를
// 테스트에서도 지킨다(intervene 이 엔진을 모르는 것과 같은 구조다).

// fakeSearcher 는 정해진 답만 준다. 부른 순서대로 하나씩 나간다.
//
// **mutex가 장식이 아니다.** ws 쪽은 탐색을 goroutine에서 돌리고 답이 소켓을 지나 오므로,
// -race 가 그 왕복으로는 순서를 볼 수 없다 — 여기서 잠그지 않으면 테스트가 자기 자신을
// 경합으로 신고한다.
type fakeSearcher struct {
	mu      sync.Mutex
	results []usi.SearchResult
	err     error

	calls []searchCall
}

type searchCall struct {
	moves   []string
	multiPV int
	// depth 는 **캐시가 한 무리인지**를 보는 값이다. 표면마다 다른 깊이로 물으면
	// `positions` 가 서로 못 쓰는 무리로 갈린다(02-architecture.md §4).
	depth int
}

func (f *fakeSearcher) SearchMultiPV(_ context.Context, _ string, moves []string, depth, multiPV int) (usi.SearchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, searchCall{moves: slices.Clone(moves), multiPV: multiPV, depth: depth})
	if f.err != nil {
		return usi.SearchResult{}, f.err
	}
	if len(f.calls) > len(f.results) {
		return usi.SearchResult{}, errors.New("fake: no more results")
	}
	return f.results[len(f.calls)-1], nil
}

// searches 는 지금까지 불린 탐색들이다.
func (f *fakeSearcher) searches() []searchCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// found 는 후보 목록이 붙은 탐색 결과 하나다. 점수는 40씩 내려간다 — 최선수 대비 낙폭이
// 몇인지가 한눈에 보여야 그 값을 테스트가 확인할 수 있다.
func found(moves ...string) usi.SearchResult {
	res := usi.SearchResult{Depth: whatifDepth}
	for i, m := range moves {
		cp := 100 - i*40
		res.Lines = append(res.Lines, usi.SearchLine{Depth: whatifDepth, MultiPV: i + 1, Move: m, ScoreCp: cp})
		if i == 0 {
			res.Best, res.ScoreCp = m, cp
		}
	}
	return res
}

// fakeCache 는 이미 잰 국면들이다. 키는 `archive.Key` 로 만든다 — 읽는 쪽과 쓰는 쪽이
// 키를 각자 만들면 히트율이 조용히 0이 된다.
type fakeCache struct{ rows map[string]store.Position }

func (c *fakeCache) GetPosition(_ context.Context, key string) (store.Position, error) {
	p, ok := c.rows[key]
	if !ok {
		return store.Position{}, store.ErrNoPosition
	}
	return p, nil
}

// **응수를 서버가 대신 두지 않는다.** 상대 차례면 상대의 합법수를 내주고 최선수는
// 화살표로만 알려준다 — 「상대라면 이렇게 둔다」를 직접 둬 보는 것이 이 표면의 내용이다.
func TestWhatIfDoesNotMoveForYou(t *testing.T) {
	rec := recordOf("b", "7g7f", "3c3d", "6g6f")
	search := &fakeSearcher{results: []usi.SearchResult{found("8c8d", "3c3d")}}

	node, err := whatifNodeOf(t.Context(), rootOf(rec), whatifRequest{Ply: 1}, search, nil)
	if err != nil {
		t.Fatalf("whatifNodeOf: %v", err)
	}

	if len(node.Line) != 0 {
		t.Fatalf("line = %+v, want 빈 줄 — 서버는 한 수도 안 둔다", node.Line)
	}
	// 1手目 뒤는 後手 차례다. 사람(先手)이 아니어도 그 쪽 수를 둘 수 있어야 한다.
	if node.Turn != "w" || node.YourTurn {
		t.Errorf("turn=%q yourTurn=%v, want w false", node.Turn, node.YourTurn)
	}
	if !slices.Contains(node.LegalMoves, "8c8d") {
		t.Errorf("legalMoves 에 상대의 수가 없다 — 상대 차례라고 못 두게 하지 않는다")
	}
	// 첫 후보가 곧 화면의 초록 화살표다.
	if len(node.Candidates) != 2 || node.Candidates[0].USI != "8c8d" {
		t.Fatalf("candidates = %+v", node.Candidates)
	}
	if node.Candidates[0].Ja != "△8四歩" {
		t.Errorf("candidates[0].ja = %q, want △8四歩", node.Candidates[0].Ja)
	}
	// 낙폭은 최선수 대비다. 화면이 뺄셈을 하지 않는다.
	if node.Candidates[0].LossCp != 0 || node.Candidates[1].LossCp != 40 {
		t.Errorf("lossCp = %d / %d, want 0 40", node.Candidates[0].LossCp, node.Candidates[1].LossCp)
	}

	// **탐색은 한 번이다.** 응수를 대신 두던 때는 두 번이었다.
	calls := search.searches()
	if len(calls) != 1 {
		t.Fatalf("searches = %d, want 1", len(calls))
	}
	// 엔진에는 **뿌리까지의 실제 수순이 그대로 앞에 붙어** 간다. 국면만 넘기면 千日手를
	// 세는 근거가 사라진다.
	if !slices.Equal(calls[0].moves, []string{"7g7f"}) || calls[0].multiPV != whatifCandidates {
		t.Errorf("탐색 = %+v", calls[0])
	}
}

// 값은 **플레이어 관점**이다. 상대 차례의 국면을 상대 관점으로 보내면 한 줄을 넘겨 보는
// 동안 부호가 뒤집혀서 「좋아지고 있나」를 읽을 수 없다.
func TestWhatIfKeepsThePlayerPointOfView(t *testing.T) {
	rec := recordOf("b", "7g7f")
	// 後手 차례의 국면이고, 엔진은 後手에게 +100이라고 답했다 — 사람에게는 -100이다.
	search := &fakeSearcher{results: []usi.SearchResult{found("8c8d")}}

	node, err := whatifNodeOf(t.Context(), rootOf(rec), whatifRequest{Ply: 1}, search, nil)
	if err != nil {
		t.Fatalf("whatifNodeOf: %v", err)
	}
	if node.EvalCp == nil || *node.EvalCp != -100 {
		t.Fatalf("evalCp = %v, want -100", node.EvalCp)
	}
	// **후보의 cp는 뒤집지 않는다.** 그 값의 주인은 그 수를 두는 쪽이고, 그게 `Turn` 이다.
	if node.Candidates[0].EvalCp != 100 {
		t.Errorf("candidates[0].evalCp = %d, want 100 (수번 관점)", node.Candidates[0].EvalCp)
	}
}

// 사람이 後手인 판. **누구의 수인지가 뒤집히면** 가정 수순이 남의 수를 내 것으로 그린다.
func TestWhatIfAttributesMovesByColor(t *testing.T) {
	rec := recordOf("w", "7g7f")
	search := &fakeSearcher{results: []usi.SearchResult{found("2g2f")}}

	node, err := whatifNodeOf(t.Context(), rootOf(rec), whatifRequest{Ply: 1, Moves: []string{"3c3d"}}, search, nil)
	if err != nil {
		t.Fatalf("whatifNodeOf: %v", err)
	}
	// 1手目는 상대(先手)의 수이고 2手目가 사람의 수다.
	if node.Line[0].By != game.SideHuman {
		t.Errorf("line[0].by = %q, want human", node.Line[0].By)
	}
	if node.Turn != "b" || node.YourTurn {
		t.Errorf("turn=%q yourTurn=%v, want b false — 사람은 後手다", node.Turn, node.YourTurn)
	}
}

// 사람의 수를 받으면 그 수를 두고 **거기서 멈춘다.**
func TestWhatIfAppliesTheMoveAndStops(t *testing.T) {
	rec := recordOf("b", "7g7f", "3c3d")
	search := &fakeSearcher{results: []usi.SearchResult{found("3a2b")}}

	node, err := whatifNodeOf(t.Context(), rootOf(rec), whatifRequest{Ply: 2, Moves: []string{"8h2b+"}}, search, nil)
	if err != nil {
		t.Fatalf("whatifNodeOf: %v", err)
	}

	if len(node.Line) != 1 {
		t.Fatalf("line = %+v, want 사람의 수 하나", node.Line)
	}
	if node.Line[0].By != game.SideHuman || node.Line[0].Ja != "▲2二角成" {
		t.Errorf("line[0] = %+v, want ▲2二角成 by human", node.Line[0])
	}
	if node.Line[0].SFEN == "" {
		t.Error("line[0].sfen is empty — 화면이 판을 못 그린다")
	}
	if node.BasePly != 2 || node.Ply != 3 {
		t.Errorf("basePly=%d ply=%d, want 2 3", node.BasePly, node.Ply)
	}
	// 되잡는 수가 화살표로 선다 — 「그래서 상대가 어떻게 하나」가 그 한 줄이다.
	if len(node.Candidates) != 1 || node.Candidates[0].Ja != "△同銀" {
		t.Errorf("candidates = %+v, want △同銀", node.Candidates)
	}
}

// 물러진 수는 **기보에 없다.** 그 수를 그 국면에서 둬 볼 수 있는 것이 이 표면의 이유다
// (journal §25 — 가르치는 것은 최선 수순이 아니라 「두려던 수의 변화」다).
func TestWhatIfPlaysTheRetractedMove(t *testing.T) {
	rec := recordOf("b", "7g7f", "3c3d", "6g6f")
	rec.Interventions = []store.RecordedIntervention{{
		Ply: 3, Kind: "blunder", Category: "hangs_piece", RetractedUSI: "8h2b+",
	}}
	search := &fakeSearcher{results: []usi.SearchResult{found("3a2b")}}

	iv := rec.Interventions[0]
	// 물러진 수는 `Ply-1` 手目의 국면에서 두어졌다 — 분기의 뿌리가 거기다.
	node, err := whatifNodeOf(
		t.Context(), rootOf(rec), whatifRequest{Ply: iv.Ply - 1, Moves: []string{iv.RetractedUSI}}, search, nil,
	)
	if err != nil {
		t.Fatalf("whatifNodeOf: %v", err)
	}
	if node.Line[0].Ja != "▲2二角成" || node.Line[0].By != game.SideHuman {
		t.Errorf("line[0] = %+v", node.Line[0])
	}
}

// 王手는 **서버가 짚는다.** 화면은 규칙을 모르므로 이 칸이 안 오면 王手가 안 보인다.
func TestWhatIfMarksCheck(t *testing.T) {
	rec := recordOf("b", "7g7f", "3c3d", "8h2b+", "3a2b")
	search := &fakeSearcher{results: []usi.SearchResult{found("5a4b")}}

	// 4二에 角을 打하면 5一의 玉에 닿는다.
	node, err := whatifNodeOf(t.Context(), rootOf(rec), whatifRequest{Ply: 4, Moves: []string{"B*4b"}}, search, nil)
	if err != nil {
		t.Fatalf("whatifNodeOf: %v", err)
	}
	if node.Line[0].Checked != "5a" {
		t.Errorf("line[0].checked = %q, want 5a", node.Line[0].Checked)
	}
	if node.Checked != "5a" {
		t.Errorf("checked = %q, want 5a", node.Checked)
	}
}

// 못 두는 수는 거절한다. **분기는 그 국면 위에서 새로 두는 일**이라, 어긋난 수를 흘려보내면
// 한 번도 없었던 국면을 그럴듯하게 그리게 된다.
func TestWhatIfRejectsIllegalBranchMove(t *testing.T) {
	rec := recordOf("b", "7g7f", "3c3d")
	for _, u := range []string{"1a1b", "zzzz", "7g7f"} {
		search := &fakeSearcher{results: []usi.SearchResult{found("2g2f")}}
		_, err := whatifNodeOf(t.Context(), rootOf(rec), whatifRequest{Ply: 2, Moves: []string{u}}, search, nil)
		if !errors.Is(err, errWhatifMove) {
			t.Errorf("%q: err = %v, want errWhatifMove", u, err)
		}
	}
}

// 기보에 구멍이 있으면 그 뒤로는 분기를 못 연다. review.go 는 거기서 멈추고 뒤를 표기 없이
// 내보내면 되지만, 여기는 **그 국면 위에서 두는** 일이라 어긋난 판이면 아예 안 된다.
func TestWhatIfRefusesBrokenRecord(t *testing.T) {
	holed := store.GameRecord{
		GameSummary: store.GameSummary{ID: 7, MyColor: "b"},
		Moves: []store.RecordedMove{
			{Ply: 1, USI: "7g7f"},
			{Ply: 3, USI: "6g6f"}, // 2手目가 빠졌다
		},
	}
	empty := &fakeSearcher{}
	if _, err := whatifNodeOf(t.Context(), rootOf(holed), whatifRequest{Ply: 2}, empty, nil); !errors.Is(err, errWhatifPly) {
		t.Errorf("구멍 뒤: err = %v, want errWhatifPly", err)
	}

	// 기보보다 뒤의 手数도 마찬가지다.
	rec := recordOf("b", "7g7f")
	if _, err := whatifNodeOf(t.Context(), rootOf(rec), whatifRequest{Ply: 5}, empty, nil); !errors.Is(err, errWhatifPly) {
		t.Errorf("기보 밖: err = %v, want errWhatifPly", err)
	}

	// 시작 국면을 못 읽으면 한 수도 두지 않는다.
	broken := recordOf("b", "7g7f")
	broken.StartSFEN = "not-a-sfen"
	if _, err := whatifNodeOf(t.Context(), rootOf(broken), whatifRequest{Ply: 0}, empty, nil); !errors.Is(err, errWhatifPly) {
		t.Errorf("깨진 시작 국면: err = %v, want errWhatifPly", err)
	}
}

// 詰み이면 그 자리에서 끝난다. **후보도 값도 없다** — 둘 수가 없는 국면에 최선수를 그리면
// 화면이 없는 수를 짚는다. 엔진도 안 부른다.
func TestWhatIfStopsAtCheckmate(t *testing.T) {
	rec := store.GameRecord{GameSummary: store.GameSummary{ID: 7, MyColor: "b"}}
	// 5一의 玉이 혼자 있고 先手가 金을 손에 들고 있다. `G*5b` 로 一手詰め다.
	rec.StartSFEN = "4k4/9/4G4/9/9/9/9/9/4K4 b G 1"

	search := &fakeSearcher{}
	node, err := whatifNodeOf(t.Context(), rootOf(rec), whatifRequest{Ply: 0, Moves: []string{"G*5b"}}, search, nil)
	if err != nil {
		t.Fatalf("whatifNodeOf: %v", err)
	}
	if node.Status != game.StatusCheckmate {
		t.Fatalf("status = %q, want checkmate", node.Status)
	}
	if len(node.LegalMoves) != 0 || len(node.Candidates) != 0 || node.EvalCp != nil {
		t.Errorf("끝난 국면에 %d 합법수·%d 후보·eval %v", len(node.LegalMoves), len(node.Candidates), node.EvalCp)
	}
	if len(search.searches()) != 0 {
		t.Error("끝난 국면을 엔진에 물었다")
	}
}

// 후보에 못 두는 수가 섞이면 **그 줄만 버린다.** 화면에서 「이렇게 뒀어야 한다」는 단언이라
// 틀린 것을 그리느니 적게 그린다.
func TestWhatIfDropsUnplayableCandidates(t *testing.T) {
	rec := recordOf("b", "7g7f", "3c3d")
	search := &fakeSearcher{results: []usi.SearchResult{found("2g2f", "5a5b", "6g6f")}} // 5a5b 는 남의 駒다

	node, err := whatifNodeOf(t.Context(), rootOf(rec), whatifRequest{Ply: 2}, search, nil)
	if err != nil {
		t.Fatalf("whatifNodeOf: %v", err)
	}
	usis := make([]string, 0, len(node.Candidates))
	for _, c := range node.Candidates {
		usis = append(usis, c.USI)
	}
	if !slices.Equal(usis, []string{"2g2f", "6g6f"}) {
		t.Errorf("candidates = %v, want [2g2f 6g6f]", usis)
	}
}

// 詰み은 cp가 아니라 手数로 나간다. 30000이라는 숫자를 화면에 그대로 흘리면 그건
// 평가치가 아니라 환산값이다.
func TestWhatIfReportsMateInPlies(t *testing.T) {
	rec := recordOf("b", "7g7f", "3c3d")
	search := &fakeSearcher{results: []usi.SearchResult{{
		Depth: whatifDepth, Best: "2g2f", ScoreCp: usi.MateCp, IsMate: true, MateIn: 5,
		Lines: []usi.SearchLine{
			{Depth: whatifDepth, MultiPV: 1, Move: "2g2f", ScoreCp: usi.MateCp, IsMate: true, MateIn: 5},
		},
	}}}

	node, err := whatifNodeOf(t.Context(), rootOf(rec), whatifRequest{Ply: 2}, search, nil)
	if err != nil {
		t.Fatalf("whatifNodeOf: %v", err)
	}
	if node.MateIn != 5 || node.Candidates[0].MateIn != 5 {
		t.Errorf("mateIn = %d / %d, want 5 5", node.MateIn, node.Candidates[0].MateIn)
	}
}

// 詰み이 섞인 줄에는 **낙폭을 안 적는다.** cp가 환산값이라 뺄셈이 29900을 내놓고, 화면은
// 그것을 「최선수보다 29900 손해」로 읽는다 — 자가 다른 두 값의 차다.
func TestWhatIfLeavesLossOutWhenMateIsInTheList(t *testing.T) {
	rec := recordOf("b", "7g7f", "3c3d")
	search := &fakeSearcher{results: []usi.SearchResult{{
		Depth: whatifDepth, Best: "2g2f", ScoreCp: usi.MateCp - 50, IsMate: true, MateIn: 5,
		Lines: []usi.SearchLine{
			{Depth: whatifDepth, MultiPV: 1, Move: "2g2f", ScoreCp: usi.MateCp - 50, IsMate: true, MateIn: 5},
			{Depth: whatifDepth, MultiPV: 2, Move: "6g6f", ScoreCp: 100},
		},
	}}}

	node, err := whatifNodeOf(t.Context(), rootOf(rec), whatifRequest{Ply: 2}, search, nil)
	if err != nil {
		t.Fatalf("whatifNodeOf: %v", err)
	}
	if len(node.Candidates) != 2 {
		t.Fatalf("candidates = %+v", node.Candidates)
	}
	for i, c := range node.Candidates {
		if c.LossCp != 0 {
			t.Errorf("candidates[%d].lossCp = %d, want 0 — 詰み이 섞인 자리다", i, c.LossCp)
		}
	}
}

// **이미 잰 국면은 다시 재지 않는다.** 탐색이 0번이 되는 것보다 중요한 것은 값이 안
// 흔들리는 것이다 — 같은 국면·같은 깊이가 치환표 상태에 따라 ±150cp 갈린다(§34 ②).
func TestWhatIfUsesTheCache(t *testing.T) {
	rec := recordOf("b", "7g7f", "3c3d")
	root := rootOf(rec)
	pos := replayed(t, root, 2)

	cache := &fakeCache{rows: map[string]store.Position{
		archive.Key(pos): {
			SFENKey:       archive.Key(pos),
			SideToMove:    "b",
			ComputedDepth: whatifDepth,
			Candidates: []store.Candidate{
				{USI: "2g2f", Cp: 96},
				{USI: "6i7h", Cp: 76},
				{USI: "5i6h", Cp: 51},
			},
		},
	}}
	search := &fakeSearcher{} // 부르면 에러다 — 부르지 않아야 한다

	node, err := whatifNodeOf(t.Context(), root, whatifRequest{Ply: 2}, search, cache)
	if err != nil {
		t.Fatalf("whatifNodeOf: %v", err)
	}
	if len(search.searches()) != 0 {
		t.Error("캐시에 있는데 다시 쟀다")
	}
	if node.EvalCp == nil || *node.EvalCp != 96 {
		t.Fatalf("evalCp = %v, want 96", node.EvalCp)
	}
	// 표기는 캐시에 없다. **판에서 만든다** — 그래서 캐시 히트와 미스가 같은 모양이다.
	if len(node.Candidates) != 3 || node.Candidates[0].Ja != "▲2六歩" {
		t.Errorf("candidates = %+v", node.Candidates)
	}
	if node.Candidates[1].LossCp != 20 {
		t.Errorf("candidates[1].lossCp = %d, want 20", node.Candidates[1].LossCp)
	}
}

// **얕은 캐시는 안 쓴다.** 개입 판정이 k=1로 남긴 행을 그대로 쓰면 「최선수 Top 3」이
// 1개가 된다 — 약속한 것이 셋이므로 다시 잰다.
func TestWhatIfIgnoresTooFewCachedCandidates(t *testing.T) {
	rec := recordOf("b", "7g7f", "3c3d")
	root := rootOf(rec)
	key := archive.Key(replayed(t, root, 2))

	rows := map[string]store.Position{
		"후보가 하나": {ComputedDepth: whatifDepth, Candidates: []store.Candidate{{USI: "2g2f", Cp: 96}}},
		"얕게 쟀다": {ComputedDepth: whatifDepth - 2, Candidates: []store.Candidate{
			{USI: "2g2f", Cp: 1}, {USI: "6g6f", Cp: 2}, {USI: "1g1f", Cp: 3},
		}},
	}
	for name, row := range rows {
		cache := &fakeCache{rows: map[string]store.Position{key: row}}
		search := &fakeSearcher{results: []usi.SearchResult{found("2g2f", "6g6f", "1g1f")}}

		if _, err := whatifNodeOf(t.Context(), root, whatifRequest{Ply: 2}, search, cache); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(search.searches()) != 1 {
			t.Errorf("%s: 다시 재지 않았다", name)
		}
	}
}

// replayed 는 뿌리를 ply 手目까지 둔 국면이다. 캐시 키를 만드는 데 쓴다.
func replayed(t *testing.T, root whatifRoot, ply int) shogi.Position {
	t.Helper()
	start, err := shogi.ParseSFEN(startSFENOf(root.StartSFEN))
	if err != nil {
		t.Fatalf("ParseSFEN: %v", err)
	}
	pos, _, err := replayTo(start, root.Moves, ply)
	if err != nil {
		t.Fatalf("replayTo: %v", err)
	}
	return pos
}

// 엔진이 답하지 못하면 그 요청만 실패한다. 되짚기는 그대로 살아 있어야 한다.
func TestWhatIfSurfacesEngineFailure(t *testing.T) {
	rec := recordOf("b", "7g7f")
	search := &fakeSearcher{err: errors.New("engine died")}
	if _, err := whatifNodeOf(t.Context(), rootOf(rec), whatifRequest{Ply: 1}, search, nil); !errors.Is(err, errWhatifEngine) {
		t.Errorf("err = %v, want errWhatifEngine", err)
	}
}

// 엔진이 없으면 **가정 수순만** 꺼진다. 되짚기(GET)는 조건이 다르다 —
// 엔진이 죽어도 지난 판은 그대로 볼 수 있어야 한다.
func TestWhatIfWithoutEngine(t *testing.T) {
	h := Handler(Options{Store: &store.Store{}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/games/1/whatif", strings.NewReader(`{"ply":0}`)))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	var body struct{ Error, Message string }
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error != "engine_unavailable" {
		t.Errorf("error = %q, want engine_unavailable", body.Error)
	}
	if body.Message == "" {
		t.Error("message is empty — 화면에 나갈 문구가 없다")
	}
}

// DB가 없으면 세 경로가 다 503이고 이유는 **기록 쪽**이다. 엔진 탓으로 말하면
// 화면이 「엔진만 살아나면 된다」로 읽는다.
func TestWhatIfWithoutStore(t *testing.T) {
	h := Handler(Options{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/games/1/whatif", strings.NewReader(`{"ply":0}`)))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	var body struct{ Error string }
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error != "store_unavailable" {
		t.Errorf("error = %q, want store_unavailable", body.Error)
	}
}

// 분기 길이와 본문 크기에 상한이 있다. **엔진이 붙은 경로라 상한이 곧 안전장치다.**
func TestWhatIfRejectsOverlongLine(t *testing.T) {
	h := &whatifHandler{store: &store.Store{}, search: &fakeSearcher{}}
	long := make([]string, whatifMaxLine+1)
	for i := range long {
		long[i] = "7g7f"
	}
	body, err := json.Marshal(whatifRequest{Ply: 0, Moves: long})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/games/1/whatif", strings.NewReader(string(body)))
	h.play(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// **대국 중에는 물러진 수 위에서만 분기가 자란다.**
//
// 이 표면은 최선수 셋을 답해 준다. 뿌리를 자유롭게 고를 수 있으면 그것이 곧 「지금 어떻게
// 둬야 하나」의 답이 되고, 그건 안 알려주기로 한 것이다(01-core.md §7). 되짚기에는 이 벽이
// 없다 — 끝난 판이라 무엇을 둬 봐도 아무도 안 잃는다.
func TestBranchRootOnlyOpensOnTheRetractedMove(t *testing.T) {
	var played confirmed
	played.set(game.Snapshot{
		Moves:        []game.Move{{USI: "7g7f"}, {USI: "3c3d"}},
		Intervention: &game.Intervention{RetractedUSI: "8h3c+"},
	})

	ply, retracted, open := played.branchRoot()
	if !open {
		t.Fatal("개입 중인데 분기가 안 열렸다")
	}
	if ply != 2 || retracted != "8h3c+" {
		t.Errorf("ply=%d retracted=%q, want 2 / 8h3c+", ply, retracted)
	}

	// 개입이 없는 스냅샷이 오면 그 자리에서 닫힌다 — 다음 착수가 개입을 지운다.
	played.set(game.Snapshot{Moves: []game.Move{{USI: "7g7f"}, {USI: "3c3d"}}})
	if _, _, open := played.branchRoot(); open {
		t.Error("개입이 없는데 분기가 열려 있다")
	}
}
