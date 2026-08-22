package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 저장은 벽 셋과 왕복 하나다. 벽은 DB 없이 확인한다 — 로그인·이름·수순 셋이 다 기록에
// 닿기 전에 거절되므로, 그 셋이 CI에서도 도는 것이 여기의 요점이다.

// snapshotWalls 는 store 없는 핸들러다. 벽에서 거절되는 요청은 기록에 안 닿으므로,
// 여기서 500이 나오면 그 요청이 벽을 지나갔다는 뜻이다.
func snapshotWalls(t *testing.T) (*exploreSnapshotHandler, *http.Cookie) {
	t.Helper()

	ah := signedInHandler()
	value, err := ah.codec.Encode(7, "さとし", time.Now())
	if err != nil {
		t.Fatalf("encode session: %v", err)
	}
	return &exploreSnapshotHandler{store: nil, auth: ah}, &http.Cookie{Name: sessionCookie, Value: value}
}

// snapshotStore 는 진짜 DB에 붙은 핸들러와 서로 다른 두 사람이다.
//
// 왕복과 주인 검사가 SQL 의 WHERE 절에만 있다(query/explore.sql) — 가짜로는 확인할 수 없다.
//
//	SHOWGI_TEST_DATABASE_URL=postgres://showgi:showgi@localhost:5432/showgi go test ./internal/server/
func snapshotStore(t *testing.T) (*exploreSnapshotHandler, *http.Cookie, *http.Cookie) {
	t.Helper()

	url := os.Getenv("SHOWGI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SHOWGI_TEST_DATABASE_URL 미설정 — DB 테스트 건너뜀")
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)

	// 실행마다 다른 사람이어야 한다. 남은 행을 물려받으면 두 번째 실행부터 목록이 길다.
	stamp := t.Name() + "-" + time.Now().Format("150405.000000000")
	ah := signedInHandler()
	cookies := make([]*http.Cookie, 0, 2)
	for _, who := range []string{"a", "b"} {
		id, err := st.UpsertUser(t.Context(), "test", stamp+"-"+who, "テスト")
		if err != nil {
			t.Fatalf("upsert %s: %v", who, err)
		}
		value, err := ah.codec.Encode(id, "テスト", time.Now())
		if err != nil {
			t.Fatalf("encode %s: %v", who, err)
		}
		cookies = append(cookies, &http.Cookie{Name: sessionCookie, Value: value})
	}
	return &exploreSnapshotHandler{store: st, auth: ah}, cookies[0], cookies[1]
}

// call 은 요청 하나를 건다. c 가 nil이면 로그인 안 한 요청이다.
func (h *exploreSnapshotHandler) call(
	t *testing.T,
	method, path, body string,
	c *http.Cookie,
) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	if c != nil {
		r.AddCookie(c)
	}
	// PathValue 는 라우터가 채운다. 핸들러를 직접 부르므로 여기서 손으로 넣는다.
	if i := strings.LastIndex(path, "/"); strings.HasPrefix(path, "/api/explore/snapshots/") {
		r.SetPathValue("id", path[i+1:])
	}
	rec := httptest.NewRecorder()
	switch method {
	case http.MethodGet:
		h.list(rec, r)
	case http.MethodPost:
		h.save(rec, r)
	case http.MethodPatch:
		h.rename(rec, r)
	case http.MethodDelete:
		h.remove(rec, r)
	default:
		t.Fatalf("모르는 메서드 %s", method)
	}
	return rec
}

func decodeSnapshot(t *testing.T, rec *httptest.ResponseRecorder, want int) exploreSnapshotView {
	t.Helper()

	if rec.Code != want {
		t.Fatalf("status = %d, want %d — body = %s", rec.Code, want, rec.Body.String())
	}
	var out exploreSnapshotView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v — body = %s", err, rec.Body.String())
	}
	return out
}

