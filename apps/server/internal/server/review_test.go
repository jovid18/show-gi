package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/handicap"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// detailOf 는 DB를 안 탄다. 재현이 이 패키지의 순수 함수라서, 기록을 손으로 만들어
// 엔진도 DB도 없이 확인할 수 있다 — 여기가 리뷰 화면의 정합성이 걸린 자리다.

func recordOf(myColor string, usis ...string) store.GameRecord {
	rec := store.GameRecord{GameSummary: store.GameSummary{ID: 7, MyColor: myColor}}
	for i, u := range usis {
		rec.Moves = append(rec.Moves, store.RecordedMove{Ply: i + 1, USI: u})
	}
	return rec
}

func TestDetailReplaysKifu(t *testing.T) {
	got := detailOf(recordOf("b", "7g7f", "3c3d", "8h2b+"))

	if len(got.Moves) != 3 {
		t.Fatalf("moves = %d, want 3", len(got.Moves))
	}
	// 표기는 서버가 만든다. 화면이 USI에서 다시 만들면 두 벌이 된다.
	wantJa := []string{"▲7六歩", "△3四歩", "▲2二角成"}
	for i, w := range wantJa {
		if got.Moves[i].Ja != w {
			t.Errorf("moves[%d].ja = %q, want %q", i, got.Moves[i].Ja, w)
		}
		if got.Moves[i].SFEN == "" {
			t.Errorf("moves[%d].sfen is empty — 판을 못 그린다", i)
		}
	}

	// 사람이 先手면 홀수 手数가 사람이다.
	wantBy := []game.Side{game.SideHuman, game.SideEngine, game.SideHuman}
	for i, w := range wantBy {
		if got.Moves[i].By != w {
			t.Errorf("moves[%d].by = %q, want %q", i, got.Moves[i].By, w)
		}
	}

	if got.StartSFEN == "" {
		t.Error("startSfen is empty — 0手目의 판을 못 그린다")
	}
}

// 사람이 後手면 짝수 手数가 사람이다. 여기가 뒤집히면 리뷰가 남의 실수를 내 것으로
// 보여준다.
func TestDetailAttributesMovesByColor(t *testing.T) {
	got := detailOf(recordOf("w", "7g7f", "3c3d"))

	if got.Moves[0].By != game.SideEngine {
		t.Errorf("moves[0].by = %q, want engine", got.Moves[0].By)
	}
	if got.Moves[1].By != game.SideHuman {
		t.Errorf("moves[1].by = %q, want human", got.Moves[1].By)
	}
}

// 평가치는 DB에 先手 관점으로 들어 있다. 後手로 둔 판에서 뒤집지 않으면 잃은 판이
// 이긴 판으로 보인다.
func TestDetailFlipsEvalForWhite(t *testing.T) {
	cp := 320
	rec := recordOf("w", "7g7f")
	rec.Moves[0].EvalCp = &cp

	got := detailOf(rec)
	if got.Moves[0].EvalCp == nil || *got.Moves[0].EvalCp != -320 {
		t.Fatalf("evalCp = %v, want -320", got.Moves[0].EvalCp)
	}

	// 先手로 둔 판은 그대로다.
	rec.MyColor = "b"
	got = detailOf(rec)
	if got.Moves[0].EvalCp == nil || *got.Moves[0].EvalCp != 320 {
		t.Fatalf("evalCp = %v, want 320", got.Moves[0].EvalCp)
	}
}

