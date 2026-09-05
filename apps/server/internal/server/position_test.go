package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/boardread"
)

// 사진에서 국면을 취해 오는 표면은 경계가 넷이다 — 로그인 · 시간당 몫 · 그림이 그림인가 ·
// 룰 엔진이 무엇을 말하는가. 판독 자체는 internal/boardread 가 확인한다.

const startSFEN = "lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1"

// fakePNG 는 앞머리만 맞는 그림이다. 형식은 앞머리로 정한다(boardread.imageMIME).
var fakePNG = append([]byte("\x89PNG\r\n\x1a\n"), []byte("pretend this is a board")...)

// positionTest 는 읽기 창구가 stub 서버를 보는 핸들러다.
func positionTest(t *testing.T, answer string) *positionHandler {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":%q}]}],"usage":{"total_tokens":10}}`, answer)
	}))
	t.Cleanup(srv.Close)

	c := boardread.New("key", "")
	boardread.SetURLForTest(c, srv.URL)
	return &positionHandler{
		auth:   signedInHandler(),
		read:   c,
		budget: newHourlyBudget(maxBoardReadsPerHour),
	}
}

// startGrid 는 평수 초기 국면을 「先手로 앉아 찍은 화면」으로 적은 답이다.
func startGrid() string {
	rows := []string{
		`["l","n","s","g","k","g","s","n","l"]`,
		`[".","r",".",".",".",".",".","b","."]`,
		`["p","p","p","p","p","p","p","p","p"]`,
		`[".",".",".",".",".",".",".",".","."]`,
		`[".",".",".",".",".",".",".",".","."]`,
		`[".",".",".",".",".",".",".",".","."]`,
		`["P","P","P","P","P","P","P","P","P"]`,
		`[".","B",".",".",".",".",".","R","."]`,
		`["L","N","S","G","K","G","S","N","L"]`,
	}
	const zero = `{"P":0,"L":0,"N":0,"S":0,"G":0,"B":0,"R":0}`
	return fmt.Sprintf(`{"found":true,"rows":[%s],"nearHand":%s,"farHand":%s}`,
		strings.Join(rows, ","), zero, zero)
}

// signIn 은 요청에 세션 쿠키를 붙인다.
func signIn(t *testing.T, h *positionHandler, r *http.Request, userID int64) {
	t.Helper()
	value, err := h.auth.codec.Encode(userID, "さとし", time.Now())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: value})
}

func (h *positionHandler) postRead(t *testing.T, userID int64, image []byte) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(positionReadRequest{Image: base64.StdEncoding.EncodeToString(image)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/position/read", strings.NewReader(string(body)))
	if userID > 0 {
		signIn(t, h, r, userID)
	}
	rec := httptest.NewRecorder()
	h.readImage(rec, r)
	return rec
}

func postCheck(t *testing.T, sfen string) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(positionCheckRequest{SFEN: sfen})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/position/check", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	(&positionHandler{}).check(rec, r)
	return rec
}

func decodePosition(t *testing.T, rec *httptest.ResponseRecorder) positionResponse {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var res positionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v — body = %s", err, rec.Body.String())
	}
	return res
}

func TestReadTurnsAnImageIntoAPosition(t *testing.T) {
	h := positionTest(t, startGrid())

	res := decodePosition(t, h.postRead(t, 7, fakePNG))

	if res.SFEN != startSFEN {
		t.Errorf("sfen = %q, want %q", res.SFEN, startSFEN)
	}
	if len(res.Faults) != 0 {
		t.Errorf("faults = %v, want none for a real position", res.Faults)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v, want none for a full set", res.Warnings)
	}
}

// 지키는 것이 돈이라 사람마다 세야 하고, 익명끼리는 구별할 수단이 없다.
func TestReadNeedsASignIn(t *testing.T) {
	h := positionTest(t, startGrid())

	rec := h.postRead(t, 0, fakePNG)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 — body = %s", rec.Code, rec.Body.String())
	}
}

