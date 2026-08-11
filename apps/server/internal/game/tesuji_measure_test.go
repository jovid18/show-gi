package game

import (
	"os"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/tag"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 06-status.md §34의 숫자를 만든 자리다. `TesujiLossCp` 가 **[미확정]** 이라 언젠가 다시
// 재야 하고, 그때 재는 방법이 문서에만 있으면 같은 것을 두 번 만들게 된다.
//
// 판정하지 않는다 — 값을 찍고 지나간다. 통과/실패로 만들면 엔진이 흔들릴 때마다 CI가
// 빨개지는데, 흔들린다는 것 자체가 여기서 잰 사실 중 하나다(§34 ②).
//
//	SHOWGI_USI_CMD=/opt/yaneuraou/run SHOWGI_MEASURE=1 go test ./internal/game/ -run MeasureTesuji -v

func measurePool(t *testing.T) *usi.Pool {
	t.Helper()
	cmd := os.Getenv("SHOWGI_USI_CMD")
	if cmd == "" || os.Getenv("SHOWGI_MEASURE") == "" {
		t.Skip("SHOWGI_USI_CMD · SHOWGI_MEASURE 미설정")
	}
	pool, err := usi.NewPool(1, cmd, map[string]string{
		"USI_Hash": "128", "Threads": "1", "FV_SCALE": "24",
		"BookFile": "no_book", "USI_OwnBook": "false",
	})
	if err != nil {
		t.Fatalf("엔진 풀: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// tesujiLoss 는 그 수의 낙폭(사람 관점 cp)과 붙은 이름을 함께 낸다.
//
// **세션이 부르는 것과 같은 함수를 부른다.** 여기서 형태를 따로 세면 측정이 프로덕션과
// 다른 것을 재게 되고, 그 어긋남은 문서의 숫자로만 나타나 아무 데서도 안 터진다.
func tesujiLoss(t *testing.T, an Analyst, moves []string) (int, []tag.Tag) {
	t.Helper()
	before, after := gamePositions(t, moves)
	j, err := an.Judge(t.Context(), shogi.StartSFEN, moves, len(moves))
	if err != nil {
		t.Fatalf("판정: %v", err)
	}
	me := before.Turn
	loss := cpFor(j.SenteCpBefore, me) - cpFor(j.SenteCpAfter, me)
	return loss, namedTesuji(before, after, me, moves[len(moves)-1], j)
}

// gamePositions 는 그 수순의 마지막 수 **앞뒤** 국면이다.
func gamePositions(t *testing.T, moves []string) (before, after shogi.Position) {
	t.Helper()
	before, err := positionAfter(shogi.StartSFEN, moves[:len(moves)-1])
	if err != nil {
		t.Fatalf("앞 국면: %v", err)
	}
	after, err = positionAfter(shogi.StartSFEN, moves)
	if err != nil {
		t.Fatalf("뒤 국면: %v", err)
	}
	return before, after
}

// **게이트가 실제로 무엇을 끄는가** — 실 기보 한 판을 수마다 훑는다(§34 ④).
//
// 세는 것은 셋이다: 手筋의 형태가 **새로** 생긴 수, 그중 이름이 붙은 수, 서로 다른 이름의
// 수. 마지막이 곧 화면에 뜨는 횟수다 — 클라이언트가 한 대국에 이름 하나를 한 번만 띄운다.
func TestMeasureTesujiGateOverAGame(t *testing.T) {
	pool := measurePool(t)
	analyst := NewEngineAnalyst(pool, nil, intervene.Beginner)

	shapes, named := 0, 0
	seen := map[string]bool{}
	for p := 1; p <= len(playtestUpTo103); p++ {
		moves := playtestUpTo103[:p]
		before, after := gamePositions(t, moves)
		me := before.Turn

		// **게이트 앞의 룰 층**이다. 세션이 쓰는 것과 같은 함수라, 「몇 번 뜨는가」가
		// 프로덕션과 어긋날 수 없다.
		fresh := codes(freshTesuji(before, after, me, moves[p-1]))
		if len(fresh) == 0 {
			continue
		}
		shapes++

		loss, out := tesujiLoss(t, analyst, moves)
		if len(out) > 0 {
			named++
			for _, tg := range out {
				seen[tg.Code] = true
			}
		}
		t.Logf("%3d수 %-5s 형태=%v 낙폭=%+5dcp 통과=%v", p, moves[p-1], fresh, loss, codes(out))

		// **「이름은 붙었는데 딸 수가 없는」 형태를 드러낸다.**
		//
		// 게이트가 묻는 것은 「이 **수**가 손해인가」인데, 両取り의 손해는 형태를 만든
		// 수가 아니라 **그다음에 딸 때** 난다 — 게이트의 질문이 한 수 앞이다. 그래서
		// 飛로 銀 둘을 노렸지만 어느 쪽을 먹어도 飛가 죽는 국면이 그대로 통과한다.
		//
		// 최선 수순을 같이 찍으면 눈으로 갈린다: 그 안에 **대상을 따는 수**가 있으면
		// 형태가 현금화되는 것이고, 없으면 이름만 선 것이다. 여기서 판정하지 않는 것은
		// 「딴다」를 자동으로 재려면 대상 칸을 `tag` 밖으로 꺼내야 하고, 그 API를
		// **무엇을 셀지 정하기 전에** 만들면 안 되기 때문이다.
		if len(out) > 0 {
			res, err := pool.SearchMultiPV(t.Context(), shogi.StartSFEN, moves, JudgeDepth, TesujiHintMultiPV)
			if err != nil {
				t.Logf("        최선 수순: %v", err)
				continue
			}
			pv := res.PV
			if len(pv) > 6 {
				pv = pv[:6] // 뒤로 갈수록 확실하지 않다(RefutationPlies 와 같은 이유)
			}
			t.Logf("        최선 수순(상대부터) %v", pv)
		}
	}
	t.Logf("형태 %d개 · 이름이 붙은 수 %d개 · 서로 다른 이름 %d개 (한계 %dcp)", shapes, named, len(seen), TesujiLossCp)
}

// **「이름은 맞는데 값이 없는」 両取り를 재현한다.**
//
// 실제로 화면에서 본 사례다: 飛가 銀 둘을 노려 十字飛車가 떴는데, **어느 쪽을 먹어도
// 飛가 죽어서** 아무 의미가 없었다.
//
//	8五銀  8四歩이 지킨다      ← 飛가 따면 歩로 되잡는다
//	5三銀  4二銀이 지킨다      ← 飛가 따면 銀으로 되잡는다
//	5五飛  지금은 안 잡힌다    ← 그래서 이 수 자체는 손해가 아니다
//
// 게이트가 묻는 것이 「이 **수**가 손해인가」라서 통과할 수 있다 — 손해는 형태를 만든
// 수가 아니라 **딸 때** 나고, 엔진은 「안 따면 그만」이라 국면을 나쁘게 보지 않는다.
//
// 여기서 답이 갈린다. 통과하면 이름 조건이 아니라 **게이트에 조건을 하나 더** 붙여야
// 하고(최선 수순이 대상을 따는가), 안 통과하면 지금 게이트로 이미 잡히는 것이라
// 사용자가 본 화면은 게이트가 붙기 전(06-status.md §34, 8/11)의 것이다.
func TestMeasureDecorativeFork(t *testing.T) {
	pool := measurePool(t)
	analyst := NewEngineAnalyst(pool, nil, intervene.Beginner)

	// 5九飛 → 5五飛. 뜨는 순간 5三銀(縦)과 8五銀(横)을 함께 노린다.
	//
	// **재료를 맞춰 둔다.** 처음에 玉과 飛만 놓고 쟀더니 낙폭이 +1291cp 로 나왔는데,
	// 그건 手筋이 아니라 **詰み 경쟁**이 평가치를 삼킨 것이었다 — 한쪽이 압도적이면
	// 무슨 수를 둬도 수천 cp가 움직여서 100cp 게이트를 재는 의미가 없다.
	const start = "8k/5s1g1/4s4/1p7/1s7/9/2P3P2/1B1G5/4R3K b - 1"
	moves := []string{"5i5e"}

	before, err := positionAfter(start, nil)
	if err != nil {
		t.Fatalf("앞 국면: %v", err)
	}
	after, err := positionAfter(start, moves)
	if err != nil {
		t.Fatalf("뒤 국면: %v", err)
	}

	fresh := codes(freshTesuji(before, after, shogi.Black, moves[0]))
	t.Logf("룰 층이 본 이름: %v", fresh)

	j, err := analyst.Judge(t.Context(), start, moves, len(moves))
	if err != nil {
		t.Fatalf("판정: %v", err)
	}
	loss := cpFor(j.SenteCpBefore, shogi.Black) - cpFor(j.SenteCpAfter, shogi.Black)
	passed := codes(namedTesuji(before, after, shogi.Black, moves[0], j))

	res, err := pool.SearchMultiPV(t.Context(), start, moves, JudgeDepth, TesujiHintMultiPV)
	if err != nil {
		t.Fatalf("최선 수순: %v", err)
	}
	pv := res.PV
	if len(pv) > 6 {
		pv = pv[:6]
	}

	t.Logf("낙폭 %+dcp (한계 %dcp) · 게이트 통과=%v", loss, TesujiLossCp, passed)
	t.Logf("최선 수순(상대부터) %v", pv)
	t.Logf("→ 수순에 5c/8e 를 따는 수가 있는가? 없으면 이름만 선 것이다")
}

// **같은 국면·같은 깊이가 같은 값을 주는가**(§34 ②).
//
// 앞선 탐색이 남긴 치환표 때문에 안 준다. 그것이 곧 임계치를 좁게 못 잡는 이유이고,
// 다른 엔진·다른 해시 크기로 갈아탈 때 제일 먼저 다시 재야 하는 값이다.
func TestMeasureEvalWobble(t *testing.T) {
	cmd := os.Getenv("SHOWGI_USI_CMD")
	if cmd == "" || os.Getenv("SHOWGI_MEASURE") == "" {
		t.Skip("SHOWGI_USI_CMD · SHOWGI_MEASURE 미설정")
	}

	for _, ply := range []int{27, 79, 85, 91} {
		moves := playtestUpTo103[:ply]

		fresh := NewEngineAnalyst(measurePool(t), nil, intervene.Beginner)
		first, _ := tesujiLoss(t, fresh, moves)
		again, _ := tesujiLoss(t, fresh, moves) // 같은 프로세스, 해시가 이미 그 국면을 안다

		warmed := NewEngineAnalyst(measurePool(t), nil, intervene.Beginner)
		tesujiLoss(t, warmed, playtestUpTo103[:60]) // 다른 국면을 먼저 태운다
		after, _ := tesujiLoss(t, warmed, moves)

		t.Logf("%3d수 %-5s 낙폭: 처음 %+5d · 다시 %+5d · 다른 국면 뒤 %+5d", ply, playtestUpTo103[ply-1], first, again, after)
	}
}