// TestDetailCarriesTheHandicapBaseline 는 되짚기가 手合割의 「형세 0」을 같이 내려보내는지다.
//
// EvalCp 와 같은 관점이어야 한다(플레이어). 부호가 어긋나면 형세 그래프가 駒落ち 판을
// 반대로 그리고, 그 그림은 「접어 준 것을 다 잃었다」와 「그대로 들고 있다」를 뒤집는다.
func TestDetailCarriesTheHandicapBaseline(t *testing.T) {
	nimai, ok := handicap.Find("nimaiochi")
	if !ok {
		t.Fatal("nimaiochi 가 표에 없다")
	}

	rec := recordOf("b", "7g7f")
	rec.StartSFEN = nimai.SFEN
	if got := detailOf(rec).BaselineCp; got != nimai.BaselineCp {
		t.Errorf("下手로 둔 판: baselineCp = %d, want %d", got, nimai.BaselineCp)
	}

	// 上手를 잡는 판은 화면에 없지만(newSetup 이 下手로 덮는다) 부호 규약은 여기서 닫는다.
	rec.MyColor = "w"
	if got := detailOf(rec).BaselineCp; got != -nimai.BaselineCp {
		t.Errorf("上手로 둔 판: baselineCp = %d, want %d", got, -nimai.BaselineCp)
	}

	// 平手는 0이라 응답에 아예 안 나간다(omitempty).
	if got := detailOf(recordOf("b", "7g7f")).BaselineCp; got != 0 {
		t.Errorf("平手: baselineCp = %d, want 0", got)
	}
}

// 평가치가 없는 手数는 없는 채로 나가야 한다. 0으로 채우면 호각과 구별이 안 된다.
func TestDetailKeepsMissingEvalMissing(t *testing.T) {
	got := detailOf(recordOf("b", "7g7f"))
	if got.Moves[0].EvalCp != nil {
		t.Errorf("evalCp = %v, want nil", *got.Moves[0].EvalCp)
	}
}

// 手数에 구멍이 있으면 거기서 재현을 멈춘다. 이어서 두면 없던 국면을 그린다.
func TestDetailStopsAtGapInPlies(t *testing.T) {
	rec := store.GameRecord{
		GameSummary: store.GameSummary{ID: 7, MyColor: "b"},
		Moves: []store.RecordedMove{
			{Ply: 1, USI: "7g7f"},
			{Ply: 3, USI: "8h2b+"}, // 2手目가 빠졌다
		},
	}

	got := detailOf(rec)
	if got.Moves[0].SFEN == "" {
		t.Error("moves[0].sfen is empty — 구멍 앞은 그려져야 한다")
	}
	// 수 자체는 남는다. 둔 것은 둔 것이고, 판을 못 그릴 뿐이다.
	if len(got.Moves) != 2 {
		t.Fatalf("moves = %d, want 2", len(got.Moves))
	}
	if got.Moves[1].SFEN != "" || got.Moves[1].Ja != "" {
		t.Errorf("moves[1] = %+v, want 국면도 표기도 없음", got.Moves[1])
	}
}

// 읽을 수 없는 수가 있어도 그 앞은 살아야 한다.
func TestDetailSurvivesBrokenMove(t *testing.T) {
	got := detailOf(recordOf("b", "7g7f", "zzzz"))
	if got.Moves[0].Ja == "" {
		t.Error("moves[0].ja is empty — 앞 수까지는 살아야 한다")
	}
	if got.Moves[1].Ja != "" {
		t.Errorf("moves[1].ja = %q, want empty", got.Moves[1].Ja)
	}
}

// 물러진 수는 기보에 없다. Ply-1 手目의 국면에서 두어졌고, 리뷰는 거기서
// 표기를 만들어야 한다 — 이것이 개입에 오염되지 않은 유일한 신호다(01-core.md §5).
func TestDetailNamesRetractedMove(t *testing.T) {
	rec := recordOf("b", "7g7f", "3c3d", "6g6f")
	rec.Interventions = []store.RecordedIntervention{{
		Ply:          3,
		Kind:         "blunder",
		Category:     "hangs_piece",
		DeltaWin:     0.5,
		RetractedUSI: "8h2b+", // 3手目에 두려다 물러진 수
	}}

	got := detailOf(rec)
	if len(got.Interventions) != 1 {
		t.Fatalf("interventions = %d, want 1", len(got.Interventions))
	}
	iv := got.Interventions[0]
	if iv.RetractedJa != "▲2二角成" {
		t.Errorf("retractedJa = %q, want ▲2二角成", iv.RetractedJa)
	}
	// 이름과 문구는 서버가 만든다. 화면이 코드로 문장을 지으면 어휘가 두 벌이 된다.
	if iv.CategoryJa == "" {
		t.Error("categoryJa is empty")
	}
	if iv.Message == "" {
		t.Error("message is empty")
	}
}

