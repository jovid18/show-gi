// Package boardread 는 판이 찍힌 그림에서 국면 한 벌을 읽는다.
//
// **여기는 그려진 것만 만진다.** 手番도 정하지 않고, 합법인지도 안 보고, 무엇이 좋은
// 수인지는 묻지도 않는다 — 나온 국면은 룰 엔진의 검사를 지나야 쓰이고(shogi.Faults),
// 그 검사가 못 잡는 오독은 사람이 확인 화면에서 고친다(journal §129).
//
// 좌표를 시키지 않는다. kifunorm 이 그은 경계와 같은 자리이고, 여기서는 그것이
// 「프롬프트가 筋도 段도 말하지 않는다」로 나온다 — 시키는 일이 「위 줄부터, 왼쪽부터
// 그려진 대로 적어라」 하나다. 그 순서가 SFEN 판 칸의 순서와 그대로 같아서(internal/shogi
// 패키지 doc) 옮기는 코드에 좌표 계산이 없다.
//
// 先手·後手도 안 시킨다. 사진은 찍은 사람의 시점이라 아래쪽이 언제나 자기 편이고,
// 그림에서 알 수 있는 것은 「위쪽 편인가 아래쪽 편인가」뿐이다 — 그래서 이 계층은
// 그것만 말하고, 아래쪽을 先手로 두는 것은 코드가 정한다. 쇼기에 선후 비대칭 규칙이
// 없어서 그 정규화에 잃는 것이 없고, 그 덕에 사람에게 물을 것이 「あなたの手番ですか」
// 하나로 줄어든다.
//
// 그림은 신뢰할 수 없는 입력이다. 스키마가 출력 모양을 묶으므로 그림에 무엇이 적혀
// 있든 최악이 「거절되는 읽기」다 — 실제로 방송 화면에는 그런 글이 찍혀 온다.
//
// 키가 없으면 이 계층만 꺼진다. 다른 표면은 한 줄도 안 바뀐다.
package boardread

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// MaxImage 는 받는 그림의 크기 상한이다.
//
// 화면 캡처 한 장이 보통 2MB 아래다. 그 세 배로 두면 사람이 올리는 것은 다 들어오고,
// 넘는 것은 사진이 아니라 다른 것이다.
const MaxImage = 6 << 20

// DefaultModel 은 값이 안 주어졌을 때의 모델이다. **실측으로 골랐다**(journal §129).
//
// kifunorm 과 달리 mini 가 아니다. 저쪽은 글자를 옮기는 일이고 여기는 81칸의 작은
// 글자와 그 방향을 읽는 일이라, 정확도가 곧 이 기능의 값이다.
//
// 라벨 붙인 그림 8장에서 gpt-5.4 가 92.9%·성립하는 판 0/8 인데 이 모델이 98.1%·8/8 이다.
// 프롬프트를 네 번 고쳐 얻은 것이 2.8%p 인데 모델 하나가 5.2%p 를 냈다 — **이 계층에서는
// 모델이 프롬프트보다 큰 손잡이다.** 토큰은 두 배쯤 쓴다.
//
// 갈아 끼울 자리를 남긴다(BOARDREAD_MODEL). 재는 법은 apps/server/README.md.
const DefaultModel = "gpt-5.5"

// defaultTimeout 은 한 번의 호출에 주는 시한이다.
//
// kifunorm(30s)보다 한참 길다. 81칸을 큰 해상도로 보는 호출이라 더 걸리고, 사람이 그림을
// 올려 둔 채 기다리는 자리라 한 번에 끝나는 편이 다시 올리는 것보다 낫다.
//
// 60초에서 올렸다. 그 값에서 실측 8장 중 한 장이 걸렸고(journal §129), 걸린 호출은
// 사람에게 「読み取れませんでした」로 보인다 — 다시 올리면 토큰을 한 번 더 쓴다.
const defaultTimeout = 2 * time.Minute

const endpoint = "https://api.openai.com/v1/responses"

// ErrDisabled 는 키가 없어 이 계층이 꺼져 있는 자리다.
var ErrDisabled = errors.New("boardread: no api key")

// ErrTooLarge 는 그림이 MaxImage 를 넘은 자리다.
var ErrTooLarge = errors.New("boardread: image too large")

// ErrNotImage 는 받은 것이 아는 그림 형식이 아닌 자리다.
var ErrNotImage = errors.New("boardread: not a png, jpeg or webp image")

// ErrNoBoard 는 그림에 판이 없다고 답이 온 자리다. 고장이 아니라 사실이라 사유를 가른다 —
// 화면이 「다시 눌러 보라」가 아니라 「판이 보이는 그림을 올려라」를 말해야 한다.
var ErrNoBoard = errors.New("boardread: no board in the image")

