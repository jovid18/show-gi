package game

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// **게이트가 왜 한 번도 안 열렸는지를 단계로 가른다. 그리고 k를 고른다.**
//
// 사람이 끝까지 둔 두 판에서 제안형 手筋(`kind='tesuji'`)이 **0건**이었고, 그것이 「후보가
// 없어서」인지 「엔진이 다 떨어뜨려서」인지 「시한을 넘겨서」인지가 안 갈려 있었다
// (journal §45 · §56). §56은 **비용**만 쟀다 — 이 테스트가 재는 것은 **결과**다.
//
//	SHOWGI_MEASURE=1 SHOWGI_USI_CMD=/opt/yaneuraou/run \
//	  go test ./internal/game/ -run MeasureTesujiHintGate -v -timeout 60m
//
// 세션을 세우지 않고 `maybeTesujiHint` 의 조건을 그대로 다시 쓴다. 세션으로 돌리면 사람의
// 착수 시각을 흉내내야 하는데 기보에 시각이 없어서, 그 흉내가 곧 결론이 되어 버린다.
//
// **k마다 엔진을 새로 띄운다.** 치환표가 더워진 채로 다음 k를 재면 뒤에 오는 k가 공짜로
// 보이고, 고르려는 것이 바로 그 비용이다(§58이 k=1 → k=3에서 겪은 자리).
func TestMeasureTesujiHintGate(t *testing.T) {
	if os.Getenv("SHOWGI_MEASURE") == "" {
		t.Skip("SHOWGI_MEASURE 미설정")
	}
	cmd := os.Getenv("SHOWGI_USI_CMD")
	if cmd == "" {
		t.Skip("SHOWGI_USI_CMD 미설정")
	}

	moves := humanOneKifu(t)
	for _, k := range []int{3, 6, 8, 12} {
		t.Run("k="+strconv.Itoa(k), func(t *testing.T) { measureTesujiGate(t, cmd, moves, k, shogi.Black) })
	}
}

// **§74 뒤에 사람이 처음 둔 판이다.** 그 절이 k=3을 고른 근거는 회차 1의 기보 하나였고,
// 회차 3은 화면에서 **0건**이 나왔다 — 후보 12개가 전부 「모르는 채」로 침묵했다
// (journal §76 ④). 그래서 갈라야 할 것이 하나 남아 있다: **이 판에 통과할 手筋이
// 애초에 없었는가, 아니면 k=3 줄 밖이라 못 물었는가.**
//
// k를 올리면 그 12개가 판정을 **받기는** 한다. 받는 것과 통과하는 것은 다르고, 회차 1의
// 기보에서는 그 차이가 0이었다(k=3·6·12에서 뜬 이름이 셋으로 같다). 이 판은 안 재봤다.
//
// **사람이 後手다** — 위 판과 갈리는 유일한 인자이고, 넘기지 않으면 상대의 차례를 재게 된다.
func TestMeasureTesujiHintGateHuman3(t *testing.T) {
	if os.Getenv("SHOWGI_MEASURE") == "" {
		t.Skip("SHOWGI_MEASURE 미설정")
	}
	cmd := os.Getenv("SHOWGI_USI_CMD")
	if cmd == "" {
		t.Skip("SHOWGI_USI_CMD 미설정")
	}

	moves := humanThreeKifu(t)
	for _, k := range []int{3, 6} {
		t.Run("k="+strconv.Itoa(k), func(t *testing.T) { measureTesujiGate(t, cmd, moves, k, shogi.White) })
	}
}

func measureTesujiGate(t *testing.T, cmd string, moves []string, k int, human shogi.Color) {
	pool, err := usi.NewPool(1, cmd, map[string]string{
		"USI_Hash": "128", "Threads": "1", "FV_SCALE": "24",
		"BookFile": "no_book", "USI_OwnBook": "false",
	})
	if err != nil {
		t.Fatalf("엔진 풀: %v", err)
	}
	defer pool.Close()

	pos, err := shogi.ParseSFEN(shogi.StartSFEN)
	if err != nil {
		t.Fatalf("초기 국면: %v", err)
	}

	var (
		turns     int // 사람 차례
		asked     int // 쿨다운·상한을 지나 실제로 물어본 회차
		withCands int // 그중 룰 필터가 후보를 낸 회차
		opened    int // 그중 엔진 게이트를 통과한 것이 하나라도 있던 회차

		cands, kept, undecided int
		failed                 int

		gateTotal, gateWorst time.Duration
		hintCount            int

		lastAsk    int
		everAsked  bool
		firstNames []string
	)

	for i, u := range moves {
		if pos.Turn == human {
			ply := i
			turns++

			// **maybeTesujiHint 의 순서 그대로다.** 상한 → 쿨다운 → 룰 필터 → 엔진.
			// 쿨다운은 「물어본 자리」에서 재므로 후보가 없던 회차도 자리를 쓴다(§56).
			switch {
			case hintCount >= TagHintMaxPerGame:
			case everAsked && ply-lastAsk < TagHintCooldown:
			default:
				asked++
				lastAsk, everAsked = ply, true

				opts := tesujiOptions(pos, human)
				if len(opts) > 0 {
					withCands++
					cands += len(opts)

					// 프로덕션과 같은 시한 안에서 잰다 — 넘긴 회차가 몇인지가 답의 일부다.
					hctx, cancel := context.WithTimeout(t.Context(), DefaultExtraDeadline)
					start := time.Now()
					got, d, err := gateTesujiOptions(hctx, pool, JudgeDepth, k, shogi.StartSFEN, moves[:i], opts, human)
					spent := time.Since(start)
					cancel()

					gateTotal += spent
					if spent > gateWorst {
						gateWorst = spent
					}
					kept += len(got)
					undecided += d
					if err != nil {
						failed++
					}
					if len(got) > 0 {
						opened++
						hintCount++
						firstNames = append(firstNames, tagCodes(got))
					}
					t.Logf("%3d手目  후보 %2d → 통과 %d %s (모름 %d, %v%s)",
						ply, len(opts), len(got), tagCodes(got), d, spent.Round(time.Millisecond), errSuffix(err))
				}
			}
		}

		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			t.Fatalf("%d手目 %q: %v", i+1, u, err)
		}
		pos = pos.Apply(m)
	}

	t.Logf("k=%d  사람 차례 %d회 · 물어본 회차 %d · 후보가 있던 회차 %d · 이름이 뜬 회차 %d %v",
		k, turns, asked, withCands, opened, firstNames)
	t.Logf("k=%d  후보 %d개 → 통과 %d개 · 모르는 채 남은 것 %d개 · 탐색이 실패한 회차 %d",
		k, cands, kept, undecided, failed)
	if withCands > 0 {
		t.Logf("k=%d  게이트 한 회차 평균 %v · 최장 %v",
			k, (gateTotal / time.Duration(withCands)).Round(time.Millisecond), gateWorst.Round(time.Millisecond))
	}
}

func errSuffix(err error) string {
	if err == nil {
		return ""
	}
	return ", " + err.Error()
}

func tagCodes(opts []TesujiOption) string {
	var out []string
	for _, o := range opts {
		for _, t := range o.Tags {
			out = append(out, o.USI+":"+t.Code)
		}
	}
	if len(out) == 0 {
		return ""
	}
	return "[" + strings.Join(out, " ") + "]"
}
