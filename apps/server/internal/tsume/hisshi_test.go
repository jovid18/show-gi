package tsume

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

func solveHisshi(t *testing.T, sfen string) HisshiResult {
	t.Helper()
	pos, err := shogi.ParseSFEN(sfen)
	if err != nil {
		t.Fatal(err)
	}
	res, err := SolveHisshi(context.Background(), pos, DefaultHisshiLimits)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// 책의 1手必至 두 문제다. 둘 다 詰め将棋로는 詰まない.
func TestHisshiFindsTheOnlyAnswer(t *testing.T) {
	for _, c := range []struct {
		sfen, want string
		// replies 는 수비 응수 전부, threat 는 詰めろ와 같은 그룹의 크기다.
		replies, threat int
	}{
		{"5RSk1/6g1l/6s1p/9/9/9/9/9/9 b Sr2b3gs4n3l17p 1", "▲2三銀", 481, 476},
		{"7+P1/4+b4/6+Rgk/5R2p/9/9/9/9/9 b b3g4s4n4l16p 1", "▲2四飛", 419, 396},
	} {
		res := solveHisshi(t, c.sfen)
		if res.Status != HisshiFound {
			t.Fatalf("%s: status %d", c.sfen, res.Status)
		}
		if !slices.Equal(res.Answers, []string{c.want}) {
			t.Errorf("%s: answers %v, want [%s]", c.sfen, res.Answers, c.want)
		}
		if res.First.Ja != c.want || len(res.First.Replies) == 0 {
			t.Fatalf("%s: tree root %q with %d replies", c.sfen, res.First.Ja, len(res.First.Replies))
		}
		// 응수마다 詰み 수순이 이어지고, 공격 쪽 수는 응수 뒤 국면에서 詰み를 건다.
		for _, r := range res.First.Replies {
			if len(r.Replies) != 1 || r.Rest != r.Replies[0].Rest+1 {
				t.Fatalf("%s: reply %s has %d lines, rest %d", c.sfen, r.Ja, len(r.Replies), r.Rest)
			}
		}
		// 응수는 공격 쪽 수순이 같은 것끼리 묶인다. 두 문제 모두 4그룹이다.
		total, threats := 0, 0
		for _, r := range res.First.Replies {
			total += 1 + len(r.Same)
			if r.Threat {
				threats++
				if 1+len(r.Same) != c.threat {
					t.Errorf("%s: threat group %s has %d replies, want %d", c.sfen, r.Ja, 1+len(r.Same), c.threat)
				}
			}
		}
		if len(res.First.Replies) != 4 || total != c.replies || threats != 1 {
			t.Errorf("%s: %d groups, %d replies, %d threat groups", c.sfen, len(res.First.Replies), total, threats)
		}
		t.Logf("%s: %s, %d groups of %d replies, longest %d plies, %d nodes",
			c.sfen, res.First.Ja, len(res.First.Replies), total, res.First.Rest, res.Nodes)
	}
}

func TestHisshiTellsMateApart(t *testing.T) {
	// 頭金 1手詰め.
	res := solveHisshi(t, "4k4/9/4P4/9/9/9/9/9/9 b G2r2b3g4s4n4l17p 1")
	if res.Status != HisshiMate {
		t.Fatalf("status %d, want HisshiMate", res.Status)
	}
}

func TestHisshiRefusesTheAttackingKing(t *testing.T) {
	pos, err := shogi.ParseSFEN("4k4/9/9/9/9/9/9/9/4K4 b G2r2b3g4s4n4l18p 1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SolveHisshi(context.Background(), pos, DefaultHisshiLimits); !errors.Is(err, ErrAttackerKing) {
		t.Fatalf("err %v, want ErrAttackerKing", err)
	}
}

func TestHisshiNoneWhenTheKingRunsFree(t *testing.T) {
	// 넓은 판의 玉 하나와 歩 한 장. 어느 수를 두어도 逃げる.
	res := solveHisshi(t, "9/9/9/9/4k4/9/9/9/9 b P2r2b4g4s4n4l17p 1")
	if res.Status != HisshiNone {
		t.Fatalf("status %d, want HisshiNone", res.Status)
	}
}
