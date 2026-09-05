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
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/handicap"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 검토는 뿌리만 새롭다. 계산부(branch.go)는 되짚기가 이미 확인하고 있으므로, 여기서
// 보는 것은 그 경계 넷이다 — 뿌리가 手合割에서 오는가 · 관점이 先手인가 · 슬롯 벽 ·
// 깊이가 대국과 같은가(캐시가 한 무리여야 한다).

// exploreTest 는 검토 핸들러 하나다. store 는 nil이다 — 캐시가 없어도 답이 같은 것이
// 이 표면의 성질이고(exploreHandler.store), 여기서 확인하는 것은 뿌리와 벽이라 DB에
// 닿을 이유가 없다. 쿠키도 없다: 이 표면에 자격이 없다(journal §100).
func exploreTest(t *testing.T, search Searcher) *exploreHandler {
	t.Helper()
	return newExploreHandler(nil, search)
}

// post 는 검토에 한 걸음 묻는다. 언제나 익명 요청이다.
func (h *exploreHandler) post(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	return h.postCtx(t, t.Context(), body)
}

func (h *exploreHandler) postCtx(t *testing.T, ctx context.Context, body string) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/explore", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.play(rec, r)
	return rec
}

// decode 는 응답을 검토 노드로 읽는다. 상태코드가 200이 아니면 거기서 멈춘다.
func decodeExplore(t *testing.T, rec *httptest.ResponseRecorder) exploreNode {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var node exploreNode
	if err := json.Unmarshal(rec.Body.Bytes(), &node); err != nil {
		t.Fatalf("decode: %v — body = %s", err, rec.Body.String())
	}
	return node
}

// 뿌리가 手合割 표에서 온다. 화면은 SFEN을 안 보내고 id 하나만 보내므로, 여기가
// 틀리면 사람이 고른 것과 다른 판을 검토하게 된다.
func TestExploreStartsFromTheHandicapPosition(t *testing.T) {
	nimai, ok := handicap.Find("nimaiochi")
	if !ok {
		t.Fatal("二枚落ち가 표에 없다")
	}
	// 上手의 수를 준다. 二枚落ち의 0手目가 上手 차례라(journal §88) 下手의 수를 주면
	// 후보가 전부 걸러지고, 그 실패는 「엔진이 이상한 답을 줬다」와 구별되지 않는다.
	search := &fakeSearcher{results: []usi.SearchResult{found("3c3d", "8c8d", "4a3b")}}
	h := exploreTest(t, search)

	node := decodeExplore(t, h.post(t, `{"handicap":"nimaiochi","moves":[]}`))

	start, err := shogi.ParseSFEN(nimai.SFEN)
	if err != nil {
		t.Fatalf("표의 SFEN을 못 읽는다: %v", err)
	}
	if node.SFEN != start.SFEN() {
		t.Errorf("sfen = %q, want %q", node.SFEN, start.SFEN())
	}
	if node.Ply != 0 || node.BasePly != 0 {
		t.Errorf("ply = %d / basePly = %d, want 0 0 — 검토의 뿌리는 언제나 0手目다", node.Ply, node.BasePly)
	}
	// 駒落ち는 上手의 駒를 빼고 그 上手부터 둔다(journal §88). 관점은 下手로 못박혀
	// 있으므로(exploreRoot) 0手目는 「내 차례」가 아니다 — 검토는 양쪽을 다 움직인다.
	if node.Turn != "w" || node.YourTurn {
		t.Errorf("turn=%q yourTurn=%v, want w false", node.Turn, node.YourTurn)
	}
	// 기준점이 같이 온다. 없으면 +1386이 「압승 중」으로 읽히고 후보의 색도 전부
	// 최대 파랑이 된다(exploreNode).
	if node.BaselineCp != nimai.BaselineCp || node.HandicapJa != nimai.Name {
		t.Errorf("baselineCp=%d handicapJa=%q, want %d %q",
			node.BaselineCp, node.HandicapJa, nimai.BaselineCp, nimai.Name)
	}
	if len(node.Candidates) != 3 {
		t.Errorf("candidates = %d, want 3 — 최선수 Top 3가 이 화면의 내용이다", len(node.Candidates))
	}

	// 대국과 같은 깊이·같은 후보 수로 묻는다. 다르면 positions 가 서로 못 쓰는
	// 무리로 갈리고, 「탐색할수록 빨라진다」가 검토 안에서만 성립한다.
	calls := search.searches()
	if len(calls) != 1 {
		t.Fatalf("searches = %d, want 1", len(calls))
	}
	if calls[0].depth != game.DefaultDepth || calls[0].multiPV != whatifCandidates {
		t.Errorf("탐색 = depth %d / multiPV %d, want %d %d",
			calls[0].depth, calls[0].multiPV, game.DefaultDepth, whatifCandidates)
	}
	if len(calls[0].moves) != 0 {
		t.Errorf("moves = %v, want 빈 줄", calls[0].moves)
	}
}

