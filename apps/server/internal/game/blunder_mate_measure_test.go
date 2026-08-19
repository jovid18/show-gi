package game

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 「other 로 떨어진 수는 사실 詰まされる 수 아닌가」를 재는 자리다(journal §40).
//
// other 54건의 중앙값이 대국의 88% 지점이다 — 분기들이 보는 중반의 모양(タダ捨て·駒得·
// 王手)이 아니라 종반의 언어로 나빠진 수라는 뜻이다. 종반에서 「왜 나쁜가」는 대개
// 하나뿐이다: 그 수로 詰まされる.
//
// 재야 하는 것은 세 가지가 갈린다는 점이다.
//
//	① 둔 수 뒤에 상대의 詰み이 있다          → 詰まされる 수
//	② 최선수 뒤에도 상대의 詰み이 있다        → 이미 진 국면이다. 그 수의 죄가 아니다
//	③ ① 인데 ② 가 아니다                    → 이 수가 詰み을 불렀다. 여기만 말해도 된다
//
// ②를 안 가르고 「この手で詰まされます」를 내보내면 이미 詰んでいた 국면에서 거짓말을
// 한다. 초심자는 검증할 수단이 없어 그대로 배운다 — 이 제품에서 가장 큰 실패다.
//
// 엔진과 DB가 동시에 필요하다. 엔진은 arm64 Debian 바이너리라 macOS에서 직접 못 돌고,
// db는 컨테이너라 컨테이너 안에서 localhost 로는 안 보인다 — 돌리는 방법은
// [README](../../README.md) ④에 있다.
//
//	go test ./internal/game/ -run MeasureBlunderMate -v -timeout 60m
//
// 판정하지 않는다 — 값을 찍고 지나간다.

// measureMatePool 은 詰将棋 solver 풀이다. 탐색부와 다른 바이너리다 — 탐색 엔진에
// go mate 를 보내면 checkmate 대신 bestmove 가 돌아온다(02-architecture.md §3).
//
// DepthLimit 은 프로덕션과 같은 11로 둔다. 여기서 다른 값을 쓰면 재는 것이 프로덕션과
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

// mateVerdict 는 한 국면에서 수번 측이 상대를 詰ます 수 있는가다.
//
// 착수 후 국면에 물으면 수번은 상대이므로, 답은 「상대가 나를 詰ます」가 된다.
// analyst.go 가 이 호출을 안 쓰는 것은 거기서 알아야 하는 것이 그 반대(내 詰み이 남았나)
// 이기 때문이고, 지금 묻는 것에는 정확히 이 호출이 맞는 답을 준다.
type mateVerdict struct {
	found  bool
	proven bool
	plies  int
}