func decodeSnapshotList(t *testing.T, rec *httptest.ResponseRecorder) []exploreSnapshotView {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — body = %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Snapshots []exploreSnapshotView `json:"snapshots"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v — body = %s", err, rec.Body.String())
	}
	return out.Snapshots
}

// 로그인 벽. 검토 자체와 같은 자리이고(explore.go) 같은 문구여야 한다 — 한 화면의 두
// 자리가 다른 말로 거절하면 사람은 그 둘을 다른 일로 읽는다.
func TestSnapshotsNeedLogin(t *testing.T) {
	h, _ := snapshotWalls(t)

	for _, c := range []struct {
		method, path, body string
	}{
		{http.MethodGet, "/api/explore/snapshots", ""},
		{http.MethodPost, "/api/explore/snapshots", `{"name":"a","handicap":"","moves":[]}`},
		{http.MethodPatch, "/api/explore/snapshots/1", `{"name":"a"}`},
		{http.MethodDelete, "/api/explore/snapshots/1", ""},
	} {
		rec := h.call(t, c.method, c.path, c.body, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401", c.method, c.path, rec.Code)
		}
		if got := rec.Body.String(); !strings.Contains(got, whatifMessages["login_required"]) {
			t.Errorf("%s %s: 문구가 검토와 다르다 — %s", c.method, c.path, got)
		}
	}
}

// 둘 수 없는 수는 저장되지 않는다. 저장할 때 막지 않으면 불러올 때마다 거절되는 행이
// 기록에 남고, 그건 화면에서 「고장난 국면」으로 보인다.
//
// store 가 nil이라 벽을 지나가면 500이 아니라 패닉이다 — 지나가지 않는 것이 이 테스트다.
func TestSnapshotsRejectAnIllegalLine(t *testing.T) {
	h, who := snapshotWalls(t)

	for _, c := range []struct {
		name, body, want string
	}{
		// 先手의 첫 수로 後手의 수를 뒀다. 모양은 USI 그대로라 정규식으로는 안 걸린다.
		{"手番이 아닌 수", `{"handicap":"","moves":["3c3d"]}`, "bad_move"},
		// 二歩. 룰 엔진만 아는 거절이다.
		{"二歩", `{"handicap":"","moves":["2g2f","3c3d","P*2e"]}`, "bad_move"},
		{"없는 手合割", `{"handicap":"hachimaiochi","moves":[]}`, "bad_handicap"},
		{"이름이 너무 길다", fmt.Sprintf(`{"name":%q,"moves":[]}`, strings.Repeat("あ", exploreSnapshotNameMax+1)), "bad_name"},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := h.call(t, http.MethodPost, "/api/explore/snapshots", c.body, who)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 — body = %s", rec.Code, rec.Body.String())
			}
			if got := rec.Body.String(); !strings.Contains(got, c.want) {
				t.Errorf("사유가 %s 가 아니다 — %s", c.want, got)
			}
		})
	}
}

// 저장한 자리가 그대로 돌아온다. 手合割 이름은 표에서 붙고(internal/handicap) 수순은
// 보낸 그대로다 — 화면이 그 둘로 주소를 다시 만든다.
func TestSnapshotRoundTrip(t *testing.T) {
	h, who, _ := snapshotStore(t)

	// 二枚落ち는 上手가 먼저 둔다(journal §88). 下手의 수를 앞에 두면 저장이 거절된다 —
	// 되짚어 보는 검사가 실제로 手番을 보고 있다는 뜻이다.
	line := `["3c3d","7g7f","8c8d"]`
	saved := decodeSnapshot(t, h.call(t, http.MethodPost, "/api/explore/snapshots",
		`{"name":"  矢倉の入り口  ","handicap":"nimaiochi","moves":`+line+`}`, who), http.StatusCreated)

	// 양끝 공백을 지운다. 안 지우면 목록에 빈 것처럼 보이는 줄이 선다.
	if saved.Name != "矢倉の入り口" {
		t.Errorf("name = %q", saved.Name)
	}
	if saved.Handicap != "nimaiochi" || saved.HandicapJa != "二枚落ち" {
		t.Errorf("手合割 = %q / %q", saved.Handicap, saved.HandicapJa)
	}
	if len(saved.Moves) != 3 || saved.Moves[0] != "3c3d" {
		t.Errorf("moves = %v", saved.Moves)
	}

	got := decodeSnapshotList(t, h.call(t, http.MethodGet, "/api/explore/snapshots", "", who))
	if len(got) != 1 || got[0].ID != saved.ID {
		t.Fatalf("목록이 %+v", got)
	}
	if len(got[0].Moves) != 3 {
		t.Errorf("목록의 수순이 %v", got[0].Moves)
	}

	// 이름만 고친다. 국면이 따라 움직이면 옛 이름이 가리키던 자리가 조용히 달라진다.
	if rec := h.call(t, http.MethodPatch, fmt.Sprintf("/api/explore/snapshots/%d", saved.ID),
		`{"name":"矢倉"}`, who); rec.Code != http.StatusOK {
		t.Fatalf("rename: status = %d — %s", rec.Code, rec.Body.String())
	}
	got = decodeSnapshotList(t, h.call(t, http.MethodGet, "/api/explore/snapshots", "", who))
	if len(got) != 1 || got[0].Name != "矢倉" || len(got[0].Moves) != 3 {
		t.Fatalf("이름을 고친 뒤 %+v", got)
	}

	if rec := h.call(t, http.MethodDelete, fmt.Sprintf("/api/explore/snapshots/%d", saved.ID), "", who); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d — %s", rec.Code, rec.Body.String())
	}
	if got := decodeSnapshotList(t, h.call(t, http.MethodGet, "/api/explore/snapshots", "", who)); len(got) != 0 {
		t.Fatalf("지운 뒤에도 %+v", got)
	}
}

// 0手目도 저장할 수 있고, 이름을 비우면 서버가 짓는다. 그 둘이 같은 요청에서 만난다 —
// text[] 컬럼에 빈 배열이 가야 하고(store.SaveExploreSnapshot) 이름이 비면 안 된다.
func TestSnapshotAtTheStartGetsAName(t *testing.T) {
	h, who, _ := snapshotStore(t)

	saved := decodeSnapshot(t, h.call(t, http.MethodPost, "/api/explore/snapshots",
		`{"name":"   ","handicap":"","moves":[]}`, who), http.StatusCreated)

	if saved.Name != "0手目の局面" {
		t.Errorf("name = %q", saved.Name)
	}
	if saved.Moves == nil || len(saved.Moves) != 0 {
		t.Errorf("moves = %v, want 빈 배열", saved.Moves)
	}
	if saved.Handicap != "" || saved.HandicapJa != "" {
		t.Errorf("平手인데 手合割이 %q / %q", saved.Handicap, saved.HandicapJa)
	}
}

// 남의 국면은 목록에도 없고 고치거나 지울 수도 없다. 없는 것과 같은 404다 — 갈라 주면
// 남이 몇 개 저장했는지를 번호로 훑어볼 수 있다.
func TestSnapshotsAreNotSharedBetweenPeople(t *testing.T) {
	h, mine, theirs := snapshotStore(t)

	saved := decodeSnapshot(t, h.call(t, http.MethodPost, "/api/explore/snapshots",
		`{"name":"わたしの局面","handicap":"","moves":["7g7f"]}`, mine), http.StatusCreated)

	if got := decodeSnapshotList(t, h.call(t, http.MethodGet, "/api/explore/snapshots", "", theirs)); len(got) != 0 {
		t.Fatalf("남의 목록에 %+v", got)
	}

	path := fmt.Sprintf("/api/explore/snapshots/%d", saved.ID)
	if rec := h.call(t, http.MethodPatch, path, `{"name":"うわがき"}`, theirs); rec.Code != http.StatusNotFound {
		t.Errorf("남의 이름 고치기: status = %d, want 404", rec.Code)
	}
	if rec := h.call(t, http.MethodDelete, path, "", theirs); rec.Code != http.StatusNotFound {
		t.Errorf("남의 국면 지우기: status = %d, want 404", rec.Code)
	}

	// 내 것은 그대로다. 위 둘이 통과했다면 여기서 이름이 바뀌거나 줄이 사라진다.
	got := decodeSnapshotList(t, h.call(t, http.MethodGet, "/api/explore/snapshots", "", mine))
	if len(got) != 1 || got[0].Name != "わたしの局面" {
		t.Fatalf("내 목록이 %+v", got)
	}
}
