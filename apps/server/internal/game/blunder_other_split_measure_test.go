package game

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// other 로 남은 수를 룰 엔진의 사실로 가르는 자리다(journal §146).
//
// 묻는 것은 셋이다. 놓은 칸의 교환이 손해인가, 원래 노려지던 내 駒를 그대로 두었나,
// 이 수로 새로 노려지게 된 駒가 있나. 셋 다 엔진 없이 판에서 나온다. 엔진이 있으면
// 상대의 반박 첫 수와 최선수가 무엇을 하는지도 같이 적는다.
//
// 표본은 둘 중 하나에서 온다. DB의 interventions(category=other), 또는
// SHOWGI_MEASURE_ROWS 가 가리키는 JSON Lines 파일(prod 에서 SELECT 로 뽑은 것).
//
//	SHOWGI_MEASURE=1 SHOWGI_TEST_DATABASE_URL=... go test ./internal/game/ -run MeasureOtherSplit -v
//
// 판정하지 않는다. 값을 찍고 지나간다.

// splitRow 는 가를 수 하나다. before 는 착수 전까지의 수순이다.
type splitRow struct {
	Src       string   `json:"src"`
	ID        string   `json:"id"`
	StartSFEN string   `json:"start_sfen"`
	Before    []string `json:"before"`
	Played    string   `json:"played"`
	Category  string   `json:"category"`
	DeltaWin  float64  `json:"delta_win"`
}

func loadSplitRows(t *testing.T) []splitRow {
	t.Helper()
	if os.Getenv("SHOWGI_MEASURE") == "" {
		t.Skip("SHOWGI_MEASURE 미설정")
	}
	if path := os.Getenv("SHOWGI_MEASURE_ROWS"); path != "" {
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("rows: %v", err)
		}
		defer f.Close()
		var out []splitRow
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var r splitRow
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				t.Fatalf("rows: %v", err)
			}
			if r.StartSFEN == "" {
				r.StartSFEN = shogi.StartSFEN
			}
			out = append(out, r)
		}
		return out
	}

	conn := measureDB(t)
	all, moves := loadBlunders(t, conn)
	var out []splitRow
	for _, b := range all {
		if b.category != "other" {
			continue
		}
		gm := moves[b.gameID]
		if _, _, err := replayBlunder(b, gm); err != nil {
			continue
		}
		out = append(out, splitRow{
			Src: "db", ID: fmt.Sprintf("g%d/%d", b.gameID, b.ply),
			StartSFEN: b.startSFEN, Before: append([]string(nil), gm[:b.ply-1]...),
			Played: b.retracted, Category: b.category, DeltaWin: b.deltaWin,
		})
	}
	return out
}

// exposure 는 c 의 駒 중 상대가 따서 이득을 보는 것들이다. 칸 → 이득.
//
// pos 의 수번이 c 의 상대여야 한다.
func exposure(pos shogi.Position, c shogi.Color) map[int]int {
	out := map[int]int{}
	for sq, p := range pos.Board {
		if p.Empty() || p.Color() != c || p.Type() == shogi.King {
			continue
		}
		if g := see(pos, sq); g > 0 {
			out[sq] = g
		}
	}
	return out
}

func maxGain(m map[int]int, skip int) (sq, gain int) {
	sq = -1
	for s, g := range m {
		if s != skip && g > gain {
			sq, gain = s, g
		}
	}
	return sq, gain
}

func sqName(sq int) string {
	if sq < 0 {
		return "-"
	}
	return fmt.Sprintf("%d%d", shogi.FileOf(sq), shogi.RankOf(sq))
}

// splitFacts 는 한 수의 룰 엔진 사실이다.
type splitFacts struct {
	landLoss  int // 놓은 칸의 교환에서 잃는 가치
	leftSq    int // 원래 노려지던 駒를 그대로 둔 칸
	leftGain  int
	newSq     int // 이 수로 새로 노려지게 된 駒의 칸
	newGain   int
	inCheck   bool // 착수 전 王手를 받고 있었다. 「원래 노려지던」을 묻지 못한다
	moverSave bool // 움직인 駒 자신이 노려지던 駒였다(도망친 수)
}

