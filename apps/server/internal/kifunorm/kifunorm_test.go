package kifunorm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// reply 는 Responses API 의 응답 한 벌을 흉내 낸다.
func reply(payload string) string {
	body, err := json.Marshal(map[string]any{
		"status": "completed",
		"output": []any{
			map[string]any{"type": "reasoning", "content": []any{}},
			map[string]any{"type": "message", "content": []any{
				map[string]any{"type": "output_text", "text": payload},
			}},
		},
		"usage": map[string]any{"total_tokens": 497},
	})
	if err != nil {
		panic(err)
	}
	return string(body)
}

func serving(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New("test-key", "")
	c.url = srv.URL
	return c
}

func TestNormalize(t *testing.T) {
	var gotBody []byte
	c := serving(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		gotBody, _ = readAll(r)
		fmt.Fprint(w, reply(`{"handicap":"香落ち","sente":"a","gote":"b","result":"sente","moves":["3四歩","7六歩"]}`))
	})

	got, err := c.Normalize(context.Background(), "どんな形か分からない棋譜")
	if err != nil {
		t.Fatal(err)
	}
	if got.Handicap != "香落ち" || got.Result != "sente" || len(got.Moves) != 2 {
		t.Errorf("got %+v", got)
	}
	if got.Tokens != 497 {
		t.Errorf("Tokens = %d, want 497", got.Tokens)
	}

	// 프롬프트에 국면도 평가치도 안 실린다. 보내는 것은 원문과 지시뿐이다.
	var sent request
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Model != DefaultModel {
		t.Errorf("Model = %q, want %q", sent.Model, DefaultModel)
	}
	if sent.Input != "どんな形か分からない棋譜" {
		t.Errorf("Input = %q", sent.Input)
	}
	if !sent.Text.Format.Strict {
		t.Error("the schema is not strict; the response shape would be unbounded")
	}
}

// 반쯤 옮긴 것을 쓰면 뒷부분이 조용히 없어진 기보가 되고, 그 위에서 평가치와 段級이 돈다.
func TestIncompleteResponseIsRefused(t *testing.T) {
	c := serving(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"status":"incomplete","output":[{"type":"message","content":[{"type":"output_text","text":"{\"handicap\":\"\",\"sente\":\"\",\"gote\":\"\",\"result\":\"unknown\",\"moves\":[\"7六歩\"]}"}]}]}`)
	})
	if _, err := c.Normalize(context.Background(), "x"); err == nil {
		t.Fatal("accepted a truncated response")
	}
}

func TestBrokenPayloadIsRefused(t *testing.T) {
	for _, tc := range []struct{ name, payload string }{
		{"not json", `not json at all`},
		{"no moves", `{"handicap":"","sente":"","gote":"","result":"unknown","moves":[]}`},
	} {
		c := serving(t, func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, reply(tc.payload))
		})
		if _, err := c.Normalize(context.Background(), "x"); err == nil {
			t.Errorf("%s: accepted", tc.name)
		}
	}
}

// 사람이 둔 한 판이 아니다. 그대로 받으면 분석 큐에 手 수천 개가 한 번에 선다.
func TestTooManyMovesIsRefused(t *testing.T) {
	moves := make([]string, MaxMoves+1)
	for i := range moves {
		moves[i] = "7六歩"
	}
	body, err := json.Marshal(normalized{Result: "unknown", Moves: moves})
	if err != nil {
		t.Fatal(err)
	}
	c := serving(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, reply(string(body)))
	})
	if _, err := c.Normalize(context.Background(), "x"); err == nil {
		t.Fatal("accepted more moves than one game has")
	}
}

// 4xx 는 같은 요청이 같은 답을 준다. 다시 물어 봐야 값이 없다.
func TestClientErrorIsNotRetried(t *testing.T) {
	calls := 0
	c := serving(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"message":"bad"}}`)
	})
	if _, err := c.Normalize(context.Background(), "x"); err == nil {
		t.Fatal("accepted a 400")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

// 5xx 는 다음 번에 붙는다. 한 번만 다시 해 본다.
func TestServerErrorIsRetriedOnce(t *testing.T) {
	calls := 0
	c := serving(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, reply(`{"handicap":"","sente":"","gote":"","result":"unknown","moves":["7六歩"]}`))
	})
	if _, err := c.Normalize(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestTooLargeIsRefusedWithoutCalling(t *testing.T) {
	c := serving(t, func(http.ResponseWriter, *http.Request) {
		t.Error("called the api for an input over the limit")
	})
	if _, err := c.Normalize(context.Background(), strings.Repeat("x", MaxInput+1)); err != ErrTooLarge {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

// 키가 없으면 이 계층만 꺼진다. nil 에 불러도 안전해야 부르는 쪽이 nil 검사를 안 흘린다.
func TestNoKeyIsDisabled(t *testing.T) {
	var c *Client
	if got := New("", "m"); got != nil {
		t.Error("made a client without a key")
	}
	if _, err := c.Normalize(context.Background(), "x"); err != ErrDisabled {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
	if c.Model() != "" {
		t.Error("a nil client named a model")
	}
}

func readAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}
