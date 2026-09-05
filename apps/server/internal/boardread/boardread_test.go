package boardread

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// png 는 앞머리만 맞는 가짜 그림이다. 형식은 앞머리로 정하므로(imageMIME) 이 여덟 바이트가
// 곧 「png 다」이고, 뒤에 무엇이 붙어도 이 계층은 그림을 안 본다.
var png = append([]byte("\x89PNG\r\n\x1a\n"), []byte("not really a png")...)

// 이 시험이 이 패키지의 요점이다. 그림의 위 줄부터·왼쪽부터가 곧 SFEN 판 칸의 순서라
// 옮기는 코드에 좌표 계산이 없다는 것을, 평수 초기 국면 한 판으로 확인한다.
//
// 격자는 「先手로 앉은 사람이 보는 화면」이다 — 자기 駒(대문자)가 아래 줄에 있다.
func TestReadMapsTheDrawnGridStraightToSFEN(t *testing.T) {
	rows := [][]string{
		far("l", "n", "s", "g", "k", "g", "s", "n", "l"),
		far(".", "r", ".", ".", ".", ".", ".", "b", "."),
		far("p", "p", "p", "p", "p", "p", "p", "p", "p"),
		empty(),
		empty(),
		empty(),
		near("P", "P", "P", "P", "P", "P", "P", "P", "P"),
		near(".", "B", ".", ".", ".", ".", ".", "R", "."),
		near("L", "N", "S", "G", "K", "G", "S", "N", "L"),
	}
	got := mustRead(t, stub(t, read{Found: true, Rows: rows}))

	const want = "lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1"
	if got.SFEN != want {
		t.Fatalf("SFEN =\n%q\nwant\n%q", got.SFEN, want)
	}
	// 룰 엔진이 읽을 수 있어야 쓸 수 있다. 여기서 걸리면 위의 문자열 비교가 맞아도
	// 국면이 안 선다.
	if _, err := shogi.ParseSFEN(got.SFEN); err != nil {
		t.Fatalf("ParseSFEN(%q): %v", got.SFEN, err)
	}
}

// 手番은 언제나 "b" 다. 사진이 말해 주지 않는 값이라 이 계층은 모르고, 사람이 고른 값이
// 그 자리를 덮는다.
func TestReadAlwaysSaysBlackToMove(t *testing.T) {
	got := mustRead(t, stub(t, read{Found: true, Rows: onlyKings()}))
	if fields := strings.Fields(got.SFEN); fields[1] != "b" {
		t.Fatalf("turn = %q, want b", fields[1])
	}
}

// 아래쪽 駒台가 대문자다. 순서는 관례대로 飛角金銀桂香歩이고, 1장은 개수를 안 적는다.
func TestReadWritesBothHands(t *testing.T) {
	got := mustRead(t, stub(t, read{
		Found:    true,
		Rows:     onlyKings(),
		NearHand: hand{R: 1, G: 2, P: 3},
		FarHand:  hand{B: 1, L: 4},
	}))
	if fields := strings.Fields(got.SFEN); fields[2] != "R2G3Pb4l" {
		t.Fatalf("hands = %q, want R2G3Pb4l", fields[2])
	}
	if _, err := shogi.ParseSFEN(got.SFEN); err != nil {
		t.Fatalf("ParseSFEN(%q): %v", got.SFEN, err)
	}
}

func TestReadWritesADashForEmptyHands(t *testing.T) {
	got := mustRead(t, stub(t, read{Found: true, Rows: onlyKings()}))
	if fields := strings.Fields(got.SFEN); fields[2] != "-" {
		t.Fatalf("hands = %q, want -", fields[2])
	}
}

// 종류마다 한 벌의 수로 깎지 않는다. 넘치는 것은 국면에 실려 나가 룰 엔진이 짚어 주고
// (shogi.Faults), 여기서 막는 것은 int8 이 넘치는 값뿐이다.
func TestReadKeepsTooManyPiecesButStaysWithinInt8(t *testing.T) {
	got := mustRead(t, stub(t, read{
		Found: true, Rows: onlyKings(), NearHand: hand{P: 19}, FarHand: hand{S: 900},
	}))
	fields := strings.Fields(got.SFEN)
	if fields[2] != "19P40s" {
		t.Fatalf("hands = %q, want 19P40s", fields[2])
	}
	pos, err := shogi.ParseSFEN(got.SFEN)
	if err != nil {
		t.Fatalf("ParseSFEN(%q): %v", got.SFEN, err)
	}
	// 19장은 룰 엔진이 잡는다. 그것이 이 값을 안 깎는 이유다.
	if pos.InventoryExcess()[shogi.Pawn] == 0 {
		t.Fatal("19 pawns should be an excess the rule engine reports")
	}
}