func askMate(t *testing.T, mate *usi.Pool, startSFEN string, moves []string) mateVerdict {
	t.Helper()
	r, err := mate.SearchMate(t.Context(), startSFEN, moves)
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
	ctx := t.Context()

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

		// 착수 후 국면의 일반 탐색. 이 값은 프로덕션이 이미 손에 들고 있다 —
		// analyst.go 가 after 로 부르는 바로 그 탐색이고, MateIn 이 양수인 경우를
		// 지금은 쓰지 않고 버린다.
		if res, err := pool.SearchDepth(ctx, b.startSFEN, played, JudgeDepth); err == nil {
			if res.IsMate && res.MateIn > 0 {
				r.searchMateIn = res.MateIn
			}
		}

		r.afterMate = askMate(t, mate, b.startSFEN, played)

		// 최선수 뒤에도 詰まされるか. 이것이 ②를 가른다.
		if res, err := pool.SearchDepth(ctx, b.startSFEN, before, JudgeDepth); err == nil && res.Best != "" {
			r.best = res.Best
			bestLine := append(append([]string(nil), before...), res.Best)
			r.bestMate = askMate(t, mate, b.startSFEN, bestLine)
		}

		rows = append(rows, r)
	}

	var caused, alreadyLost, noMate, searchOnly, unknown int
	t.Logf("== `other` %d건에 詰み을 물었다 ==", len(rows))
	t.Logf("  %-6s %-5s %-9s %-12s %-12s %-10s", "game", "ply", "Δwin", "둔 수 뒤", "최선수 뒤", "탐색 mate")
	for _, r := range rows {
		desc := func(v mateVerdict) string {
			switch {
			case v.found:
				return "詰み " + strconv.Itoa(v.plies) + "手"
			case v.proven:
				return "なし(증명)"
			default:
				return "불명"
			}
		}
		sm := "-"
		if r.searchMateIn > 0 {
			sm = strconv.Itoa(r.searchMateIn) + "手"
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
		// 「증명된 なし」와 「모른다」를 같은 칸에 세지 않는다. solver가 한계 안에서
		// 결론을 못 낸 것(timeout)은 「詰み이 없다」가 아니다. 섞어 세면 이 측정이
		// 하지 말라고 적어 둔 바로 그것 — 모르는 것을 아는 것처럼 세는 일 — 을 한다.
		case !r.afterMate.proven:
			unknown++
		default:
			noMate++
		}
	}

	t.Logf("\n== 갈래 ==")
	t.Logf("  ③ 이 수가 詰み을 불렀다 (말해도 된다)   %3d / %d", caused, len(rows))
	t.Logf("  ② 최선수로도 詰まされる (이미 졌다)     %3d / %d", alreadyLost, len(rows))
	t.Logf("  탐색만 詰み을 본다 (solver 는 못 찾음)  %3d / %d", searchOnly, len(rows))
	t.Logf("  詰み 아님 (증명됨)                      %3d / %d", noMate, len(rows))
	t.Logf("  모른다 (solver가 결론을 못 냈다)        %3d / %d", unknown, len(rows))
}

// TestMeasureLetsMateOnRecords 는 프로덕션 경로 그대로 기록을 다시 판정한다.
//
// 위의 두 측정은 「詰み이 있느냐」를 직접 물었다. 이것은 NewEngineAnalyst 를 세워
// Judge 를 부르므로, 플레이어가 실제로 보게 될 카테고리와 문장이 나온다 — 배선이
// 어딘가 빠져 있으면 여기서만 드러난다.
//
// 낙폭이 다시 임계치를 넘는지는 별개다. 같은 국면·같은 깊이가 같은 값을 안 주므로
// (journal §39 ⑦) 그때 걸린 수가 지금은 안 걸릴 수 있다 — 그 건수도 같이 센다.
func TestMeasureLetsMateOnRecords(t *testing.T) {
	conn := measureDB(t)
	pool := measurePool(t)
	mate := measureMatePool(t)

	all, moves := loadBlunders(t, conn)
	an := NewEngineAnalyst(pool, mate, intervene.Beginner)

	counts := map[intervene.Category]int{}
	var notTripped int
	var samples []string

	for _, b := range all {
		if b.category != "other" {
			continue
		}
		gm := moves[b.gameID]
		if _, _, err := replayBlunder(b, gm); err != nil {
			continue
		}
		played := append(append([]string(nil), gm[:b.ply-1]...), b.retracted)

		j, err := an.Judge(t.Context(), b.startSFEN, played, b.ply)
		if err != nil {
			t.Logf("  game %d ply %d: 판정 실패: %v", b.gameID, b.ply, err)
			continue
		}
		if j.Verdict.Kind == intervene.KindNone {
			notTripped++
			continue
		}
		counts[j.Verdict.Category]++
		if j.Verdict.Category == intervene.CategoryLetsMate && len(samples) < 6 {
			samples = append(samples, fmt.Sprintf("game %d ply %d → %s (%d手 · 반박 %d手)",
				b.gameID, b.ply, explain.Render(j.Facts), j.Facts.MatePlies, len(j.Refutation)))
		}
	}

	t.Logf("== 옛 `other` 를 지금 코드로 다시 판정했다 ==")
	for c, n := range counts {
		name := string(c)
		if c == intervene.CategoryOther {
			name += " (그대로)"
		}
		t.Logf("  %-16s %3d", name, n)
	}
	t.Logf("  %-16s %3d  ← 낙폭이 다시 임계치를 안 넘었다(§39 ⑦)", "판정 안 걸림", notTripped)

	t.Logf("\n== 플레이어가 보게 될 문장 ==")
	for _, s := range samples {
		t.Logf("  %s", s)
	}
}