// 벽이 부르기 전에 선다. 부른 뒤에 세면 시한에 걸린 호출이 몫을 안 쓰는데, 그 실패가
// 가장 비싼 호출이다.
func TestReadStopsAtTheHourlyWall(t *testing.T) {
	h := positionTest(t, startGrid())

	for i := range maxBoardReadsPerHour {
		if rec := h.postRead(t, 7, fakePNG); rec.Code != http.StatusOK {
			t.Fatalf("read %d: status = %d, body = %s", i+1, rec.Code, rec.Body.String())
		}
	}
	rec := h.postRead(t, 7, fakePNG)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 — body = %s", rec.Code, rec.Body.String())
	}

	// 몫은 사람마다다. 남의 몫이 내 벽이 되면 안 된다.
	if rec := h.postRead(t, 8, fakePNG); rec.Code != http.StatusOK {
		t.Fatalf("another person: status = %d, want 200", rec.Code)
	}
}

// 클라이언트가 말한 형식을 안 믿는다. 앞머리가 아는 셋이 아니면 저쪽 API 로 안 나간다.
func TestReadRefusesWhatIsNotAnImage(t *testing.T) {
	h := positionTest(t, startGrid())

	rec := h.postRead(t, 7, []byte("<html>not a screenshot</html>"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not_image") {
		t.Errorf("body = %s, want the not_image code", rec.Body.String())
	}
}

// 판이 없는 그림은 고장이 아니라 사실이다. 화면이 「다시 눌러 보라」가 아니라 「판이
// 보이는 그림을 올려라」를 말해야 한다.
func TestReadSaysWhenThereIsNoBoard(t *testing.T) {
	h := positionTest(t, `{"found":false,"rows":[],"nearHand":{"P":0,"L":0,"N":0,"S":0,"G":0,"B":0,"R":0},"farHand":{"P":0,"L":0,"N":0,"S":0,"G":0,"B":0,"R":0}}`)

	rec := h.postRead(t, 7, fakePNG)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 — body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no_board") {
		t.Errorf("body = %s, want the no_board code", rec.Body.String())
	}
}

