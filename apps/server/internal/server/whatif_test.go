package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// whatifNodeOf 는 DB를 안 탄다. 엔진만 손으로 만들어 넣으면 분기의 정합성을 전부
// 확인할 수 있다 — **여기가 리뷰에서 처음으로 엔진에 매인 자리**라 그 경계를 테스트에서도
// 지킨다(intervene 이 엔진을 모르는 것과 같은 구조다).

// fakeSearcher 는 정해진 답만 준다. 부른 순서대로 하나씩 나간다.
type fakeSearcher struct {
	results []usi.SearchResult
	err     error

	calls []searchCall
}

type searchCall struct {
	moves   []string
	multiPV int
}

func (f *fakeSearcher) SearchMultiPV(_ context.Context, _ string, moves []string, _, multiPV int) (usi.SearchResult, error) {
	f.calls = append(f.calls, searchCall{moves: slices.Clone(moves), multiPV: multiPV})
	if f.err != nil {
		return usi.SearchResult{}, f.err
	}
	if len(f.calls) > len(f.results) {
		return usi.SearchResult{}, errors.New("fake: no more results")
	}
	return f.results[len(f.calls)-1], nil
}

func lines(moves ...string) []usi.SearchLine {
	out := make([]usi.SearchLine, 0, len(moves))
	for i, m := range moves {
		out = append(out, usi.SearchLine{Depth: whatifDepth, MultiPV: i + 1, Move: m, ScoreCp: 100 - i*40})
	}
	return out
}

// 상대 차례면 **서버가 그 자리에서 두게 한다.** 물어보게 하면 「그래서 어떻게 되나」를
// 보러 온 화면에 통과 의례가 하나 더 생긴다.
func TestWhatIfPlaysTheOpponentReply(t *testing.T) {
	rec := recordOf("b", "7g7f", "3c3d", "6g6f")
	search := &fakeSearcher{results: []usi.SearchResult{
		{Best: "8c8d"},
		{ScoreCp: 42, Lines: lines("2g2f", "6g6f")},
	}}

	node, err := whatifNodeOf(t.Context(), rec, whatifRequest{Ply: 1}, search)
	if err != nil {
		t.Fatalf("whatifNodeOf: %v", err)
	}

	if len(node.Line) != 1 {
		t.Fatalf("line = %+v, want 상대의 응수 한 수", node.Line)
	}
	if node.Line[0].By != game.SideEngine || node.Line[0].Ja != "△8四歩" {
		t.Errorf("line[0] = %+v, want △8四歩 by engine", node.Line[0])
	}
	if node.Line[0].SFEN == "" {
		t.Error("line[0].sfen is empty — 화면이 판을 못 그린다")
	}
	// **분기의 끝은 언제나 사람 차례다.** 화면이 「이제 누가 둘 차례인가」로 갈리지 않는다.
	if !node.YourTurn || node.Ply != 2 {
		t.Errorf("yourTurn=%v ply=%d, want true 2", node.YourTurn, node.Ply)
	}
	if node.Status != game.StatusPlaying {
		t.Errorf("status = %q, want playing", node.Status)
	}
	// 합법수는 **서버가 준다.** 이게 안 오면 화면이 규칙을 갖게 된다.
	if len(node.LegalMoves) == 0 {
		t.Error("legalMoves is empty")
	}
	if node.EvalCp == nil || *node.EvalCp != 42 {
		t.Errorf("evalCp = %v, want 42", node.EvalCp)
	}
	if len(node.Candidates) != 2 || node.Candidates[0].Ja != "▲2六歩" || node.Candidates[1].Ja != "▲6六歩" {
		t.Fatalf("candidates = %+v", node.Candidates)
	}
	if node.Candidates[0].EvalCp != 100 {
		t.Errorf("candidates[0].evalCp = %d, want 100", node.Candidates[0].EvalCp)
	}

	// 엔진에는 **뿌리까지의 실제 기보가 그대로 앞에 붙어** 간다. 국면만 넘기면
	// 千日手를 세는 근거가 사라진다.
	if len(search.calls) != 2 {
		t.Fatalf("searches = %d, want 2", len(search.calls))
	}
	if !slices.Equal(search.calls[0].moves, []string{"7g7f"}) || search.calls[0].multiPV != 1 {
		t.Errorf("첫 탐색 = %+v", search.calls[0])
	}
	if !slices.Equal(search.calls[1].moves, []string{"7g7f", "8c8d"}) || search.calls[1].multiPV != whatifCandidates {
		t.Errorf("두 번째 탐색 = %+v", search.calls[1])
	}
}