// 빈 id 가 平手다. 표에 平手가 없고 그것이 화면의 기본값이라(internal/handicap),
// 이 자리가 틀리면 기본 상태에서 판이 안 선다.
func TestExplorePlainIsTheEmptyID(t *testing.T) {
	search := &fakeSearcher{results: []usi.SearchResult{found("7g7f")}}
	h := exploreTest(t, search)

	rec := h.post(t, `{"handicap":"","moves":[]}`)
	node := decodeExplore(t, rec)

	start, err := shogi.ParseSFEN(shogi.StartSFEN)
	if err != nil {
		t.Fatalf("평수 초기 국면: %v", err)
	}
	if node.SFEN != start.SFEN() {
		t.Errorf("sfen = %q, want %q", node.SFEN, start.SFEN())
	}
	// 기준점 0은 안 나간다. 平手는 빼는 것이 없고, 0을 보내면 화면이 「互角ライン」을
	// 그릴 자리가 아닌 곳에 그린다.
	if body := rec.Body.String(); strings.Contains(body, "baselineCp") || strings.Contains(body, "handicapJa") {
		t.Errorf("平手 응답에 手合 칸이 들어 있다: %s", body)
	}
}

// 값은 下手 관점이다. 되짚기는 플레이어 관점인데 검토에는 플레이어가 없고,
// 手合 기준점이 下手 관점 cp라 그쪽으로 못박았다(exploreRoot).
func TestExploreKeepsTheSentePointOfView(t *testing.T) {
	// 1手目 뒤는 後手 차례다. 엔진은 수번(後手)에게 +100이라고 답한다.
	search := &fakeSearcher{results: []usi.SearchResult{found("8c8d", "3c3d")}}
	h := exploreTest(t, search)

	node := decodeExplore(t, h.post(t, `{"handicap":"","moves":["7g7f"]}`))

	if node.EvalCp == nil || *node.EvalCp != -100 {
		t.Fatalf("evalCp = %v, want -100 (先手 관점)", node.EvalCp)
	}
	// 후보의 cp는 뒤집지 않는다. 그 값의 주인은 그 수를 두는 쪽이다(whatifCandidate).
	if node.Candidates[0].EvalCp != 100 {
		t.Errorf("candidates[0].evalCp = %d, want 100 (수번 관점)", node.Candidates[0].EvalCp)
	}
	// 한 수도 대신 두지 않는다 — 줄에 있는 것은 받은 수뿐이다.
	if len(node.Line) != 1 || node.Line[0].USI != "7g7f" || node.Line[0].Ja != "▲7六歩" {
		t.Fatalf("line = %+v", node.Line)
	}
	if node.Turn != "w" || node.YourTurn {
		t.Errorf("turn=%q yourTurn=%v, want w false", node.Turn, node.YourTurn)
	}
}

