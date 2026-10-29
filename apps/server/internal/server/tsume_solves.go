package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 사람이 푼 詰め将棋의 기록. 목록·이름 고치기·삭제 셋이다(journal §155).
//
// 남기는 쪽은 여기가 아니다. 풀기(tsume.go 의 record)가 풀 때마다 남긴다. 다시 열기도
// 화면이 같은 국면으로 풀기를 다시 부르는 것이고, 그 요청이 캐시에서 답한다.

// tsumeSolveView 는 목록 한 줄이다.
type tsumeSolveView struct {
	ID int64 `json:"id"`
	// Name 은 사람이 붙인 이름이다. 비어 있으면 화면이 결과를 이름 자리에 쓴다.
	Name string `json:"name"`
	// SFEN 은 풀기에 그대로 보낼 수 있는 국면이다. 手数는 늘 1이다.
	SFEN   string `json:"sfen"`
	Status string `json:"status"`
	// Plies 는 mate 일 때만 온다.
	Plies int `json:"plies,omitempty"`
	// Shortest 는 Plies 보다 짧은 詰み가 없음을 확인했는가다. 화면이 「N手詰め」로 단정할지를 가른다.
	Shortest bool      `json:"shortest"`
	SolvedAt time.Time `json:"solvedAt"`
}

type tsumeSolveHandler struct {
	store *store.Store
	auth  *authHandler
}

// list 는 그 사람의 기록 전부다. 최근에 푼 것이 앞이다. 빈 목록도 200이다.
func (h *tsumeSolveHandler) list(w http.ResponseWriter, r *http.Request) {
	s, ok := h.auth.viewer(r)
	if !ok {
		tsumeSolveLoginRequired(w)
		return
	}
	rows, err := h.store.TsumeSolves(r.Context(), s.UserID)
	if err != nil {
		log.Printf("tsume solves: list for %d: %v", s.UserID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "解いた詰将棋を読み込めませんでした。",
		})
		return
	}
	out := make([]tsumeSolveView, 0, len(rows))
	for _, row := range rows {
		out = append(out, tsumeSolveView{
			ID:       row.ID,
			Name:     row.Name,
			SFEN:     row.SFENKey + " 1",
			Status:   row.Status,
			Plies:    row.Plies,
			Shortest: row.Shortest,
			SolvedAt: row.SolvedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"solves": out})
}

// rename 은 이름만 고친다. 빈 이름도 받는다. 그때는 화면이 결과를 이름 자리에 쓴다.
func (h *tsumeSolveHandler) rename(w http.ResponseWriter, r *http.Request) {
	s, ok := h.auth.viewer(r)
	if !ok {
		tsumeSolveLoginRequired(w)
		return
	}
	id, ok := tsumeSolveID(w, r)
	if !ok {
		return
	}
	var req exploreSnapshotRename
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, exploreSnapshotBodyLimit)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_request", "message": "リクエストを読み取れませんでした。",
		})
		return
	}
	// 이름 규칙은 검토의 저장 국면과 같다. 두 목록의 이름 칸이 다른 상한을 가질 이유가 없다.
	name, ok := exploreSnapshotName(req.Name)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "bad_name",
			"message": fmt.Sprintf("名前は%d文字までにしてください。", exploreSnapshotNameMax),
		})
		return
	}
	err := h.store.RenameTsumeSolve(r.Context(), id, s.UserID, name)
	if errors.Is(err, store.ErrNoTsumeSolve) {
		tsumeSolveNotFound(w)
		return
	}
	if err != nil {
		log.Printf("tsume solves: rename %d for %d: %v", id, s.UserID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "名前を変更できませんでした。",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "name": name})
}

// remove 는 기록 하나를 지운다. 국면의 답(tsume_results)은 사람의 것이 아니라 남는다.
func (h *tsumeSolveHandler) remove(w http.ResponseWriter, r *http.Request) {
	s, ok := h.auth.viewer(r)
	if !ok {
		tsumeSolveLoginRequired(w)
		return
	}
	id, ok := tsumeSolveID(w, r)
	if !ok {
		return
	}
	err := h.store.DeleteTsumeSolve(r.Context(), id, s.UserID)
	if errors.Is(err, store.ErrNoTsumeSolve) {
		tsumeSolveNotFound(w)
		return
	}
	if err != nil {
		log.Printf("tsume solves: delete %d for %d: %v", id, s.UserID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "削除できませんでした。",
		})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func tsumeSolveLoginRequired(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnauthorized, map[string]any{
		"error": "unauthorized", "message": "ログインが必要です。",
	})
}

// tsumeSolveNotFound 는 없는 기록과 남의 기록에 같은 답을 준다(exploreSnapshotHandler.notFound).
func tsumeSolveNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]any{
		"error": "not_found", "message": "その詰将棋は見つかりません。",
	})
}

// tsumeSolveID 는 주소의 번호를 읽는다. 읽지 못하면 이미 답을 썼다.
func tsumeSolveID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_id", "message": "番号が正しくありません。",
		})
		return 0, false
	}
	return id, true
}
