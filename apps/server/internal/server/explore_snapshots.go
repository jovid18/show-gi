package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jovid18/show-gi/apps/server/internal/handicap"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 검토 화면에서 이름을 붙여 남긴 국면. 저장·목록·이름 고치기·삭제 넷이고, 정한 것은 journal §96.
//
// 엔진을 안 탄다. 手合割 id 와 수순 한 줄을 기록에 넣고 꺼내는 일뿐이고, 불러오기는 화면이
// 주소를 고쳐 /api/explore 로 다시 묻는다(explore.go).
//
// SFEN 칸을 만들지 않는다. 저장된 값이 곧 다음 요청의 본문이라, 만드는 순간 journal §37 이
// 닫아 둔 문이 이쪽으로 열린다.
//
// 로그인이 필요하다. 검토 자체가 그 벽 뒤에 있고(explore.go), 익명끼리는 구별할 수단이
// 없어서(002_anonymous_games.sql) 「내가 저장한 국면」이 성립하지 않는다.

const (
	// exploreSnapshotNameMax 는 이름의 상한이다. rune 으로 센다 — 바이트로 세면 일본어가
	// 한 자 3바이트라 13자에서 걸린다.
	exploreSnapshotNameMax = 40

	// exploreSnapshotBodyLimit 은 본문 상한이다. 이름 하나와 手合割 id 하나, 수순 한 줄이 전부다.
	exploreSnapshotBodyLimit = 16 << 10
)

// exploreSnapshotHandler 는 저장된 국면을 읽고 쓴다. Searcher 를 안 들고 있는 것이 이 표면이
// 엔진과 무관하다는 뜻이다.
type exploreSnapshotHandler struct {
	store *store.Store
	auth  *authHandler
}

// exploreSnapshotView 는 목록 한 줄이다.
//
// 手数를 안 싣는다. Moves 의 길이가 그 값이라 실으면 두 칸이 어긋날 수 있다.
type exploreSnapshotView struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Handicap 은 手合割 id다. 平手면 안 온다 — 빈 값이 平手라는 규약을 그대로 쓴다.
	Handicap string `json:"handicap,omitempty"`
	// HandicapJa 는 그 手合割의 일본어 이름이다. 平手면 안 온다.
	//
	// 화면이 id로 이름을 만들지 않는다(review.go 의 handicapJa 와 같은 규약).
	HandicapJa string    `json:"handicapJa,omitempty"`
	Moves      []string  `json:"moves"`
	SavedAt    time.Time `json:"savedAt"`
}

// exploreSnapshotRequest 는 저장 요청이다. 검토 화면이 지금 보고 있는 자리 그대로다.
type exploreSnapshotRequest struct {
	// Name 은 사람이 붙인 이름이다. 비면 서버가 짓는다(exploreSnapshotDefaultName).
	Name string `json:"name"`
	// Handicap 은 手合割 id다. 빈 값이 平手다.
	Handicap string `json:"handicap"`
	// Moves 는 0手目부터의 양쪽 수 전부다. 빈 배열이 시작 국면이다.
	Moves []string `json:"moves"`
}

// exploreSnapshotRename 은 이름 고치기 요청이다. 국면은 안 바뀐다.
type exploreSnapshotRename struct {
	Name string `json:"name"`
}

// list 는 그 사람이 저장한 국면 전부다. 최근에 저장한 것이 앞이다.
//
// 빈 목록이 200이다. 화면이 검토를 열 때마다 부르는 자리라, 404면 「아직 하나도 없다」와
// 고장이 같은 그림이 된다.
func (h *exploreSnapshotHandler) list(w http.ResponseWriter, r *http.Request) {
	s, ok := h.auth.viewer(r)
	if !ok {
		h.loginRequired(w)
		return
	}

	rows, err := h.store.ExploreSnapshots(r.Context(), s.UserID)
	if err != nil {
		log.Printf("explore snapshots: list for %d: %v", s.UserID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "保存した局面を読み込めませんでした。",
		})
		return
	}

	out := make([]exploreSnapshotView, 0, len(rows))
	for _, row := range rows {
		out = append(out, exploreSnapshotViewOf(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": out})
}

// save 는 지금 보고 있는 자리를 남긴다.
//
// 수순을 룰 엔진에 되짚어 본다. 합법성 검사뿐이라 엔진 슬롯을 안 잡는다(journal §96) —
// 안 하면 불러올 때마다 거절되는 행이 기록에 남는다.
func (h *exploreSnapshotHandler) save(w http.ResponseWriter, r *http.Request) {
	s, ok := h.auth.viewer(r)
	if !ok {
		h.loginRequired(w)
		return
	}

	var req exploreSnapshotRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, exploreSnapshotBodyLimit)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_request", "message": "リクエストを読み取れませんでした。",
		})
		return
	}

	name, ok := exploreSnapshotName(req.Name)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "bad_name",
			"message": fmt.Sprintf("名前は%d文字までにしてください。", exploreSnapshotNameMax),
		})
		return
	}
	if len(req.Moves) > exploreMaxLine {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_line", "message": whatifMessages["bad_line"],
		})
		return
	}

	// 手合割을 검토 자체와 같은 문으로 확인한다. 어휘가 두 벌이면 새 手合이 붙는 날
	// 저장만 조용히 거절한다.
	root, _, ok := exploreRoot(req.Handicap)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_handicap", "message": "その手合割は選べません。",
		})
		return
	}
	start, err := shogi.ParseSFEN(startSFENOf(root.StartSFEN))
	if err != nil {
		// 手合割 표가 깨진 것이라 사람이 고칠 일이다. 여기까지 아무것도 안 썼으므로
		// 잃는 것은 없다.
		log.Printf("explore snapshots: start sfen %q: %v", root.StartSFEN, err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "局面を保存できませんでした。",
		})
		return
	}
	if _, _, err := replayTo(start, req.Moves, len(req.Moves)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_move", "message": whatifMessages["bad_move"],
		})
		return
	}

	if name == "" {
		name = exploreSnapshotDefaultName(len(req.Moves))
	}

	saved, err := h.store.SaveExploreSnapshot(r.Context(), s.UserID, name, req.Handicap, req.Moves)
	if err != nil {
		log.Printf("explore snapshots: save for %d: %v", s.UserID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "局面を保存できませんでした。",
		})
		return
	}
	writeJSON(w, http.StatusCreated, exploreSnapshotViewOf(saved))
}