// Client 는 읽기 창구다. 키가 없으면 New 가 nil 을 주고, nil 에 Read 를 불러도 안전하게
// ErrDisabled 다 — 부르는 쪽이 nil 검사를 안 흘리게 하는 자리다(kifunorm.Client 와 같다).
type Client struct {
	key   string
	model string
	http  *http.Client
	url   string
}

// New 는 창구를 만든다. 키가 비면 nil 이다.
func New(key, model string) *Client {
	if key == "" {
		return nil
	}
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		key:   key,
		model: model,
		http:  &http.Client{Timeout: defaultTimeout},
		url:   endpoint,
	}
}

// Result 는 그림에서 읽어 낸 국면이다.
type Result struct {
	// SFEN 은 아래쪽 편을 先手로 둔 국면이다.
	//
	// 手番이 언제나 "b" 로 적혀 있고, 그 한 글자는 아직 사실이 아니다 — 사진은 手番을
	// 말해 주지 않으므로 사람이 고르고, 고른 값이 이 자리를 덮는다.
	SFEN string
	// Tokens 는 이 호출이 쓴 토큰 수다. 로그에만 나간다.
	Tokens int
}

// Model 은 부르고 있는 모델 이름이다. 로그가 그것을 적는다.
func (c *Client) Model() string {
	if c == nil {
		return ""
	}
	return c.model
}

// Read 는 그림 한 장에서 국면을 읽는다.
//
// 실패·시한·스키마 위반이 전부 같은 결과다 — 거절. 반쯤 읽은 판을 쓰면 없는 국면 위에서
// 형세와 최선수가 돌고, 그것은 초심자가 검증할 수 없는 거짓이다.
//
// 한 번만 다시 해 본다. 5xx 와 끊긴 연결은 다음 번에 붙지만, 스키마를 어긴 응답은 다시
// 물어도 같은 자리에서 같은 답이다(kifunorm.Normalize 와 같은 판단).
func (c *Client) Read(ctx context.Context, image []byte) (Result, error) {
	if c == nil {
		return Result{}, ErrDisabled
	}
	if len(image) > MaxImage {
		return Result{}, ErrTooLarge
	}
	// 클라이언트가 말한 형식을 안 믿는다. 앞머리를 직접 본다 — 남이 붙인 이름으로
	// 형식을 정하면 png 라고 적힌 무엇이든 저쪽 API 로 그대로 나간다.
	mime, ok := imageMIME(image)
	if !ok {
		return Result{}, ErrNotImage
	}

	dataURL := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(image)

	var last error
	for attempt := range 2 {
		got, retry, err := c.once(ctx, dataURL)
		if err == nil {
			return got, nil
		}
		last = err
		if !retry || ctx.Err() != nil || attempt == 1 {
			break
		}
	}
	return Result{}, last
}

// once 는 한 번 부른다. retry 가 참이면 다시 해 볼 값이 있는 실패다.
func (c *Client) once(ctx context.Context, dataURL string) (Result, bool, error) {
	body, err := json.Marshal(request{
		Model:        c.model,
		Instructions: instructions,
		Input:        imageInput(dataURL),
		Text:         textFormat{Format: schemaFormat()},
	})
	if err != nil {
		return Result{}, false, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return Result{}, false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		return Result{}, true, fmt.Errorf("boardread: %w", err)
	}
	defer res.Body.Close()

	// 응답을 통째로 읽되 상한을 건다. 여기서 무한정 읽으면 남의 서버가 이 프로세스의
	// 메모리를 정하게 된다.
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return Result{}, true, fmt.Errorf("boardread: read: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		retry := res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests
		return Result{}, retry, fmt.Errorf("boardread: http %d: %s", res.StatusCode, snippet(raw))
	}

	var out response
	if err := json.Unmarshal(raw, &out); err != nil {
		return Result{}, false, fmt.Errorf("boardread: decode: %w", err)
	}
	// 잘린 응답은 반쪽 판이다. 시한이나 토큰 상한에 걸린 자리다.
	if out.Status != "" && out.Status != "completed" {
		return Result{}, false, fmt.Errorf("boardread: response %s", out.Status)
	}

	payload, ok := out.text()
	if !ok {
		return Result{}, false, errors.New("boardread: no message in the response")
	}
	var got read
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		return Result{}, false, fmt.Errorf("boardread: payload: %w", err)
	}
	if !got.Found {
		return Result{}, false, ErrNoBoard
	}

	sfen, err := sfenOf(got)
	if err != nil {
		return Result{}, false, err
	}
	return Result{SFEN: sfen, Tokens: out.Usage.TotalTokens}, false, nil
}

// imageMIME 은 앞머리로 형식을 정한다. 아는 셋이 아니면 거짓이다.
//
// gif 를 안 받는다. 애니메이션이면 어느 프레임을 읽었는지가 답에 안 적히고, 그러면
// 사람이 확인 화면에서 보는 판이 어느 순간의 것인지 알 수 없다.
func imageMIME(b []byte) (string, bool) {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png", true
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg", true
	case len(b) >= 12 && bytes.HasPrefix(b, []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return "image/webp", true
	}
	return "", false
}

