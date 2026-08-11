package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// detailOf 는 DB를 안 탄다. 재현이 이 패키지의 순수 함수라서, 기록을 손으로 만들어
// **엔진도 DB도 없이** 확인할 수 있다 — 여기가 리뷰 화면의 정합성이 걸린 자리다.

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

// 사람이 後手면 짝수 手数가 사람이다. 여기가 뒤집히면 리뷰가 **남의 실수를 내 것으로**
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

// 평가치는 DB에 先手 관점으로 들어 있다. 後手로 둔 판에서 뒤집지 않으면 **잃은 판이
// 이긴 판으로** 보인다.
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

// 평가치가 없는 手数는 **없는 채로** 나가야 한다. 0으로 채우면 호각과 구별이 안 된다.
func TestDetailKeepsMissingEvalMissing(t *testing.T) {
	got := detailOf(recordOf("b", "7g7f"))
	if got.Moves[0].EvalCp != nil {
		t.Errorf("evalCp = %v, want nil", *got.Moves[0].EvalCp)
	}
}

// 手数에 구멍이 있으면 거기서 재현을 멈춘다. 이어서 두면 **없던 국면**을 그린다.
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

// 물러진 수는 **기보에 없다.** `Ply-1` 手目의 국면에서 두어졌고, 리뷰는 거기서
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
	// 이름과 문구는 **서버가** 만든다. 화면이 코드로 문장을 지으면 어휘가 두 벌이 된다.
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

// 中盤에서 시작하는 판. 手数의 홀짝이 아니라 **시작 국면의 手番**이 누가 먼저인지를 정한다.
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

// DB가 없으면 503이다. **엔진과 조건이 갈린다** — 엔진이 죽어도 지난 판은 볼 수 있어야 하고,
// 여기가 404면 "기록이 없다"와 "기록을 못 읽는다"가 섞인다.
func TestReviewWithoutStore(t *testing.T) {
	h := Handler(Options{})
	for _, path := range []string{"/api/games", "/api/games/1"} {
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

// 王手는 **서버가 짚는다.** 화면은 규칙을 모르므로, 이 칸이 안 오면 리뷰에서 王手가
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

// 기보에 구멍이 나도 **누구의 수인지는 안 흔들린다.** 배열의 자리로 세면 구멍 뒤가
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

// 시작 국면을 못 읽으면 **한 수도 두지 않는다.**
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
