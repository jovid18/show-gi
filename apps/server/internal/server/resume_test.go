package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// **기록이 없는 배포에서도 200이다.** 첫 화면이 늘 부르는 자리라 503이면 물음 카드가
// 뜰 자리에 오류가 뜬다 — `/api/me` 와 같은 판단이다(docs/06-status.md §46).
func TestResumableWithoutStoreSaysThereIsNone(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/resumable", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Game *json.RawMessage `json:"game"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("본문: %v (%s)", err, rec.Body)
	}
	if body.Game != nil {
		t.Errorf("game = %s, want null", *body.Game)
	}
}

// 기보에 구멍이 있으면 이어하지 않는다. 기록은 큐가 넘치면 이벤트를 버리므로
// (recorder.go) 이런 판이 실제로 나온다.
func TestResumeMovesRejectsAGapInPlies(t *testing.T) {
	ok := store.GameRecord{
		GameSummary: store.GameSummary{ID: 7},
		Moves: []store.RecordedMove{
			{Ply: 1, USI: "7g7f"}, {Ply: 2, USI: "3c3d"}, {Ply: 3, USI: "2g2f"},
		},
	}
	got, err := resumeMoves(ok)
	if err != nil {
		t.Fatalf("resumeMoves: %v", err)
	}
	if len(got) != 3 || got[2] != "2g2f" {
		t.Errorf("moves = %v", got)
	}

	// 2手目가 빠졌다. **여기서 눈감으면 3手目가 2手目 자리에 서서 없던 판이 된다.**
	broken := store.GameRecord{
		GameSummary: store.GameSummary{ID: 7},
		Moves:       []store.RecordedMove{{Ply: 1, USI: "7g7f"}, {Ply: 3, USI: "2g2f"}},
	}
	if _, err := resumeMoves(broken); err == nil {
		t.Error("구멍 난 기보를 그대로 받았다")
	}
}

// 새 판의 뿌리는 Options 의 것이다. 이어하는 판은 그 행의 것을 쓰므로(resumeSetup)
// 가정 수순이 두 경로에서 같은 국면을 본다.
func TestNewSetupCarriesTheStartPosition(t *testing.T) {
	const sfen = "lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1"
	got := newSetup(httptest.NewRequest(http.MethodGet, "/ws/game", nil), Options{StartSFEN: sfen})
	if got.startSFEN != sfen {
		t.Errorf("startSFEN = %q, want %q", got.startSFEN, sfen)
	}
	if got.resumeID != 0 || got.startMoves != nil {
		t.Errorf("새 판인데 이어하기 값이 붙었다: %+v", got)
	}
}