// 개입이 기보보다 앞서 있어도(두는 중인 판을 읽으면 그럴 수 있다) 터지지 않는다.
func TestDetailToleratesInterventionBeyondKifu(t *testing.T) {
	rec := recordOf("b", "7g7f")
	rec.Interventions = []store.RecordedIntervention{{
		Ply: 9, Kind: "blunder", Category: "other", RetractedUSI: "8h2b+",
	}}

	got := detailOf(rec)
	if len(got.Interventions) != 1 {
		t.Fatalf("interventions = %d, want 1", len(got.Interventions))
	}
	if got.Interventions[0].RetractedJa != "" {
		t.Errorf("retractedJa = %q, want empty (그 국면을 아직 모른다)", got.Interventions[0].RetractedJa)
	}
}

// 中盤에서 시작하는 판. 手数의 홀짝이 아니라 시작 국면의 手番이 누가 먼저인지를 정한다.
func TestDetailUsesStartPositionForTurnOrder(t *testing.T) {
	rec := recordOf("b", "3c3d")
	rec.StartSFEN = "lnsgkgsnl/1r5b1/ppppppppp/9/9/2P6/PP1PPPPPP/1B5R1/LNSGKGSNL w - 2"

	got := detailOf(rec)
	// 사람이 先手(b)인데 1手目는 後手 차례다 — 그러니 엔진의 수다.
	if got.Moves[0].By != game.SideEngine {
		t.Errorf("moves[0].by = %q, want engine", got.Moves[0].By)
	}
	if got.Moves[0].Ja != "△3四歩" {
		t.Errorf("moves[0].ja = %q, want △3四歩", got.Moves[0].Ja)
	}
}

// DB가 없으면 503이다. 엔진과 조건이 갈린다 — 엔진이 죽어도 지난 판은 볼 수 있어야 하고,
// 여기가 404면 "기록이 없다"와 "기록을 못 읽는다"가 섞인다.
func TestReviewWithoutStore(t *testing.T) {
	h := Handler(Options{})
	for _, path := range []string{"/api/games", "/api/games/1", "/api/games/1/summary"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("GET %s: status = %d, want %d", path, rec.Code, http.StatusServiceUnavailable)
		}
		var body struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("GET %s: decode: %v", path, err)
		}
		if body.Error != "store_unavailable" {
			t.Errorf("GET %s: error = %q", path, body.Error)
		}
		if body.Message == "" {
			t.Errorf("GET %s: message is empty — 화면에 나갈 문구가 없다", path)
		}
	}
}