// 키가 없으면 이 뿌리만 안 열린다. 검사와 검토는 그대로 돈다.
func TestReadWithoutAKeyIsUnavailable(t *testing.T) {
	rec := httptest.NewRecorder()
	boardReadUnavailable(rec, httptest.NewRequest(http.MethodPost, "/api/position/read", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// 검사가 엔진도 로그인도 안 쓴다. 확인 화면이 한 칸을 고칠 때마다 부르는 자리다.
func TestCheckAnswersWithoutASignIn(t *testing.T) {
	res := decodePosition(t, postCheck(t, startSFEN))

	if res.SFEN != startSFEN {
		t.Errorf("sfen = %q, want it echoed back", res.SFEN)
	}
	if len(res.Faults) != 0 || len(res.Warnings) != 0 {
		t.Errorf("faults = %v, warnings = %v, want none", res.Faults, res.Warnings)
	}
}

// 사유가 칸을 든다. 안 주면 사람이 81칸에서 二歩를 눈으로 찾아야 한다.
func TestCheckPointsAtTheSquare(t *testing.T) {
	res := decodePosition(t, postCheck(t, "4k4/9/9/9/4P4/9/4P4/9/4K4 b - 1"))

	if len(res.Faults) != 1 {
		t.Fatalf("faults = %v, want one nifu", res.Faults)
	}
	f := res.Faults[0]
	if f.Reason != "nifu" {
		t.Errorf("reason = %q, want nifu", f.Reason)
	}
	if f.Square == nil {
		t.Fatal("nifu has no square — the screen cannot mark it")
	}
	// 4五(row 4, col 4)와 4七(row 6, col 4)의 歩 둘 중 아래쪽이 둘째 장이다.
	if *f.Square != 6*9+4 {
		t.Errorf("square = %d, want %d", *f.Square, 6*9+4)
	}
	if f.Message == "" {
		t.Error("the fault has no Japanese message — the screen does not write sentences")
	}
}

// 手番을 잘못 고른 자리가 여기로 온다. 사진은 手番을 말해 주지 않는다.
func TestCheckCatchesTheWrongTurn(t *testing.T) {
	res := decodePosition(t, postCheck(t, "4k4/9/9/9/4R4/9/9/9/3K5 b - 1"))

	if len(res.Faults) != 1 || res.Faults[0].Reason != "check ignored" {
		t.Fatalf("faults = %v, want a check-ignored fault", res.Faults)
	}
	// 같은 판에 手番만 바꾸면 성립한다. 그것이 이 사유가 말하는 전부다.
	ok := decodePosition(t, postCheck(t, "4k4/9/9/9/4R4/9/9/9/3K5 w - 1"))
	if len(ok.Faults) != 0 {
		t.Fatalf("faults = %v, want none once the turn is the other side", ok.Faults)
	}
}

// 말이 모자란 것은 거절이 아니다. 駒台가 잘려 나간 사진도 정상이라 경고로만 나간다.
func TestCheckWarnsAboutMissingPieces(t *testing.T) {
	res := decodePosition(t, postCheck(t, "4k4/9/9/9/9/9/9/9/4K4 b - 1"))

	if len(res.Faults) != 0 {
		t.Fatalf("faults = %v, want none — a short set is not illegal", res.Faults)
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("warnings = %v, want one about the missing pieces", res.Warnings)
	}
	if !strings.Contains(res.Warnings[0], "38") {
		t.Errorf("warning = %q, want it to count the 38 missing pieces", res.Warnings[0])
	}
}

// 이미 끝난 국면은 분석할 것이 없다. 사유가 없을 때만 묻는다 — 성립하지 않는 판의
// 합법수는 물어봐야 뜻이 없다.
func TestCheckWarnsWhenThePositionIsAlreadyOver(t *testing.T) {
	// 頭金. 5二의 金이 玉의 도망 칸 다섯을 다 덮고, 그 金을 5九의 香가 받치고 있다.
	res := decodePosition(t, postCheck(t, "4k4/4G4/9/9/9/9/9/9/4L3K w - 1"))

	if len(res.Faults) != 0 {
		t.Fatalf("faults = %v, want none", res.Faults)
	}
	if len(res.Warnings) == 0 || !strings.Contains(strings.Join(res.Warnings, " "), "詰んで") {
		t.Fatalf("warnings = %v, want one saying the game is already over", res.Warnings)
	}
}

func TestCheckRefusesWhatIsNotAPosition(t *testing.T) {
	rec := postCheck(t, "not a position")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — body = %s", rec.Code, rec.Body.String())
	}
}

// 브라우저는 data: URL 을 준다. 화면이 앞머리를 떼는 코드를 갖는 것보다 여기서 떼는
// 편이 낫다.
func TestDecodeImageAcceptsADataURL(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString(fakePNG)
	for name, in := range map[string]string{
		"data URL": "data:image/png;base64," + raw,
		"base64만":  raw,
		// 줄바꿈이 섞여 오는 base64 가 있다.
		"줄바꿈이 섞였다": raw[:8] + "\n" + raw[8:],
	} {
		t.Run(name, func(t *testing.T) {
			got, err := decodeImage(in)
			if err != nil {
				t.Fatalf("decodeImage: %v", err)
			}
			if string(got) != string(fakePNG) {
				t.Errorf("decodeImage gave %d bytes, want %d", len(got), len(fakePNG))
			}
		})
	}

	if _, err := decodeImage(""); err == nil {
		t.Error("decodeImage(\"\") = nil error, want a refusal")
	}
	if _, err := decodeImage("data:image/png;base64"); err == nil {
		t.Error("a data url with no comma should be refused")
	}
}

// 세션이 없는 요청은 UserID 를 안 든다. 벽이 사람을 세는 자리라 그 값이 0이면 익명
// 전체가 한 몫을 나눠 쓰게 되고, 그래서 로그인 벽이 이 벽의 조건이다.
func TestReadDoesNotCountAnonymousRequests(t *testing.T) {
	h := positionTest(t, startGrid())

	for range maxBoardReadsPerHour + 1 {
		if rec := h.postRead(t, 0, fakePNG); rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	}
	// 익명 요청이 몫을 한 개도 안 썼어야 한다.
	if rec := h.postRead(t, 7, fakePNG); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — anonymous requests must not spend a quota", rec.Code)
	}
}

// 판독을 재는 그림을 모으는 자리(apps/server/README.md). 폴더가 켜져 있을 때만 서고,
// 이름을 서버가 지으므로 화면이 준 글자가 경로가 되지 않는다.

// labelTest 는 그림을 모으는 핸들러다. 폴더는 그 시험의 것이라 남는 것이 없다.
func labelTest(t *testing.T) (*positionHandler, string) {
	t.Helper()
	dir := t.TempDir()
	h := positionTest(t, startGrid())
	h.keep = dir
	return h, dir
}

func (h *positionHandler) postLabel(t *testing.T, userID int64, id, sfen string) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(positionLabelRequest{ImageID: id, SFEN: sfen})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/position/label", strings.NewReader(string(body)))
	if userID > 0 {
		signIn(t, h, r, userID)
	}
	rec := httptest.NewRecorder()
	h.label(rec, r)
	return rec
}

