package game

import (
	"context"
	"os"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 「`other` 로 떨어진 수는 사실 **詰まされる 수** 아닌가」를 재는 자리다(06-status.md §40).
//
// `other` 54건의 중앙값이 대국의 88% 지점이다 — 분기들이 보는 중반의 모양(タダ捨て·駒得·
// 王手)이 아니라 **종반의 언어로 나빠진 수**라는 뜻이다. 종반에서 「왜 나쁜가」는 대개
// 하나뿐이다: 그 수로 詰まされる.
//
// **재야 하는 것은 세 가지가 갈린다는 점이다.**
//
//	① 둔 수 뒤에 상대의 詰み이 있다          → 詰まされる 수
//	② 최선수 뒤에도 상대의 詰み이 있다        → 이미 진 국면이다. 그 수의 죄가 아니다
//	③ ① 인데 ② 가 아니다                    → **이 수가 詰み을 불렀다.** 여기만 말해도 된다
//
// ②를 안 가르고 「この手で詰まされます」를 내보내면 **이미 詰んでいた 국면에서 거짓말**을
// 한다. 초심자는 검증할 수단이 없어 그대로 배운다 — 이 제품에서 가장 큰 실패다.
//
//	docker run --rm --network show-gi-net -v "$PWD:/src" -w /src/apps/server \
//	  -e SHOWGI_USI_CMD=/opt/yaneuraou/run -e SHOWGI_MATE_CMD=/opt/yaneuraou/run-mate \
//	  -e SHOWGI_MEASURE=1 \
//	  -e SHOWGI_TEST_DATABASE_URL='postgres://showgi:showgi@show-gi-db:5432/showgi' \
//	  show-gi-enginetest:latest go test ./internal/game/ -run MeasureBlunderMate -v -timeout 60m
//
// 판정하지 않는다 — 값을 찍고 지나간다.

// measureMatePool 은 詰将棋 solver 풀이다. **탐색부와 다른 바이너리다** — 탐색 엔진에
// `go mate` 를 보내면 checkmate 대신 bestmove 가 돌아온다(02-architecture.md §3).
//
// `DepthLimit` 은 프로덕션과 같은 11로 둔다. 여기서 다른 값을 쓰면 재는 것이 프로덕션과
// 달라지고, 그 어긋남은 문서의 숫자로만 나타나 아무 데서도 안 터진다.
func measureMatePool(t *testing.T) *usi.Pool {
	t.Helper()
	cmd := os.Getenv("SHOWGI_MATE_CMD")
	if cmd == "" || os.Getenv("SHOWGI_MEASURE") == "" {
		t.Skip("SHOWGI_MATE_CMD · SHOWGI_MEASURE 미설정")
	}
	pool, err := usi.NewPool(1, cmd, map[string]string{
		"USI_Hash": "128", "Threads": "1", "DepthLimit": "11",
	})
	if err != nil {
		t.Fatalf("詰み 풀: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// mateVerdict 는 한 국면에서 **수번 측이 상대를 詰ます 수 있는가**다.
//
// 착수 **후** 국면에 물으면 수번은 상대이므로, 답은 「상대가 나를 詰ます」가 된다.
// analyst.go 가 이 호출을 안 쓰는 것은 거기서 알아야 하는 것이 그 반대(내 詰み이 남았나)
// 이기 때문이고, **지금 묻는 것에는 정확히 이 호출이 맞는 답을 준다.**
type mateVerdict struct {
	found  bool
	proven bool
	plies  int
}

func askMate(t *testing.T, mate *usi.Pool, startSFEN string, moves []string) mateVerdict {
	t.Helper()
	r, err := mate.SearchMate(context.Background(), startSFEN, moves)
	if err != nil {
		t.Logf("  詰み 탐색 실패: %v", err)
		return mateVerdict{}
	}
	return mateVerdict{found: r.Found(), proven: r.Proven, plies: len(r.Moves)}
}

func TestMeasureBlunderMate(t *testing.T) {
	conn := measureDB(t)
	pool := measurePool(t)
	mate := measureMatePool(t)

	all, moves := loadBlunders(t, conn)
	ctx := context.Background()

	type row struct {
		b            blunderRow
		afterMate    mateVerdict // 둔 수 뒤 — 상대가 나를 詰ます 수 있는가
		bestMate     mateVerdict // 최선수 뒤 — 그래도 詰まされるか (=이미 진 국면)
		searchMateIn int         // depth 12 탐색이 본 詰み. 양수 = 상대가 나를 詰ます
		best         string
	}
	var rows []row

	for _, b := range all {
		if b.category != "other" {
			continue
		}
		gm := moves[b.gameID]
		if _, _, err := replayBlunder(b, gm); err != nil {
			continue
		}
		before := append([]string(nil), gm[:b.ply-1]...)
		played := append(append([]string(nil), before...), b.retracted)

		r := row{b: b}

		// 착수 후 국면의 일반 탐색. **이 값은 프로덕션이 이미 손에 들고 있다** —
		// analyst.go 가 `after` 로 부르는 바로 그 탐색이고, MateIn 이 양수인 경우를
		// 지금은 쓰지 않고 버린다.
		if res, err := pool.SearchDepth(ctx, b.startSFEN, played, JudgeDepth); err == nil {
			if res.IsMate && res.MateIn > 0 {
				r.searchMateIn = res.MateIn
			}
		}

		r.afterMate = askMate(t, mate, b.startSFEN, played)

		// 최선수 뒤에도 詰まされるか. **이것이 ②를 가른다.**
		if res, err := pool.SearchDepth(ctx, b.startSFEN, before, JudgeDepth); err == nil && res.Best != "" {
			r.best = res.Best
			bestLine := append(append([]string(nil), before...), res.Best)
			r.bestMate = askMate(t, mate, b.startSFEN, bestLine)
		}

		rows = append(rows, r)
	}

	var caused, alreadyLost, noMate, searchOnly int
	t.Logf("== `other` %d건에 詰み을 물었다 ==", len(rows))
	t.Logf("  %-6s %-5s %-9s %-12s %-12s %-10s", "game", "ply", "Δwin", "둔 수 뒤", "최선수 뒤", "탐색 mate")
	for _, r := range rows {
		desc := func(v mateVerdict) string {
			switch {
			case v.found:
				return "詰み " + itoa(v.plies) + "手"
			case v.proven:
				return "なし(증명)"
			default:
				return "불명"
			}
		}
		sm := "-"
		if r.searchMateIn > 0 {
			sm = itoa(r.searchMateIn) + "手"
		}
		t.Logf("  %-6d %-5d %-9.3f %-12s %-12s %-10s",
			r.b.gameID, r.b.ply, r.b.deltaWin, desc(r.afterMate), desc(r.bestMate), sm)

		switch {
		case r.afterMate.found && !r.bestMate.found:
			caused++
		case r.afterMate.found && r.bestMate.found:
			alreadyLost++
		case !r.afterMate.found && r.searchMateIn > 0:
			searchOnly++
		default:
			noMate++
		}
	}

	t.Logf("\n== 갈래 ==")
	t.Logf("  ③ 이 수가 詰み을 불렀다 (말해도 된다)   %3d / %d", caused, len(rows))
	t.Logf("  ② 최선수로도 詰まされる (이미 졌다)     %3d / %d", alreadyLost, len(rows))
	t.Logf("  탐색만 詰み을 본다 (solver 는 못 찾음)  %3d / %d", searchOnly, len(rows))
	t.Logf("  詰み 아님                               %3d / %d", noMate, len(rows))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
