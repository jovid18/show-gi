// Package kifunorm 은 읽을 수 없는 형식의 기보 텍스트를 표기 한 벌로 옮긴다.
//
// **여기는 글자만 만진다.** 목적지도 출발칸도 성/불성도 정하지 않고, 합법수인지도 안 본다 —
// 그 전부를 룰 엔진이 뒤에서 다시 한다(internal/kifu 가 낸 것을 shogi.ValidateMove 로 전수
// 검증한다). 그래서 이 계층의 출력에 「믿는 부분」이 없고, 지어내면 반드시 거기서 걸린다.
//
// 좌표를 시키지 않는 것이 그 경계다. 서양식 표기(P-7f)를 일본어로 옮기게 해 봤더니 13手
// 중 하나에서 rank 글자를 잘못 짚었고, 세 번 돌려 세 번 다 같은 자리였다(journal §126) —
// 룰 엔진이 잡아 임포트는 거절됐지만, 그런 일을 시키는 것 자체가 이 계층의 일이 아니다.
// 그래서 프롬프트가 하는 말이 「원문에 적힌 표기를 그대로 옮겨라」 하나다.
//
// 부르는 조건이 하나다: 결정적 파서가 전부 실패했을 것(kifu.Read). 같은 기보가 언제나 같은
// 결과를 주는 것이 기본값이고, 이 계층은 그 기본값이 성립하지 않는 자리에만 선다.
//
// 판을 프롬프트에 넣지 않는다. 국면도 평가치도 안 보내고 「이 수 어때」를 묻지 않는다 —
// 판단과 표현은 결정적 코드가 하는 것이 이 레포의 전제다(CLAUDE.md).
//
// 키가 없으면 이 계층만 꺼진다. 결정적 파서로 읽히는 기보는 그대로 들어온다.
package kifunorm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// MaxInput 은 받는 원문의 상한이다. 토큰 값과 지연을 여기서 묶는다 —
// 300手 KIF 가 30KB 안쪽이라 그 두 배면 사람이 붙여 넣는 것은 다 들어온다.
const MaxInput = 64 << 10

// MaxMoves 는 받아들이는 手数의 상한이다. 넘으면 거절한다 — 사람이 둔 한 판이 아니다.
const MaxMoves = 512

// DefaultModel 은 값이 안 주어졌을 때의 모델이다. 하는 일이 글자 옮기기라 mini 로 충분하다.
const DefaultModel = "gpt-5.4-mini"

// defaultTimeout 은 한 번의 호출에 주는 시한이다. 넘으면 그 임포트는 거절이다 —
// 사람이 미리보기 화면 앞에서 기다리는 자리라 길게 못 잡는다.
const defaultTimeout = 30 * time.Second

const endpoint = "https://api.openai.com/v1/responses"

// ErrDisabled 는 키가 없어 이 계층이 꺼져 있는 자리다.
var ErrDisabled = errors.New("kifunorm: no api key")

// ErrTooLarge 는 원문이 MaxInput 을 넘은 자리다.
var ErrTooLarge = errors.New("kifunorm: input too large")

// Client 는 정규화 창구다. 키가 없으면 New 가 nil 을 주고, nil 에 Normalize 를 불러도
// 안전하게 ErrDisabled 다 — 부르는 쪽이 nil 검사를 안 흘리게 하는 자리다.
type Client struct {
	key   string
	model string
	http  *http.Client
	url   string
}

// New 는 창구를 만든다. 키가 비면 nil 이다 — Google 로그인이 값 하나만 비어도 표면째
// 닫히는 것과 같은 규약이다.
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

// Result 는 옮겨 적은 것이다. 아직 수가 아니라 글자다 — 수가 되는 것은 kifu.ParseMoves 를
// 지난 뒤다.
type Result struct {
	// Handicap 은 원문이 말한 手合割 이름이다. 없으면 빈 값(平手).
	Handicap string
	Sente    string
	Gote     string
	// Result 는 "sente" | "gote" | "draw" | "unknown".
	Result string
	Moves  []string
	// Tokens 는 이 호출이 쓴 토큰 수다. 로그에만 나간다 — 비용이 판당 한 번인 것을
	// 실측으로 확인하는 자리다.
	Tokens int
}

// Model 은 부르고 있는 모델 이름이다. 로그가 그것을 적는다.
func (c *Client) Model() string {
	if c == nil {
		return ""
	}
	return c.model
}

// Normalize 는 원문을 표기 한 벌로 옮긴다.
//
// 실패·시한·스키마 위반이 전부 같은 결과다 — 거절. 반쯤 옮긴 것을 쓰면 사람이 둔 판의
// 뒷부분이 조용히 없어진 기보가 되고, 그 위에서 평가치와 段級이 돈다.
//
// 한 번만 다시 해 본다. 5xx 와 끊긴 연결은 다음 번에 붙지만, 스키마를 어긴 응답은 다시
// 물어도 같은 자리에서 같은 답이다.
func (c *Client) Normalize(ctx context.Context, text string) (Result, error) {
	if c == nil {
		return Result{}, ErrDisabled
	}
	if len(text) > MaxInput {
		return Result{}, ErrTooLarge
	}

	var last error
	for attempt := range 2 {
		got, retry, err := c.once(ctx, text)
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
func (c *Client) once(ctx context.Context, text string) (Result, bool, error) {
	body, err := json.Marshal(request{
		Model:        c.model,
		Instructions: instructions,
		Input:        text,
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
		return Result{}, true, fmt.Errorf("kifunorm: %w", err)
	}
	defer res.Body.Close()

	// 응답을 통째로 읽되 상한을 건다. 여기서 무한정 읽으면 남의 서버가 이 프로세스의
	// 메모리를 정하게 된다.
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return Result{}, true, fmt.Errorf("kifunorm: read: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		// 5xx 와 429 는 다음 번에 붙을 수 있다. 4xx 는 같은 요청이 같은 답을 준다.
		retry := res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests
		return Result{}, retry, fmt.Errorf("kifunorm: http %d: %s", res.StatusCode, snippet(raw))
	}

	var out response
	if err := json.Unmarshal(raw, &out); err != nil {
		return Result{}, false, fmt.Errorf("kifunorm: decode: %w", err)
	}
	// 잘린 응답은 반쪽 기보다. 시한이나 토큰 상한에 걸린 자리이고, 그대로 쓰면 뒷부분이
	// 조용히 없어진다.
	if out.Status != "" && out.Status != "completed" {
		return Result{}, false, fmt.Errorf("kifunorm: response %s", out.Status)
	}

	payload, ok := out.text()
	if !ok {
		return Result{}, false, errors.New("kifunorm: no message in the response")
	}
	var got normalized
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		return Result{}, false, fmt.Errorf("kifunorm: payload: %w", err)
	}
	if len(got.Moves) == 0 {
		return Result{}, false, errors.New("kifunorm: no moves")
	}
	if len(got.Moves) > MaxMoves {
		return Result{}, false, fmt.Errorf("kifunorm: %d moves is more than one game", len(got.Moves))
	}

	return Result{
		Handicap: got.Handicap,
		Sente:    got.Sente,
		Gote:     got.Gote,
		Result:   got.Result,
		Moves:    got.Moves,
		Tokens:   out.Usage.TotalTokens,
	}, false, nil
}

// snippet 은 오류에 실을 만큼만 자른다. 남의 응답 전체를 로그에 붓지 않는다.
func snippet(b []byte) string {
	const n = 200
	if len(b) > n {
		return string(b[:n]) + "…"
	}
	return string(b)
}