// 사람의 수를 받으면 그 수를 두고 **상대가 답한다.** 그 한 걸음이 분기의 전부다.
func TestWhatIfAppliesHumanMoveThenReplies(t *testing.T) {
	rec := recordOf("b", "7g7f", "3c3d")
	search := &fakeSearcher{results: []usi.SearchResult{
		{Best: "3a2b"},
		{ScoreCp: -310, Lines: lines("B*4e")},
	}}

	node, err := whatifNodeOf(t.Context(), rec, whatifRequest{Ply: 2, Moves: []string{"8h2b+"}}, search)
	if err != nil {
		t.Fatalf("whatifNodeOf: %v", err)
	}

	if len(node.Line) != 2 {
		t.Fatalf("line = %+v, want 사람의 수와 상대의 응수", node.Line)
	}
	if node.Line[0].By != game.SideHuman || node.Line[0].Ja != "▲2二角成" {
		t.Errorf("line[0] = %+v, want ▲2二角成 by human", node.Line[0])
	}
	if node.Line[1].By != game.SideEngine || node.Line[1].Ja != "△同銀" {
		t.Errorf("line[1] = %+v, want △同銀 by engine", node.Line[1])
	}
	if node.BasePly != 2 || node.Ply != 4 {
		t.Errorf("basePly=%d ply=%d, want 2 4", node.BasePly, node.Ply)
	}
	// 打도 그대로 표기가 붙는다. 화면은 USI에서 표기를 다시 만들지 않는다.
	if len(node.Candidates) != 1 || node.Candidates[0].Ja != "▲4五角" {
		t.Errorf("candidates = %+v", node.Candidates)
	}
}

// 물러진 수는 **기보에 없다.** 그 수를 그 국면에서 둬 볼 수 있는 것이 이 표면의 이유다
// (06-status.md §25 — 가르치는 것은 최선 수순이 아니라 「두려던 수의 변화」다).
func TestWhatIfPlaysTheRetractedMove(t *testing.T) {
	rec := recordOf("b", "7g7f", "3c3d", "6g6f")
	rec.Interventions = []store.RecordedIntervention{{
		Ply: 3, Kind: "blunder", Category: "hangs_piece", RetractedUSI: "8h2b+",
	}}
	search := &fakeSearcher{results: []usi.SearchResult{
		{Best: "3a2b"},
		{ScoreCp: -420, Lines: lines("7i6h")},
	}}

	iv := rec.Interventions[0]
	// 물러진 수는 `Ply-1` 手目의 국면에서 두어졌다 — 분기의 뿌리가 거기다.
	node, err := whatifNodeOf(t.Context(), rec, whatifRequest{Ply: iv.Ply - 1, Moves: []string{iv.RetractedUSI}}, search)
	if err != nil {
		t.Fatalf("whatifNodeOf: %v", err)
	}
	if node.Line[0].Ja != "▲2二角成" || node.Line[0].By != game.SideHuman {
		t.Errorf("line[0] = %+v", node.Line[0])
	}
}

// 사람이 後手인 판. **누구의 수인지가 뒤집히면** 가정 수순이 남의 수를 내 것으로 그린다.
func TestWhatIfAttributesMovesByColor(t *testing.T) {
	rec := recordOf("w", "7g7f")
	search := &fakeSearcher{results: []usi.SearchResult{
		{ScoreCp: -20, Lines: lines("3c3d")},
	}}

	// 1手目 뒤는 後手(=사람) 차례다. 상대의 응수를 붙일 자리가 없다.
	node, err := whatifNodeOf(t.Context(), rec, whatifRequest{Ply: 1}, search)
	if err != nil {
		t.Fatalf("whatifNodeOf: %v", err)
	}
	if len(node.Line) != 0 {
		t.Errorf("line = %+v, want 빈 줄 (이미 사람 차례다)", node.Line)
	}
	if !node.YourTurn {
		t.Error("yourTurn = false, want true")
	}
	if len(search.calls) != 1 || search.calls[0].multiPV != whatifCandidates {
		t.Fatalf("searches = %+v, want 후보 탐색 하나뿐", search.calls)
	}
}

// 王手는 **서버가 짚는다.** 화면은 규칙을 모르므로 이 칸이 안 오면 王手가 안 보인다.
func TestWhatIfMarksCheck(t *testing.T) {
	rec := recordOf("b", "7g7f", "3c3d", "8h2b+", "3a2b")
	search := &fakeSearcher{results: []usi.SearchResult{
		{Best: "5a4b"},
		{ScoreCp: 0, Lines: lines("7i6h")},
	}}

	// 4二에 角을 打하면 5一의 玉에 닿는다.
	node, err := whatifNodeOf(t.Context(), rec, whatifRequest{Ply: 4, Moves: []string{"B*4b"}}, search)
	if err != nil {
		t.Fatalf("whatifNodeOf: %v", err)
	}
	if node.Line[0].Checked != "5a" {
		t.Errorf("line[0].checked = %q, want 5a", node.Line[0].Checked)
	}
}

