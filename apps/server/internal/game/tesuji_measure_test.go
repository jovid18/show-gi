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
	analyst := NewEngineAnalyst(measurePool(t), nil, intervene.Beginner)

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
	}
	t.Logf("형태 %d개 · 이름이 붙은 수 %d개 · 서로 다른 이름 %d개 (한계 %dcp)", shapes, named, len(seen), TesujiLossCp)
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