func computeSplit(before shogi.Position, m shogi.Move) splitFacts {
	me := before.Turn
	after := before.Apply(m)
	f := splitFacts{leftSq: -1, newSq: -1}

	f.landLoss = see(after, int(m.To))
	post := exposure(after, me)

	var pre map[int]int
	if before.InCheck(me) {
		f.inCheck = true
	} else {
		flip := before
		flip.Turn = me.Other()
		pre = exposure(flip, me)
		if !m.IsDrop() {
			_, f.moverSave = pre[int(m.From)]
		}
	}

	left, fresh := map[int]int{}, map[int]int{}
	for sq, g := range post {
		if sq == int(m.To) {
			continue
		}
		if _, was := pre[sq]; was && before.Board[sq] == after.Board[sq] {
			left[sq] = g
		} else if !f.inCheck {
			fresh[sq] = g
		}
	}
	f.leftSq, f.leftGain = maxGain(left, -1)
	f.newSq, f.newGain = maxGain(fresh, -1)
	return f
}

func TestMeasureOtherSplit(t *testing.T) {
	rows := loadSplitRows(t)

	var pool = func() *engineAnalyst {
		if os.Getenv("SHOWGI_USI_CMD") == "" {
			return nil
		}
		p := measurePool(t)
		return &engineAnalyst{search: p, depth: JudgeDepth, breadth: 1, level: intervene.Beginner}
	}()

	type out struct {
		r      splitRow
		f      splitFacts
		bucket string
		engine string
	}
	var outs []out

	for _, r := range rows {
		pos, err := positionAfter(r.StartSFEN, r.Before)
		if err != nil {
			t.Logf("  %s: 국면 복원 실패: %v", r.ID, err)
			continue
		}
		m, err := shogi.ParseUSIMove(r.Played)
		if err != nil || pos.ValidateMove(m) != nil {
			t.Logf("  %s: 둘 수 없는 수 %s", r.ID, r.Played)
			continue
		}
		f := computeSplit(pos, m)
		o := out{r: r, f: f}
		switch {
		case f.landLoss > 0:
			o.bucket = "A 놓은 칸의 교환 손해"
		case f.leftGain > 0:
			o.bucket = "B 노려지던 駒를 방치"
		case f.newGain > 0:
			o.bucket = "C 이 수로 駒가 노려짐"
		case f.inCheck:
			o.bucket = "D 王手 받던 중"
		default:
			o.bucket = "E 판에 드러난 손해 없음"
		}

		if pool != nil {
			var ok string
			o.engine, ok = engineNotes(t, pool, r, pos, m, f)
			if ok != "" {
				o.bucket += " ✓" + ok
			}
		}
		outs = append(outs, o)
	}

	counts := map[string]int{}
	for _, o := range outs {
		counts[o.bucket]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	t.Logf("== other %d건을 판의 사실로 갈랐다 ==", len(outs))
	for _, k := range keys {
		t.Logf("  %-28s %3d", k, counts[k])
	}
	t.Logf("\n  %-10s %-7s %-6s %-5s %-9s %-9s %-4s %-26s %s", "id", "수", "Δwin", "land", "left", "new", "도망", "bucket", "engine")
	for _, o := range outs {
		t.Logf("  %-10s %-7s %-6.3f %-5d %-9s %-9s %-4v %-26s %s",
			o.r.ID, o.r.Played, o.r.DeltaWin, o.f.landLoss,
			fmt.Sprintf("%s:%d", sqName(o.f.leftSq), o.f.leftGain),
			fmt.Sprintf("%s:%d", sqName(o.f.newSq), o.f.newGain),
			o.f.moverSave, o.bucket, o.engine)
	}
}

// engineNotes 는 반박 첫 수와 최선수가 판에서 무엇을 하는지 적는다.
//
// 두 번째 값은 판의 사실을 엔진이 뒷받침하는가다. A 는 반박이 놓은 칸을 딴다. B 는
// 반박이 방치한 駒를 따고, 최선수 뒤에는 그 駒가 더는 이득으로 따이지 않는다.
func engineNotes(t *testing.T, a *engineAnalyst, r splitRow, pos shogi.Position, m shogi.Move, f splitFacts) (string, string) {
	t.Helper()
	ctx := t.Context()
	me := pos.Turn
	after := pos.Apply(m)
	played := append(append([]string(nil), r.Before...), r.Played)

	var notes []string
	r0To := -1
	if res, err := a.searchAt(ctx, r.StartSFEN, played); err == nil && len(res.PV) > 0 {
		notes = append(notes, "반박 "+describeMove(after, res.PV[0]))
		if rm, err := shogi.ParseUSIMove(res.PV[0]); err == nil && !after.Board[rm.To].Empty() {
			r0To = int(rm.To)
		}
	}
	leftSafe := false
	if res, err := a.searchAt(ctx, r.StartSFEN, r.Before); err == nil && res.Best != "" {
		d := "최선 " + describeMove(pos, res.Best)
		if bm, err := shogi.ParseUSIMove(res.Best); err == nil && pos.ValidateMove(bm) == nil {
			exp := exposure(pos.Apply(bm), me)
			_, g := maxGain(exp, -1)
			d += fmt.Sprintf(" (뒤 노출 %d)", g)
			if f.leftSq >= 0 {
				_, still := exp[f.leftSq]
				moved := !bm.IsDrop() && int(bm.From) == f.leftSq
				leftSafe = !still || moved
			}
		}
		notes = append(notes, d)
	}
	ok := ""
	switch {
	case f.landLoss > 0 && r0To == int(m.To):
		ok = "A"
	case f.leftGain > 0 && r0To == f.leftSq && leftSafe:
		ok = "B"
	}
	return strings.Join(notes, " · "), ok
}

// describeMove 는 그 수가 따는가·王手인가·成るか 를 짧게 적는다.
func describeMove(pos shogi.Position, u string) string {
	m, err := shogi.ParseUSIMove(u)
	if err != nil || pos.ValidateMove(m) != nil {
		return u + "?"
	}
	s := u
	if cap := pos.Board[m.To]; !cap.Empty() {
		s += "×" + shogi.PieceJa(cap.Type())
	}
	if m.Promote {
		s += "+成"
	}
	if next := pos.Apply(m); next.InCheck(pos.Turn.Other()) {
		s += "+王手"
	}
	return s
}

// TestMeasureOtherConvince 는 프로덕션 반박 트리(proveLoss)를 기록 위에서 돌린다.
//
// 상대의 최선수는 카드와 같은 질문(cardPV)으로 구한다. 詰み 끝점은 SHOWGI_MATE_CMD 가
// 있을 때만 증명된다. 거울상(최선수였다면 상대가 어떻게 받아도 駒를 얻었나)도 같이 잰다.
// SHOWGI_PROOF_TURNS · SHOWGI_PROOF_SEARCHES 로 상한을 넓혀 볼 수 있다.
func TestMeasureOtherConvince(t *testing.T) {
	rows := loadSplitRows(t)
	cmd := os.Getenv("SHOWGI_USI_CMD")
	if cmd == "" {
		t.Skip("SHOWGI_USI_CMD 미설정")
	}
	if v, err := strconv.Atoi(os.Getenv("SHOWGI_PROOF_TURNS")); err == nil {
		proofTurns = v
	}
	if v, err := strconv.Atoi(os.Getenv("SHOWGI_PROOF_SEARCHES")); err == nil {
		proofSearches = v
	}
	const workers = 4
	pool, err := usi.NewPool(workers, cmd, map[string]string{
		"USI_Hash": "128", "Threads": "1", "FV_SCALE": "24",
		"BookFile": "no_book", "USI_OwnBook": "false",
	})
	if err != nil {
		t.Fatalf("엔진 풀: %v", err)
	}
	t.Cleanup(pool.Close)
	var mate MateSearcher
	if mc := os.Getenv("SHOWGI_MATE_CMD"); mc != "" {
		mp, err := usi.NewPool(workers, mc, map[string]string{"USI_Hash": "128", "Threads": "1", "DepthLimit": "11"})
		if err != nil {
			t.Fatalf("詰み 풀: %v", err)
		}
		t.Cleanup(mp.Close)
		mate = mp
	}

	type result struct {
		ok, gain  bool
		spent     int
		lossSpent int
		text, why string
		gainText  string
	}
	results := make([]result, len(rows))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				r := rows[i]
				cp := &countingPool{pool: pool}
				a := &engineAnalyst{search: cp, mate: mate, depth: JudgeDepth, level: intervene.Beginner}
				played := append(append([]string(nil), r.Before...), r.Played)
				before, err := positionAfter(r.StartSFEN, r.Before)
				if err != nil {
					continue
				}
				var res result
				if pv := a.cardPV(t.Context(), r.StartSFEN, played); len(pv) > 0 {
					best, losses, ok, why := a.prove(t.Context(), r.StartSFEN, played, pv[0], before.Turn, before)
					res.ok, res.why = ok, why
					if ok {
						res.text = explain.Render(explain.Facts{
							Kind: intervene.KindBlunder, Category: intervene.CategoryForcedLoss, Known: true,
							OpponentBest: best, Losses: losses,
						})
					}
				}
				res.lossSpent = cp.n
				// 거울상. 최선수를 두었다면 상대가 어떻게 받아도 駒를 얻었는가.
				if sr, err := a.searchAt(t.Context(), r.StartSFEN, r.Before); err == nil && sr.Best != "" && sr.Best != r.Played {
					best, gains, ok, _ := a.prove(t.Context(), r.StartSFEN, r.Before, sr.Best, before.Turn.Other(), before)
					if ok {
						res.gain = true
						res.gainText = best + " : " + explain.Render(explain.Facts{
							Kind: intervene.KindBlunder, Category: intervene.CategoryForcedLoss, Known: true,
							OpponentBest: best, Losses: gains,
						})
					}
				}
				res.spent = cp.n
				results[i] = res
			}
		}()
	}
	for i := range rows {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	var loss, gain, both, spent, maxSpent, lossSpent, maxLoss int
	whys := map[string]int{}
	for i, r := range rows {
		res := results[i]
		spent += res.spent
		maxSpent = max(maxSpent, res.spent)
		lossSpent += res.lossSpent
		maxLoss = max(maxLoss, res.lossSpent)
		switch {
		case res.ok && res.gain:
			both++
		case res.ok:
			loss++
		case res.gain:
			gain++
		default:
			w := res.why
			if strings.HasPrefix(w, "turns") {
				w = "turns"
			}
			whys[w]++
		}
		mark := "  "
		if res.ok {
			mark = "L "
		} else if res.gain {
			mark = "G "
		}
		t.Logf("  %s%-6s %-10s %-7s Δ%.3f 탐색%-3d %s %s", mark, r.Src, r.ID, r.Played, r.DeltaWin, res.spent,
			strings.ReplaceAll(res.text, "\n", " / "), strings.ReplaceAll(res.gainText, "\n", " / "))
	}
	t.Logf("\n== 반박 트리 (ProofLoss=%d, 응수 %d+되따기, %d차례, 상한 %d회) ==",
		ProofLoss, OtherBranches, proofTurns, proofSearches)
	t.Logf("  잃는다만 %d · 놓친 이득만 %d · 둘 다 %d · 어느 쪽도 아님 %d / %d", loss, gain, both, len(rows)-loss-gain-both, len(rows))
	t.Logf("  잃는다 쪽이 닿지 못한 이유 %v", whys)
	t.Logf("  엔진 호출 — 잃는다 트리(cardPV 포함) 평균 %.1f회 · 최대 %d회, 두 트리 합 평균 %.1f회 · 최대 %d회",
		float64(lossSpent)/float64(max(1, len(rows))), maxLoss, float64(spent)/float64(max(1, len(rows))), maxSpent)
}

// countingPool 은 엔진 호출 수를 센다. 트리가 카드 지연에 얹는 몫이 이것이다.
type countingPool struct {
	pool *usi.Pool
	n    int
}

func (c *countingPool) SearchDepth(ctx context.Context, start string, moves []string, depth int) (usi.SearchResult, error) {
	c.n++
	return c.pool.SearchDepth(ctx, start, moves, depth)
}

func (c *countingPool) SearchMultiPV(ctx context.Context, start string, moves []string, depth, k int) (usi.SearchResult, error) {
	c.n++
	return c.pool.SearchMultiPV(ctx, start, moves, depth, k)
}