// Ext 는 이 그림의 확장자다(`.png`). 아는 형식이 아니면 빈 값이다.
//
// **앞머리로 정한다.** 그림을 파일로 떨어뜨리는 자리가 이름을 지을 때 쓰는데(server 의
// 픽스처 수집), 클라이언트가 말한 형식을 쓰면 남이 준 글자가 파일 이름에 들어간다.
func Ext(image []byte) string {
	mime, ok := imageMIME(image)
	if !ok {
		return ""
	}
	return "." + strings.TrimPrefix(mime, "image/")
}

// snippet 은 오류에 실을 만큼만 자른다. 남의 응답 전체를 로그에 붓지 않는다.
func snippet(b []byte) string {
	const n = 200
	if len(b) > n {
		return string(b[:n]) + "…"
	}
	return string(b)
}

// SetURLForTest 는 부르는 주소를 갈아 끼운다. 테스트 전용이다.
func SetURLForTest(c *Client, url string) {
	if c != nil {
		c.url = url
	}
}

// sfenOf 는 읽어 낸 격자를 SFEN 한 줄로 옮긴다.
//
// **좌표 계산이 없다.** 그림의 위 줄부터·왼쪽부터가 곧 SFEN 판 칸의 순서이고
// (段一부터, 각 단에서 筋9→筋1), 대문자가 아래쪽 편인 것이 곧 SFEN 의 대문자=先手다.
// 아래쪽을 先手로 두기로 정했으므로 글자를 그대로 옮기면 된다.
func sfenOf(got read) (string, error) {
	if len(got.Rows) != 9 {
		return "", fmt.Errorf("boardread: %d rows, want 9", len(got.Rows))
	}
	var board strings.Builder
	for i, row := range got.Rows {
		if len(row) != 9 {
			return "", fmt.Errorf("boardread: row %d has %d squares, want 9", i+1, len(row))
		}
		if i > 0 {
			board.WriteByte('/')
		}
		empty := 0
		for _, cell := range row {
			if cell == emptyCell {
				empty++
				continue
			}
			if empty > 0 {
				board.WriteString(itoa(empty))
				empty = 0
			}
			board.WriteString(cell)
		}
		if empty > 0 {
			board.WriteString(itoa(empty))
		}
	}

	// 手番은 아직 사실이 아니다. 사람이 고른 값이 이 자리를 덮는다(Result.SFEN).
	return board.String() + " b " + handField(got.NearHand, got.FarHand) + " 1", nil
}

// handField 는 두 駒台를 SFEN 의 持ち駒 칸으로 옮긴다.
//
// 아래쪽이 대문자다. 순서는 관례대로 飛角金銀桂香歩이고, 1장은 개수를 안 적는다 —
// 값을 왕복시키는 시험이 그 규약에 걸린다(shogi.Position.SFEN).
func handField(near, far hand) string {
	var b strings.Builder
	writeSide(&b, near, true)
	writeSide(&b, far, false)
	if b.Len() == 0 {
		return "-"
	}
	return b.String()
}

// handOrder 는 持ち駒를 적는 순서다. 관례와 같다.
var handOrder = []struct {
	letter string
	get    func(hand) int
}{
	{"R", func(h hand) int { return h.R }},
	{"B", func(h hand) int { return h.B }},
	{"G", func(h hand) int { return h.G }},
	{"S", func(h hand) int { return h.S }},
	{"N", func(h hand) int { return h.N }},
	{"L", func(h hand) int { return h.L }},
	{"P", func(h hand) int { return h.P }},
}

// maxInHand 는 한 종류의 持ち駒로 적을 수 있는 최대 수다. 한 판의 말 수다.
//
// 종류마다 한 벌의 수(歩 18·香 4…)로 자르지 않는다. 넘치는 것은 그대로 국면에 실어
// 보내고 룰 엔진이 「歩가 몇 장 많다」로 짚어 주는 편이, 조용히 깎아서 사람이 駒台를
// 다시 세게 만드는 것보다 낫다 — 어느 종류든 이 값을 넘으면 그것은 이미 개수가 아니고,
// 여기서 막는 것은 shogi.Position.Hands 의 int8 이 넘치는 값뿐이다.
const maxInHand = 40

func writeSide(b *strings.Builder, h hand, near bool) {
	for _, p := range handOrder {
		n := min(p.get(h), maxInHand)
		if n <= 0 {
			continue
		}
		if n >= 2 {
			b.WriteString(itoa(n))
		}
		letter := p.letter
		if !near {
			letter = strings.ToLower(letter)
		}
		b.WriteString(letter)
	}
}

// itoa 는 작은 수 하나를 적는다. strconv 를 부르지 않는 것은 값이 1~18 뿐이라서다.
func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