// rename 은 이름만 고친다. 수순을 같은 행에 덮어쓰면 옛 이름이 가리키던 자리가 조용히
// 다른 국면이 된다.
func (h *exploreSnapshotHandler) rename(w http.ResponseWriter, r *http.Request) {
	s, ok := h.auth.viewer(r)
	if !ok {
		h.loginRequired(w)
		return
	}
	id, ok := exploreSnapshotID(w, r)
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
	// 빈 이름을 여기서는 안 받는다. 저장과 달리 그 행을 읽지 않으므로 이름을 지을
	// 재료가 없다.
	name, ok := exploreSnapshotName(req.Name)
	if !ok || name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "bad_name",
			"message": fmt.Sprintf("名前は1〜%d文字にしてください。", exploreSnapshotNameMax),
		})
		return
	}

	err := h.store.RenameExploreSnapshot(r.Context(), id, s.UserID, name)
	if errors.Is(err, store.ErrNoSnapshot) {
		h.notFound(w)
		return
	}
	if err != nil {
		log.Printf("explore snapshots: rename %d for %d: %v", id, s.UserID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "名前を変更できませんでした。",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "name": name})
}

// remove 는 저장된 국면 하나를 지운다.
func (h *exploreSnapshotHandler) remove(w http.ResponseWriter, r *http.Request) {
	s, ok := h.auth.viewer(r)
	if !ok {
		h.loginRequired(w)
		return
	}
	id, ok := exploreSnapshotID(w, r)
	if !ok {
		return
	}

	err := h.store.DeleteExploreSnapshot(r.Context(), id, s.UserID)
	if errors.Is(err, store.ErrNoSnapshot) {
		h.notFound(w)
		return
	}
	if err != nil {
		log.Printf("explore snapshots: delete %d for %d: %v", id, s.UserID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "局面を削除できませんでした。",
		})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// loginRequired 는 검토와 같은 문구로 거절한다(whatifMessages). 한 화면의 두 자리가 다른
// 말을 쓰면 같은 일이 다른 일로 읽힌다.
func (h *exploreSnapshotHandler) loginRequired(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnauthorized, map[string]any{
		"error": "login_required", "message": whatifMessages["login_required"],
	})
}

// notFound 는 없는 국면과 남의 국면에 같은 답을 준다. 갈라 주면 남이 몇 개 저장했는지를
// 번호로 훑어볼 수 있다.
func (h *exploreSnapshotHandler) notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]any{
		"error": "not_found", "message": "その局面は見つかりません。",
	})
}

// exploreSnapshotID 는 주소의 번호를 읽는다. 못 읽으면 이미 답을 썼다.
func exploreSnapshotID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_id", "message": "その局面の番号が正しくありません。",
		})
		return 0, false
	}
	return id, true
}

// exploreSnapshotName 은 이름을 다듬는다. 두 번째 값이 false면 너무 길다.
//
// 양끝 공백을 지운다. 안 지우면 공백 하나가 이름인 줄이 목록에 빈 채로 선다.
func exploreSnapshotName(raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if utf8.RuneCountInString(name) > exploreSnapshotNameMax {
		return "", false
	}
	return name, true
}

// exploreSnapshotDefaultName 은 이름 없이 저장했을 때의 이름이다. 화면이 아니라 서버가
// 짓는 이유와 手合割을 안 넣는 이유는 journal §96.
func exploreSnapshotDefaultName(ply int) string {
	return fmt.Sprintf("%d手目の局面", ply)
}

// exploreSnapshotViewOf 는 저장된 행을 화면이 읽는 모양으로 옮긴다. 手合割 이름은 표에서
// 붙는다(internal/handicap).
func exploreSnapshotViewOf(row store.ExploreSnapshot) exploreSnapshotView {
	out := exploreSnapshotView{
		ID:      row.ID,
		Name:    row.Name,
		Moves:   row.Moves,
		SavedAt: row.CreatedAt,
	}
	if out.Moves == nil {
		out.Moves = []string{}
	}
	// 표에서 못 찾아도 id 는 싣는다. 이 칸까지 비우면 그 줄이 平手로 보이고 불러오기도
	// 平手 0手目로 열려, 다른 국면이 같은 이름으로 선다.
	out.Handicap = row.Handicap
	if hc, ok := handicap.Find(row.Handicap); ok {
		out.HandicapJa = hc.Name
	}
	return out
}