// 「올리고 · 고치고 · 누르고」 세 걸음이 그림과 정답의 짝을 하나 남긴다.
func TestReadKeepsTheImageAndLabelPutsTheAnswerBesideIt(t *testing.T) {
	h, dir := labelTest(t)

	res := decodePosition(t, h.postRead(t, 7, fakePNG))
	if res.ImageID != "board-01" {
		t.Fatalf("imageId = %q, want board-01", res.ImageID)
	}
	if _, err := os.Stat(filepath.Join(dir, "board-01.png")); err != nil {
		t.Fatalf("the image was not kept: %v", err)
	}

	if rec := h.postLabel(t, 7, res.ImageID, startSFEN); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(dir, "board-01.sfen"))
	if err != nil {
		t.Fatalf("the label was not written: %v", err)
	}
	if strings.TrimSpace(string(got)) != startSFEN {
		t.Fatalf("label = %q, want %q", strings.TrimSpace(string(got)), startSFEN)
	}
}

// 번호는 있는 것 중 가장 큰 값 다음이다. 개수를 세면 중간을 지웠을 때 남의 그림을 덮는다.
func TestKeptNumbersDoNotReuseAName(t *testing.T) {
	h, dir := labelTest(t)

	for _, want := range []string{"board-01", "board-02", "board-03"} {
		if got := decodePosition(t, h.postRead(t, 7, fakePNG)).ImageID; got != want {
			t.Fatalf("imageId = %q, want %q", got, want)
		}
	}
	// 가운데를 지운다. 개수를 세는 구현이면 여기서 board-03 을 다시 지어 덮는다.
	if err := os.Remove(filepath.Join(dir, "board-02.png")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got := decodePosition(t, h.postRead(t, 7, fakePNG)).ImageID; got != "board-04" {
		t.Fatalf("imageId = %q, want board-04", got)
	}
}

// 이 값이 파일 경로가 되므로 모양 검사가 유일한 방어다.
func TestLabelRefusesANameItDidNotMake(t *testing.T) {
	h, dir := labelTest(t)

	for _, bad := range []string{"../../etc/passwd", "board-01/../x", "board", "board-1", "", "board-99999"} {
		if rec := h.postLabel(t, 7, bad, startSFEN); rec.Code != http.StatusBadRequest {
			t.Errorf("imageId %q: status = %d, want 400", bad, rec.Code)
		}
	}
	// 폴더 밖에도 안에도 아무것도 안 생겼어야 한다.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("the refused names left %d files behind", len(entries))
	}
}

// 틀린 라벨은 없는 라벨보다 나쁘다 — 측정이 조용히 나빠 보이고 원인을 모델에서 찾게 된다.
func TestLabelRefusesAPositionThatCannotStand(t *testing.T) {
	h, dir := labelTest(t)
	id := decodePosition(t, h.postRead(t, 7, fakePNG)).ImageID

	for name, sfen := range map[string]string{
		"二歩":       "4k4/9/9/9/4P4/9/4P4/9/4K4 b - 1",
		"SFEN이 아님": "not a position",
	} {
		t.Run(name, func(t *testing.T) {
			if rec := h.postLabel(t, 7, id, sfen); rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if _, err := os.Stat(filepath.Join(dir, id+".sfen")); err == nil {
				t.Fatal("an impossible position was written as a label")
			}
		})
	}
}

// 폴더가 안 켜져 있으면 그림도 안 남고 id 도 안 온다. 프로덕션이 그 자리다.
func TestReadKeepsNothingWhenTheFolderIsOff(t *testing.T) {
	h := positionTest(t, startGrid())

	if got := decodePosition(t, h.postRead(t, 7, fakePNG)).ImageID; got != "" {
		t.Fatalf("imageId = %q, want empty when collecting is off", got)
	}
}

func TestLabelNeedsASignIn(t *testing.T) {
	h, _ := labelTest(t)

	if rec := h.postLabel(t, 0, "board-01", startSFEN); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