// 로그인 없이 된다(journal §100). 쿠키 없는 요청이 국면을 받고, 엔진도 실제로 돈다 —
// 익명을 막는 것이 아니라 동시에 잡는 수를 묶는 것이 이 표면의 벽이다(exploreSlots).
func TestExploreAllowsAnonymous(t *testing.T) {
	search := &fakeSearcher{results: []usi.SearchResult{found("7g7f")}}
	h := exploreTest(t, search)

	rec := h.post(t, `{"handicap":"","moves":[]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — body = %s", rec.Code, rec.Body.String())
	}
	if len(search.searches()) == 0 {
		t.Error("익명 요청이 엔진을 안 잡았다")
	}
}

// 표에 없는 手合割. 엔진을 부르기 전에 거절한다.
func TestExploreRejectsUnknownHandicap(t *testing.T) {
	search := &fakeSearcher{results: []usi.SearchResult{found("7g7f")}}
	h := exploreTest(t, search)

	rec := h.post(t, `{"handicap":"hachimaiochi","moves":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — body = %s", rec.Code, rec.Body.String())
	}
	if len(search.searches()) != 0 {
		t.Error("없는 手合割에 탐색이 돌았다")
	}
}

// 못 두는 수는 거절한다. 화면이 규칙을 모르기 때문에 여기가 유일한 검사다.
func TestExploreRejectsAnIllegalMove(t *testing.T) {
	search := &fakeSearcher{results: []usi.SearchResult{found("7g7f")}}
	h := exploreTest(t, search)

	rec := h.post(t, `{"handicap":"","moves":["7g7f","7f7e"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "bad_move") {
		t.Errorf("body = %s, want bad_move", rec.Body.String())
	}
}

// 줄의 상한. 되짚기보다 길고(뿌리가 0手目라 한 판을 통째로 걸어 볼 수 있어야 한다),
// 그래도 유한해야 요청 하나가 되짚는 수가 묶인다.
func TestExploreCapsTheLine(t *testing.T) {
	search := &fakeSearcher{}
	h := exploreTest(t, search)

	moves := make([]string, exploreMaxLine+1)
	for i := range moves {
		moves[i] = `"7g7f"`
	}
	rec := h.post(t, `{"handicap":"","moves":[`+strings.Join(moves, ",")+`]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — body = %s", rec.Code, rec.Body.String())
	}
	// 되짚는 일조차 안 한다. 상한이 막는 것은 탐색이 아니라 요청 하나가 재생하는 수다.
	if len(search.searches()) != 0 {
		t.Error("상한을 넘은 줄에 탐색이 돌았다")
	}
	if exploreMaxLine <= whatifMaxLine {
		t.Errorf("exploreMaxLine=%d, 되짚기(%d)보다 길어야 한다", exploreMaxLine, whatifMaxLine)
	}
}

// 슬롯이 이 표면의 유일한 벽이다. 빈자리가 없으면 기다리게 두지 않고 「まだ読んでいます」로
// 답한다 — 대국에 엔진 둘이 언제나 남아 있어야 한다(exploreSlots).
//
// 실제 대기는 exploreWait 인데, 테스트는 그만큼 멈춰 있을 이유가 없어서 요청 ctx의
// 시한을 짧게 준다. 보는 것은 꽉 찬 슬롯에서 거절되고 엔진을 안 잡는다는 것 하나다.
func TestExploreRejectsWhenAllSlotsAreBusy(t *testing.T) {
	search := &fakeSearcher{results: []usi.SearchResult{found("7g7f")}}
	h := exploreTest(t, search)

	for range exploreSlots {
		h.slots <- struct{}{}
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	rec := h.postCtx(t, ctx, `{"handicap":"","moves":[]}`)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 — body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "busy") {
		t.Errorf("body = %s, want busy", rec.Body.String())
	}
	if len(search.searches()) != 0 {
		t.Error("슬롯이 꽉 찼는데 탐색이 돌았다")
	}
	// 슬롯을 되돌려 놓지 않으면 다음 요청이 통째로 막힌다 — 거절 경로가 빌린 것을
	// 반납하지 않는가를 여기서 본다.
	if len(h.slots) != exploreSlots {
		t.Errorf("슬롯 %d개가 남아 있다, want %d", len(h.slots), exploreSlots)
	}
	for range exploreSlots {
		<-h.slots
	}
	if rec2 := h.post(t, `{"handicap":"","moves":[]}`); rec2.Code != http.StatusOK {
		t.Errorf("슬롯을 비운 뒤 status = %d, want 200", rec2.Code)
	}
}

// 엔진이 답하지 못하면 503이다. 다시 눌러 볼 수 있는 실패이고, 검토는 아무것도 안 잃는다.
func TestExploreReportsEngineFailure(t *testing.T) {
	search := &fakeSearcher{err: errors.New("fake: engine is down")}
	h := exploreTest(t, search)

	rec := h.post(t, `{"handicap":"","moves":[]}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 — body = %s", rec.Code, rec.Body.String())
	}
}

// 手合割 일곱 종이 전부 검토의 뿌리가 된다. 표에 SFEN 오타가 하나 있으면 그 手合만
// 조용히 안 열리므로 전수로 본다.
func TestExploreOpensEveryHandicap(t *testing.T) {
	for _, hc := range handicap.All() {
		t.Run(hc.ID, func(t *testing.T) {
			search := &fakeSearcher{results: []usi.SearchResult{found("3c3d", "8c8d")}}
			h := exploreTest(t, search)

			node := decodeExplore(t, h.post(t, `{"handicap":"`+hc.ID+`","moves":[]}`))
			if node.Turn != "w" {
				t.Errorf("turn = %q, want w — 駒落ち는 언제나 上手부터다(journal §88)", node.Turn)
			}
			if node.BaselineCp != hc.BaselineCp {
				t.Errorf("baselineCp = %d, want %d", node.BaselineCp, hc.BaselineCp)
			}
			// △3四歩는 일곱 종 어디에서나 둘 수 있다 — 落とす 것에 歩가 없다.
			if !slices.Contains(node.LegalMoves, "3c3d") {
				t.Errorf("legalMoves 에 △3四歩가 없다 — 판이 안 섰다")
			}
		})
	}
}

// 뿌리가 판(SFEN)으로도 온다. 사진에서 읽어 와 사람이 확인한 국면이 手合割+수순으로는
// 표현이 안 되므로 §37이 닫아 둔 문을 여기서 연다(journal §129).
func TestExploreStartsFromAGivenPosition(t *testing.T) {
	// 平手 초기 국면에서 ▲7六歩만 둔 판. 뿌리가 그대로 서는지를 보는 자리라 내용은
	// 아무 국면이어도 되지만, 아는 국면이면 틀렸을 때 무엇이 틀렸는지가 보인다.
	const sfen = "lnsgkgsnl/1r5b1/ppppppppp/9/9/2P6/PP1PPPPPP/1B5R1/LNSGKGSNL w - 1"
	search := &fakeSearcher{results: []usi.SearchResult{found("3c3d", "8c8d", "4a3b")}}
	h := exploreTest(t, search)

	node := decodeExplore(t, h.post(t, `{"sfen":"`+sfen+`","moves":[]}`))

	if node.SFEN != sfen {
		t.Errorf("sfen = %q, want %q", node.SFEN, sfen)
	}
	if node.Ply != 0 || node.BasePly != 0 {
		t.Errorf("ply = %d / basePly = %d, want 0 0", node.Ply, node.BasePly)
	}
	// 手合割이 없다. 임의의 국면에 「형세 0」이 정의되지 않으므로 기준점도 이름도
	// 안 실리고, 화면이 「互角ライン」을 말하지 않는다.
	if node.BaselineCp != 0 || node.HandicapJa != "" {
		t.Errorf("baselineCp=%d handicapJa=%q, want 0 and empty", node.BaselineCp, node.HandicapJa)
	}
	// 아래쪽을 先手로 둔 판이라(internal/boardread) 관점이 Black 이다 — 이 국면은
	// 後手 차례이므로 「내 차례」가 아니다.
	if node.Turn != "w" || node.YourTurn {
		t.Errorf("turn=%q yourTurn=%v, want w false", node.Turn, node.YourTurn)
	}
	// 깊이와 후보 수가 手合割 뿌리와 같아야 positions 가 한 무리로 남는다.
	calls := search.searches()
	if len(calls) != 1 || calls[0].depth != game.DefaultDepth || calls[0].multiPV != whatifCandidates {
		t.Fatalf("searches = %+v, want one at depth %d / multiPV %d", calls, game.DefaultDepth, whatifCandidates)
	}
}

// 판 뿌리에서도 수순을 얹을 수 있다. 그 자리에서 계속 둬 보는 것이 이 표면의 내용이다.
func TestExploreWalksOnFromAGivenPosition(t *testing.T) {
	const sfen = "lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1"
	search := &fakeSearcher{results: []usi.SearchResult{found("3c3d"), found("8h2b+")}}
	h := exploreTest(t, search)

	node := decodeExplore(t, h.post(t, `{"sfen":"`+sfen+`","moves":["7g7f"]}`))

	if node.Ply != 1 {
		t.Errorf("ply = %d, want 1", node.Ply)
	}
	if len(node.Line) != 1 || node.Line[0].USI != "7g7f" {
		t.Errorf("line = %+v, want the one move we sent", node.Line)
	}
}

// 재생이 하던 검증을 룰 엔진이 대신한다. 手合割 뿌리는 한 수씩 ValidateMove 를 지나가서
// 국면이 성립하는 것이 공짜인데, 판 뿌리에는 지나갈 수순이 없다.
func TestExploreRefusesAPositionThatCannotStand(t *testing.T) {
	cases := map[string]string{
		// 같은 筋에 歩가 둘이다.
		"二歩":        "4k4/9/9/9/4P4/9/4P4/9/4K4 b - 1",
		"玉이 없다":     "4k4/9/9/9/9/9/9/9/9 b - 1",
		"말이 한 벌을 넘": "lnsgkgsnl/1r5b1/ppppppppp/9/4P4/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1",
		// 手番이 아닌 쪽이 王手를 받고 있다 — 사람이 手番을 잘못 고른 자리다.
		"왕수 방치":    "4k4/9/9/9/4R4/9/9/9/3K5 b - 1",
		"SFEN이 아님": "not a position",
	}
	for name, sfen := range cases {
		t.Run(name, func(t *testing.T) {
			search := &fakeSearcher{results: []usi.SearchResult{found("3c3d")}}
			h := exploreTest(t, search)

			rec := h.post(t, `{"sfen":"`+sfen+`","moves":[]}`)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 — body = %s", rec.Code, rec.Body.String())
			}
			// 엔진을 한 번도 안 불렀어야 한다. 성립하지 않는 판에 슬롯을 쓰면 그 자리가
			// 곧 「아무 국면이나 재 주는」 문이 된다.
			if got := search.searches(); len(got) != 0 {
				t.Errorf("searches = %d, want 0 — an impossible position must not reach the engine", len(got))
			}
		})
	}
}

// 뿌리를 정하는 값이 둘 다 오면 서버가 골라야 하고, 그 선택은 화면과 어긋날 수 있다.
func TestExploreRefusesTwoRootsAtOnce(t *testing.T) {
	search := &fakeSearcher{results: []usi.SearchResult{found("3c3d")}}
	h := exploreTest(t, search)

	rec := h.post(t, `{"handicap":"nimaiochi","sfen":"lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1","moves":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — body = %s", rec.Code, rec.Body.String())
	}
}
