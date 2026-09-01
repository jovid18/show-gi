package kifunorm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/kifu"
)

// 실제로 OpenAI 를 부른다. 키가 없으면 skip 이다 — CI 에서 안 돈다.
//
// 재는 것은 「모델이 잘 옮기는가」가 아니라 **배선이 성립하는가**다: 결정적 파서가 전부
// 실패하는 텍스트가 정규화를 지나 룰 엔진까지 통과하는가. 모델이 지어내면 kifu.ParseMoves
// 에서 걸리므로, 이 시험이 초록이면 그 판은 합법 수순이다.
func TestLiveNormalizeReachesTheRuleEngine(t *testing.T) {
	key := os.Getenv("SHOWGI_OPENAI_KEY")
	if key == "" {
		t.Skip("no SHOWGI_OPENAI_KEY")
	}

	// 어느 결정적 파서로도 안 읽힌다. 표기는 일본어인데 줄의 모양이 KIF 도 KI2 도 아니고
	// (手数가 셀에 들어 있고 표식이 없다) 낱말로 끊어도 태그가 붙어 있다 — 웹 페이지에서
	// 복사해 오면 실제로 이렇게 온다.
	const messy = `<h2>対局結果 2026-08-30</h2>
<p>先手 わたし(2級) / 後手 あいて(1級)</p>
<table>
<tr><td>1</td><td>７六歩</td><td>2</td><td>３四歩</td></tr>
<tr><td>3</td><td>２六歩</td><td>4</td><td>８四歩</td></tr>
<tr><td>5</td><td>２五歩</td><td>6</td><td>８五歩</td></tr>
<tr><td>7</td><td>７八金</td><td>8</td><td>３二金</td></tr>
<tr><td>9</td><td>２四歩</td><td>10</td><td>同歩</td></tr>
<tr><td>11</td><td>同飛</td><td>12</td><td>２三歩打</td></tr>
<tr><td>13</td><td>２六飛</td><td>14</td><td>投了</td></tr>
</table>
<p>13手で先手の勝ち</p>`

	if _, _, err := kifu.Read(messy); err == nil {
		t.Fatal("a deterministic parser read it; this sample no longer tests the fallback")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	c := New(key, os.Getenv("SHOWGI_OPENAI_MODEL"))
	got, err := c.Normalize(ctx, messy)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("model %s, %d tokens, %d moves, handicap %q, result %q",
		c.Model(), got.Tokens, len(got.Moves), got.Handicap, got.Result)

	g, err := kifu.ParseMoves(got.Handicap, got.Moves)
	if err != nil {
		t.Fatalf("the rule engine refused what was transcribed: %v", err)
	}
	want := []string{"7g7f", "3c3d", "2g2f", "8c8d", "2f2e", "8d8e", "6i7h", "4a3b", "2d2c+"}
	if len(g.Moves) != 13 {
		t.Errorf("len(Moves) = %d, want 13", len(g.Moves))
	}
	for i, w := range want[:min(len(g.Moves), 8)] {
		if g.Moves[i] != w {
			t.Errorf("move %d = %q, want %q", i+1, g.Moves[i], w)
		}
	}
	if got.Result != "sente" {
		t.Errorf("Result = %q, want sente", got.Result)
	}
	if !strings.Contains(got.Sente, "わたし") {
		t.Errorf("Sente = %q", got.Sente)
	}
}