// 되짚기의 총평은 끝난 판을 한 번 더 읽는 것이고, 판이 끝나는 자리에서 WS가 보내는
// 것과 같은 함수가 만든다(§52). 여기가 보는 것은 그 라우트가 실제로 붙어 있는가다 —
// GET /api/games/{id} 와 한 세그먼트 차이라, 어긋나면 화면이 총평 대신 기보를 받는다.
//
// 엔진을 안 넣는다. 총평은 기록만 읽어 만들어지므로(summarize) 이 표면은 엔진이 없어도
// 답해야 하고, 그것이 지켜지는지가 여기서 갈린다.
func TestSummaryRouteReadsFinishedGame(t *testing.T) {
	st := openStoreForTest(t)

	before := maxGameID(t, st)

	srv := httptest.NewServer(Handler(Options{
		NewOpponent: func() game.Opponent { return &scriptedOpponent{moves: []string{"3c3d"}} },
		Store:       st,
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/game", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.CloseNow()
	read(t, ctx, conn) // 초기 스냅샷

	if err := wsjson.Write(ctx, conn, clientMsg{Type: "move", USI: "7g7f"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	readUntil(t, ctx, conn, func(m serverMsg) bool {
		return m.Type == "snapshot" && m.Snapshot.Ply == 2
	}, "상대 응수")

	if err := wsjson.Write(ctx, conn, clientMsg{Type: "resign"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	readUntil(t, ctx, conn, func(m serverMsg) bool {
		return m.Type == "snapshot" && m.Snapshot.Status != game.StatusPlaying
	}, "투료 반영")

	gameID := waitForNewGame(t, st, before)
	waitForResult(t, st, gameID, string(store.ResultLoss))
	t.Cleanup(func() { deleteGame(t, st, gameID) })

	res, err := http.Get(fmt.Sprintf("%s/api/games/%d/summary", srv.URL, gameID))
	if err != nil {
		t.Fatalf("GET summary: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET summary: status = %d, want 200", res.StatusCode)
	}

	var got gameSummaryPayload
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Body == "" {
		t.Error("총평 문장이 비었다 — 라우터가 없어도 결정적 문구가 나가야 한다")
	}
	// 사람이 先手로 한 수 뒀다. 이 수가 안 세어지면 화면의 표가 남의 手数를 그린다.
	if got.Stats.PlayerMoves != 1 {
		t.Errorf("playerMoves = %d, want 1", got.Stats.PlayerMoves)
	}
}

// 王手는 서버가 짚는다. 화면은 규칙을 모르므로, 이 칸이 안 오면 리뷰에서 王手가
// 통째로 안 보인다.
func TestDetailMarksCheck(t *testing.T) {
	rec := recordOf("b", "7g7f", "3c3d", "8h2b+", "3a2b", "B*4b")
	got := detailOf(rec)

	for i, m := range got.Moves[:4] {
		if m.Checked != "" {
			t.Errorf("moves[%d].checked = %q, want empty", i, m.Checked)
		}
	}
	// 角을 4二에 打하면 5一의 玉에 닿는다 — 王手다.
	if got.Moves[4].Checked != "5a" {
		t.Errorf("moves[4].checked = %q, want 5a", got.Moves[4].Checked)
	}
}

// 기보에 구멍이 나도 누구의 수인지는 안 흔들린다. 배열의 자리로 세면 구멍 뒤가
// 한 칸씩 밀려서 리뷰가 남의 실수를 내 것으로 보여준다.
func TestDetailAttributesByPlyNotIndex(t *testing.T) {
	rec := store.GameRecord{
		GameSummary: store.GameSummary{ID: 7, MyColor: "b"},
		Moves: []store.RecordedMove{
			{Ply: 1, USI: "7g7f"},
			{Ply: 3, USI: "2g2f"}, // 2手目가 빠졌다. 그래도 3手目는 사람의 수다
			{Ply: 4, USI: "3c3d"},
		},
	}

	got := detailOf(rec)
	want := []game.Side{game.SideHuman, game.SideHuman, game.SideEngine}
	for i, w := range want {
		if got.Moves[i].By != w {
			t.Errorf("moves[%d](ply %d).by = %q, want %q", i, got.Moves[i].Ply, got.Moves[i].By, w)
		}
	}
}

// 시작 국면을 못 읽으면 한 수도 두지 않는다.
//
// 평수 초기 국면으로 대신 두면 그 수들이 거기서도 합법일 수 있고, 그러면 한 번도 없었던
// 국면을 그럴듯하게 그린다 — 리뷰에서 그건 판을 못 그리는 것보다 나쁘다.
func TestDetailRefusesToReplayFromBrokenStart(t *testing.T) {
	rec := recordOf("b", "7g7f", "3c3d")
	rec.StartSFEN = "not-a-sfen"

	got := detailOf(rec)
	if got.StartSFEN != "" {
		t.Errorf("startSfen = %q, want empty", got.StartSFEN)
	}
	for i, m := range got.Moves {
		if m.SFEN != "" || m.Ja != "" {
			t.Errorf("moves[%d] = %+v, want 국면도 표기도 없음", i, m)
		}
		// 수는 남는다. 둔 것은 둔 것이다.
		if m.USI == "" {
			t.Errorf("moves[%d].usi is empty", i)
		}
	}
}

// limit 은 32비트로 파싱한다. int32 범위를 넘는 수가 통과하면 그 값이 LIMIT의
// int32로 바뀌면서 조용히 음수가 된다 — 거절로 끝나야 한다.
func TestListRejectsOutOfRangeLimit(t *testing.T) {
	h := &reviewHandler{}
	for _, raw := range []string{"0", "-1", "2147483648", "abc"} {
		rec := httptest.NewRecorder()
		h.list(rec, httptest.NewRequest(http.MethodGet, "/api/games?limit="+raw, nil))

		if rec.Code != http.StatusBadRequest {
			t.Errorf("limit=%s: status = %d, want %d", raw, rec.Code, http.StatusBadRequest)
		}
	}
}

// 무른 수는 기보에 없다 — 되짚기가 그 수를 이름으로 부르려면 Ply-1 手目의 국면을
// 다시 만들어야 한다(개입과 같은 규약).
func TestDetailNamesUndoneMove(t *testing.T) {
	cp := 123
	rec := recordOf("b", "7g7f", "3c3d")
	rec.Undos = []store.RecordedUndo{{
		Ply:    3, // 3手目에 뒀다가 무른 수
		USI:    "8h2b+",
		EvalCp: &cp,
	}}

	got := detailOf(rec)
	if len(got.Undos) != 1 {
		t.Fatalf("undos = %d, want 1", len(got.Undos))
	}
	u := got.Undos[0]
	if u.Ja != "▲2二角成" {
		t.Errorf("ja = %q, want ▲2二角成", u.Ja)
	}
	// 기보와 같은 자다. 先手로 둔 판이라 값이 그대로 나가야 한다.
	if u.EvalCp == nil || *u.EvalCp != 123 {
		t.Errorf("evalCp = %v, want 123", u.EvalCp)
	}
}

// 後手로 둔 판에서는 무른 수의 평가치도 플레이어 관점으로 뒤집힌다.
// 기보의 moves[].evalCp 와 같은 변환이라야 한 화면에서 두 줄이 같은 자를 쓴다(§60).
func TestDetailFlipsUndoEvalForWhite(t *testing.T) {
	cp := 200
	rec := recordOf("w", "7g7f")
	rec.Undos = []store.RecordedUndo{{Ply: 2, USI: "3c3d", EvalCp: &cp}}

	got := detailOf(rec)
	if len(got.Undos) != 1 {
		t.Fatalf("undos = %d, want 1", len(got.Undos))
	}
	if got.Undos[0].EvalCp == nil || *got.Undos[0].EvalCp != -200 {
		t.Errorf("evalCp = %v, want -200", got.Undos[0].EvalCp)
	}
}

// 무르기는 개입 횟수에 안 섞인다. 목록의 그 숫자는 「AI가 몇 번 막았나」다.
func TestUndosDoNotCountAsInterventions(t *testing.T) {
	rec := recordOf("b", "7g7f")
	rec.Undos = []store.RecordedUndo{{Ply: 1, USI: "7g7f"}}

	got := detailOf(rec)
	if got.InterventionCount != 0 {
		t.Errorf("interventionCount = %d, want 0", got.InterventionCount)
	}
	if len(got.Interventions) != 0 {
		t.Errorf("interventions = %d, want 0", len(got.Interventions))
	}
}