// 판이 없는 그림은 고장이 아니라 사실이다. 사유를 갈라 두면 화면이 「다시 눌러 보라」가
// 아니라 「판이 보이는 그림을 올려라」를 말할 수 있다.
func TestReadRefusesAnImageWithNoBoard(t *testing.T) {
	c := stub(t, read{Found: false})
	if _, err := c.Read(context.Background(), png); !errors.Is(err, ErrNoBoard) {
		t.Fatalf("Read() error = %v, want ErrNoBoard", err)
	}
}

// 반쯤 읽은 판을 쓰면 없는 국면 위에서 형세와 최선수가 돈다.
func TestReadRefusesAGridThatIsNotNine(t *testing.T) {
	cases := map[string][][]string{
		"줄이 여덟이다":   onlyKings()[:8],
		"한 줄이 여덟 칸": append(onlyKings()[:8], far(".", ".", ".", ".", ".", ".", ".", ".")),
	}
	for name, rows := range cases {
		t.Run(name, func(t *testing.T) {
			c := stub(t, read{Found: true, Rows: rows})
			if _, err := c.Read(context.Background(), png); err == nil {
				t.Fatal("Read() = nil error, want a refusal")
			}
		})
	}
}

func TestReadChecksTheImageItself(t *testing.T) {
	c := stub(t, read{Found: true, Rows: onlyKings()})

	// 클라이언트가 말한 형식을 안 믿는다. 앞머리가 아는 셋이 아니면 저쪽 API 로 안 나간다.
	if _, err := c.Read(context.Background(), []byte("<html>hello</html>")); !errors.Is(err, ErrNotImage) {
		t.Fatalf("Read(html) error = %v, want ErrNotImage", err)
	}
	if _, err := c.Read(context.Background(), make([]byte, MaxImage+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Read(too large) error = %v, want ErrTooLarge", err)
	}
}

// 키가 없으면 이 계층만 꺼진다. nil 에 불러도 안전해야 부르는 쪽이 nil 검사를 안 흘린다.
func TestReadOnANilClientIsDisabled(t *testing.T) {
	var c *Client
	if c.Model() != "" {
		t.Fatalf("Model() = %q, want empty", c.Model())
	}
	if _, err := c.Read(context.Background(), png); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Read() error = %v, want ErrDisabled", err)
	}
	if New("", "") != nil {
		t.Fatal("New with no key should give nil")
	}
	if got := New("k", "").Model(); got != DefaultModel {
		t.Fatalf("Model() = %q, want %q", got, DefaultModel)
	}
}

// 5xx 는 다음 번에 붙을 수 있다. 스키마를 어긴 응답은 다시 물어도 같은 답이라 안 묻는다.
func TestReadRetriesOnceOnAServerError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		writeStub(t, w, read{Found: true, Rows: onlyKings()})
	}))
	t.Cleanup(srv.Close)

	c := New("key", "")
	SetURLForTest(c, srv.URL)
	if _, err := c.Read(context.Background(), png); err != nil {
		t.Fatalf("Read() = %v, want the retry to succeed", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2", got)
	}
}

func TestReadDoesNotRetryABadPayload(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeStub(t, w, read{Found: true, Rows: onlyKings()[:3]})
	}))
	t.Cleanup(srv.Close)

	c := New("key", "")
	SetURLForTest(c, srv.URL)
	if _, err := c.Read(context.Background(), png); err == nil {
		t.Fatal("Read() = nil error, want a refusal")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

// 그림은 요청 하나에 실려 나가고 어디에도 안 남는다. 그리고 판을 판단하게 하지 않는다 —
// 프롬프트에 「좋은 수」를 묻는 말이 없어야 이 레포의 전제가 성립한다(CLAUDE.md).
func TestRequestSendsTheImageAndAsksNothingElse(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = json.Marshal(json.RawMessage(mustReadAll(t, r)))
		writeStub(t, w, read{Found: true, Rows: onlyKings()})
	}))
	t.Cleanup(srv.Close)

	c := New("key", "")
	SetURLForTest(c, srv.URL)
	if _, err := c.Read(context.Background(), png); err != nil {
		t.Fatalf("Read(): %v", err)
	}

	var sent request
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("the request is not json: %v", err)
	}
	if len(sent.Input) != 1 || len(sent.Input[0].Content) != 1 {
		t.Fatalf("input = %+v, want one image part", sent.Input)
	}
	part := sent.Input[0].Content[0]
	if part.Type != "input_image" || !strings.HasPrefix(part.ImageURL, "data:image/png;base64,") {
		t.Fatalf("part = %+v, want a png data url", part)
	}
	if part.Detail != "high" {
		t.Fatalf("detail = %q, want high — 81 squares of small glyphs", part.Detail)
	}
}