// 못 두는 수는 거절한다. **분기는 그 국면 위에서 새로 두는 일**이라, 어긋난 수를 흘려보내면
// 한 번도 없었던 국면을 그럴듯하게 그리게 된다.
func TestWhatIfRejectsIllegalBranchMove(t *testing.T) {
	rec := recordOf("b", "7g7f")
	for _, u := range []string{"9i9h", "zzzz", "7g7f"} {
		search := &fakeSearcher{results: []usi.SearchResult{{Best: "3c3d"}}}
		_, err := whatifNodeOf(t.Context(), rec, whatifRequest{Ply: 1, Moves: []string{u}}, search)
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
	if _, err := whatifNodeOf(t.Context(), holed, whatifRequest{Ply: 2}, &fakeSearcher{}); !errors.Is(err, errWhatifPly) {
		t.Errorf("구멍 뒤: err = %v, want errWhatifPly", err)
	}

	// 기보보다 뒤의 手数도 마찬가지다.
	rec := recordOf("b", "7g7f")
	if _, err := whatifNodeOf(t.Context(), rec, whatifRequest{Ply: 5}, &fakeSearcher{}); !errors.Is(err, errWhatifPly) {
		t.Errorf("기보 밖: err = %v, want errWhatifPly", err)
	}

	// 시작 국면을 못 읽으면 한 수도 두지 않는다.
	broken := recordOf("b", "7g7f")
	broken.StartSFEN = "not-a-sfen"
	if _, err := whatifNodeOf(t.Context(), broken, whatifRequest{Ply: 0}, &fakeSearcher{}); !errors.Is(err, errWhatifPly) {
		t.Errorf("깨진 시작 국면: err = %v, want errWhatifPly", err)
	}
}

// **엔진 출력을 믿지 않는다.** 투료나 못 두는 수가 오면 거기서 판이 끝난 것으로 둔다 —
// 대국 루프(session.applyEngineMove)와 같은 자리다.
func TestWhatIfEndsOnUnplayableEngineReply(t *testing.T) {
	rec := recordOf("b", "7g7f")
	for _, best := range []string{"resign", "win", "none", "", "9i9h"} {
		search := &fakeSearcher{results: []usi.SearchResult{{Best: best}}}
		node, err := whatifNodeOf(t.Context(), rec, whatifRequest{Ply: 1}, search)
		if err != nil {
			t.Fatalf("%q: whatifNodeOf: %v", best, err)
		}
		if node.Status != game.StatusResigned {
			t.Errorf("%q: status = %q, want resigned", best, node.Status)
		}
		if len(node.Line) != 0 {
			t.Errorf("%q: line = %+v, want 빈 줄", best, node.Line)
		}
		if node.SFEN == "" {
			t.Errorf("%q: sfen is empty — 끝난 판도 그려져야 한다", best)
		}
	}
}

// 후보에 못 두는 수가 섞이면 **그 줄만 버린다.** 화면에서 「이렇게 뒀어야 한다」는 단언이라
// 틀린 것을 그리느니 적게 그린다.
func TestWhatIfDropsUnplayableCandidates(t *testing.T) {
	rec := recordOf("b", "7g7f", "3c3d")
	search := &fakeSearcher{results: []usi.SearchResult{
		{ScoreCp: 30, Lines: []usi.SearchLine{
			{MultiPV: 1, Move: "2g2f", ScoreCp: 40},
			{MultiPV: 2, Move: "5a5b", ScoreCp: 10}, // 남의 駒다
			{MultiPV: 3, Move: "6g6f", ScoreCp: 5},
		}},
	}}

	node, err := whatifNodeOf(t.Context(), rec, whatifRequest{Ply: 2}, search)
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
	search := &fakeSearcher{results: []usi.SearchResult{
		{ScoreCp: usi.MateCp, IsMate: true, MateIn: 5, Lines: []usi.SearchLine{
			{MultiPV: 1, Move: "2g2f", ScoreCp: usi.MateCp, IsMate: true, MateIn: 5},
		}},
	}}

	node, err := whatifNodeOf(t.Context(), rec, whatifRequest{Ply: 2}, search)
	if err != nil {
		t.Fatalf("whatifNodeOf: %v", err)
	}
	if node.MateIn != 5 || node.Candidates[0].MateIn != 5 {
		t.Errorf("mateIn = %d / %d, want 5 5", node.MateIn, node.Candidates[0].MateIn)
	}
}

// 엔진이 답하지 못하면 그 요청만 실패한다. 되짚기는 그대로 살아 있어야 한다.
func TestWhatIfSurfacesEngineFailure(t *testing.T) {
	rec := recordOf("b", "7g7f")
	search := &fakeSearcher{err: errors.New("engine died")}
	if _, err := whatifNodeOf(t.Context(), rec, whatifRequest{Ply: 1}, search); !errors.Is(err, errWhatifEngine) {
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
