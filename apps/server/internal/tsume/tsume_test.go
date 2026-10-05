package tsume

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

func solve(t *testing.T, sfen string, lim Limits) Result {
	t.Helper()
	pos, err := shogi.ParseSFEN(sfen)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Solve(context.Background(), pos, lim)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status == Mate {
		checkTree(t, pos, res.First)
	}
	return res
}

// checkTree 는 트리 전체를 룰 엔진으로 다시 둔다. 공격 쪽 수는 王手, 수비 쪽 응수는 빠짐없이,
// 잎은 詰み여야 한다.
func checkTree(t *testing.T, pos shogi.Position, mv *Move) {
	t.Helper()
	m, err := shogi.ParseUSIMove(mv.USI)
	if err != nil {
		t.Fatal(err)
	}
	if err := pos.ValidateMove(m); err != nil {
		t.Fatalf("%s: %s is illegal: %v", pos.SFEN(), mv.USI, err)
	}
	after := pos.Apply(m)
	if !after.InCheck(after.Turn) {
		t.Fatalf("%s: %s is not a check", pos.SFEN(), mv.USI)
	}
	evasions := after.LegalMoves()
	if len(evasions) == 0 {
		if mv.Rest != 0 || len(mv.Replies) != 0 {
			t.Fatalf("%s: %s mates but has rest %d", pos.SFEN(), mv.USI, mv.Rest)
		}
		return
	}
	if len(mv.Replies) != len(evasions) {
		t.Fatalf("%s: %s has %d replies, want every one of %d evasions", pos.SFEN(), mv.USI, len(mv.Replies), len(evasions))
	}
	for _, r := range mv.Replies {
		if r.Futile {
			continue
		}
		rm, _ := shogi.ParseUSIMove(r.USI)
		if len(r.Replies) != 1 {
			t.Fatalf("%s: reply %s has %d answers", after.SFEN(), r.USI, len(r.Replies))
		}
		checkTree(t, after.Apply(rm), r.Replies[0])
	}
}

func TestHeadGoldIsMateInOne(t *testing.T) {
	res := solve(t, "4k4/9/4P4/9/9/9/9/9/9 b G 1", DefaultLimits)
	if res.Status != Mate || res.Plies != 1 || res.First.USI != "G*5b" || !res.Shortest {
		t.Fatalf("got %+v, want 1手詰 G*5b", res)
	}
	if res.First.Ja != "▲5二金" {
		t.Fatalf("Ja = %q", res.First.Ja)
	}
}

func TestLoneKingCannotBeMated(t *testing.T) {
	res := solve(t, "4k4/9/9/9/9/9/9/9/9 b P 1", DefaultLimits)
	if res.Status != NoMate {
		t.Fatalf("got %+v, want NoMate", res)
	}
}

// ▲9一飛 의 王手에 歩 合駒는 銀이 지키는 칸이라 모두 잡히고, 잡은 歩 없이도 詰む. 실제로는 3수이지만
// 관례로는 1手詰이다.
func TestFutileInterpositionsDoNotCount(t *testing.T) {
	res := solve(t, "4k4/2S6/R3G4/9/9/9/9/9/9 b p 1", DefaultLimits)
	if res.Status != Mate || res.Plies != 1 {
		t.Fatalf("got status %v plies %d, want 1手詰", res.Status, res.Plies)
	}
	if len(res.First.Replies) == 0 {
		t.Fatal("the rook check has no interpositions to fold")
	}
	for _, r := range res.First.Replies {
		if !r.Futile {
			t.Fatalf("%s %s is not folded as 無駄合い", res.First.USI, r.USI)
		}
	}
}

// 같은 手数의 王手가 여럿이면 응수가 적은 것을 고른다. 두 국면은 아래 113手目 직전 트리의 갈래다.
// 수의 순서로는 앞의 것(▲5五歩 응수 2, ▲7三銀成 응수 3)이 나왔었다.
func TestEqualMatesPreferFewerReplies(t *testing.T) {
	for _, c := range []struct {
		sfen    string
		plies   int
		first   string
		replies int
	}{
		{"l5sn1/4+R1g2/p3gp1Pp/3pk1P2/3n2S2/2P4p1/P1pP1P2P/r1BK5/5G1NL b G5Pb2sn2l 127", 5, "▲5五金", 1},
		{"l5sn1/2kS2g2/p3+Rp1Pp/3p2P2/3n2S2/2P4p1/P1pP1P2P/r1BK5/5G1NL b G4Pbgsn2lp 127", 3, "▲7三龍", 1},
	} {
		res := solve(t, c.sfen, DefaultLimits)
		if res.Status != Mate || res.Plies != c.plies || res.First.Ja != c.first || res.First.branches() != c.replies {
			t.Errorf("%s: got %d手 %s with %d replies, want %d手 %s with %d",
				c.sfen, res.Plies, res.First.Ja, res.First.branches(), c.plies, c.first, c.replies)
		}
	}
}

// 王座戦 第74期 第2局(2026-09-15) 115手 끝의 국면들. 先手가 107手目부터 王手만으로 몰았다.
// 113手目 직전: ▲６一角成 △４一玉 ▲５一金 까지 둔 실전 수순이 있다.
func TestOzaEndgameShortMate(t *testing.T) {
	res := solve(t, "l3k1sn1/1RBs2g2/p3gp1Pp/3p2P2/3n2S2/2P4p1/P1pP1P2P/r1BK5/5G1NL b G4Psn2lp 113", DefaultLimits)
	if res.Status != Mate || !res.Shortest {
		t.Fatalf("got %+v", res)
	}
	t.Logf("113: %d手詰 %s, %d nodes", res.Plies, res.First.Ja, res.Nodes)
}

// 107手目 직전은 25手詰め다. YaneuraOu 의 go mate 는 35手 수순을 주었다(journal §147).
// 1분 가까이 걸려 SHOWGI_TSUME_LONG 이 있을 때만 돈다.
func TestOzaEndgameLongMate(t *testing.T) {
	if os.Getenv("SHOWGI_TSUME_LONG") == "" {
		t.Skip("set SHOWGI_TSUME_LONG=1")
	}
	for _, c := range []struct {
		sfen  string
		plies int
	}{
		{"l5sn1/1R1sk1g2/p2gpp1Pp/3p2P2/3n2S2/2P1L2p1/P1pP1P2P/r1BK5/5G1NL b BGS3Pnlp 107", 25},
	} {
		res := solve(t, c.sfen, DefaultLimits)
		t.Logf("%s: %d手詰 shortest=%v %d nodes, first %s", c.sfen[strings.LastIndex(c.sfen, " ")+1:], res.Plies, res.Shortest, res.Nodes, res.First.Ja)
		if res.Status != Mate || res.Plies != c.plies || !res.Shortest {
			t.Fatalf("got status %v plies %d shortest %v, want %d", res.Status, res.Plies, res.Shortest, c.plies)
		}
	}
}