// 프롬프트가 좌표를 한 번도 말하지 않는 것이 이 계층의 경계다(journal §126 · §129).
// 여기에 「5五」나 「筋」이 들어오는 순간 kifunorm 이 실측으로 그은 선을 넘는다.
func TestInstructionsNeverNameASquareOrJudgeTheBoard(t *testing.T) {
	// 좌표와 선후의 낱말은 부정문으로도 안 쓴다. 프롬프트에 한 번 나오면 그것이 곧
	// 「그런 것을 아는 계층」이라는 신호이고, 다음 사람이 거기에 한 줄을 더한다.
	for _, w := range []string{"筋", "段", "先手", "後手", "sente", "gote", "file", "rank"} {
		if strings.Contains(instructions, w) {
			t.Errorf("the prompt says %q — this layer does not work out coordinates or sides", w)
		}
	}

	// 판단은 스키마가 막는다. 담을 칸이 없으면 프롬프트가 무엇을 말해도 판단이 안 나온다 —
	// 낱말 검사로는 「do not evaluate」와 「evaluate」를 가를 수 없어서, 강제하는 자리를
	// 여기로 둔다.
	schema, err := json.Marshal(schemaFormat().Schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	banned := []string{
		// 手番: 사진이 말해 주지 않는 값이라 물으면 지어낸 답이 온다.
		"turn", "toMove", "sideToMove",
		// 판단: 이 레포에서 형세와 최선수는 결정적 코드가 만든다(CLAUDE.md).
		"eval", "score", "best", "advantage", "winner", "comment",
	}
	for _, w := range banned {
		if strings.Contains(string(schema), w) {
			t.Errorf("the schema has a %q field — this layer only transcribes what is drawn", w)
		}
	}
}

func mustRead(t *testing.T, c *Client) Result {
	t.Helper()
	got, err := c.Read(context.Background(), png)
	if err != nil {
		t.Fatalf("Read(): %v", err)
	}
	return got
}

// stub 은 정해진 답 하나를 주는 창구다.
func stub(t *testing.T, answer read) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeStub(t, w, answer)
	}))
	t.Cleanup(srv.Close)

	c := New("key", "")
	SetURLForTest(c, srv.URL)
	return c
}

// writeStub 은 Responses API 의 봉투에 답을 담는다.
func writeStub(t *testing.T, w http.ResponseWriter, answer read) {
	t.Helper()
	payload, err := json.Marshal(answer)
	if err != nil {
		t.Fatalf("marshal answer: %v", err)
	}
	env := map[string]any{
		"status": "completed",
		"output": []any{map[string]any{
			"type":    "message",
			"content": []any{map[string]any{"type": "output_text", "text": string(payload)}},
		}},
		"usage": map[string]any{"total_tokens": 1234},
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(env); err != nil {
		t.Fatalf("write stub: %v", err)
	}
}

func mustReadAll(t *testing.T, r *http.Request) []byte {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read request: %v", err)
	}
	return b
}

// near·far 는 격자 한 줄을 적는 자리다. 이름이 그림의 위아래를 말한다 — 대문자가
// 아래쪽 편이고, 그것이 곧 SFEN 의 대문자다.
func near(cells ...string) []string { return cells }

func far(cells ...string) []string { return cells }

func empty() []string {
	return far(".", ".", ".", ".", ".", ".", ".", ".", ".")
}

// onlyKings 는 玉 둘뿐인 격자다. 판 내용이 시험의 관심이 아닐 때 쓴다.
func onlyKings() [][]string {
	rows := make([][]string, 0, 9)
	rows = append(rows, far(".", ".", ".", ".", "k", ".", ".", ".", "."))
	for range 7 {
		rows = append(rows, empty())
	}
	rows = append(rows, near(".", ".", ".", ".", "K", ".", ".", ".", "."))
	return rows
}
