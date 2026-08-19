package game

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// journal §40의 숫자를 만든 자리다. other 가 개입의 절반을 넘는데(54/92),
// 거기로 가는 길이 둘인 것을 DB가 못 가른다 — !Known(사실을 못 구했다)과
// default:(구했는데 안 맞았다)가 똑같이 'other' 로 저장된다. 둘은 완전히 다른
// 문제라 처방이 갈리므로, 먼저 가르지 않으면 어떤 처방도 근거가 없다.
//
// 국면은 남아 있다 — games.start_sfen + game_moves + interventions.retracted_usi 로
// 물러진 수의 국면이 그대로 복원된다. 되무른 수는 game_moves 에 안 남으므로
// ply 미만의 수를 놓은 자리가 곧 착수 전 국면이다.
//
//	SHOWGI_TEST_DATABASE_URL='postgres://showgi:showgi@localhost:5432/showgi' \
//	  SHOWGI_MEASURE=1 go test ./internal/game/ -run MeasureBlunderOther -v
//
// 판정하지 않는다 — 값을 찍고 지나간다. 프로덕션 데이터를 읽는 측정이라 판을 세우면
// 대국이 쌓일 때마다 CI가 빨개진다.

// blunderRow 는 개입 한 건과 그것을 복원하는 데 필요한 전부다.
type blunderRow struct {
	id        int64
	gameID    int64
	ply       int
	category  string
	retracted string
	deltaWin  float64
	startSFEN string
	gameLen   int // 그 대국의 총 手数. 「종반에 몰려 있는가」를 재는 데 쓴다
}

func measureDB(t *testing.T) *pgx.Conn {
	t.Helper()
	url := os.Getenv("SHOWGI_TEST_DATABASE_URL")
	if url == "" || os.Getenv("SHOWGI_MEASURE") == "" {
		t.Skip("SHOWGI_TEST_DATABASE_URL · SHOWGI_MEASURE 미설정")
	}
	conn, err := pgx.Connect(context.Background(), url)
	if err != nil {
		t.Fatalf("DB 연결: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// loadBlunders 는 되무른 수 전부와 그 대국의 수순을 읽어 온다.
//
// 읽기만 한다. 다섯 워크트리가 같은 데이터베이스를 본다.
func loadBlunders(t *testing.T, conn *pgx.Conn) ([]blunderRow, map[int64][]string) {
	t.Helper()
	ctx := context.Background()

	moves := map[int64][]string{}
	rows, err := conn.Query(ctx, `SELECT game_id, usi FROM game_moves ORDER BY game_id, ply`)
	if err != nil {
		t.Fatalf("game_moves: %v", err)
	}
	for rows.Next() {
		var gid int64
		var usi string
		if err := rows.Scan(&gid, &usi); err != nil {
			t.Fatalf("game_moves scan: %v", err)
		}
		moves[gid] = append(moves[gid], usi)
	}
	rows.Close()

	var out []blunderRow
	rows, err = conn.Query(ctx, `
		SELECT i.id, i.game_id, i.ply, COALESCE(i.category, ''), COALESCE(i.retracted_usi, ''),
		       COALESCE(i.delta_win, 0), COALESCE(g.start_sfen, '')
		FROM interventions i JOIN games g ON g.id = i.game_id
		WHERE i.kind = 'blunder'
		ORDER BY i.game_id, i.ply, i.id`)
	if err != nil {
		t.Fatalf("interventions: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var b blunderRow
		if err := rows.Scan(&b.id, &b.gameID, &b.ply, &b.category, &b.retracted, &b.deltaWin, &b.startSFEN); err != nil {
			t.Fatalf("interventions scan: %v", err)
		}
		if b.startSFEN == "" {
			b.startSFEN = shogi.StartSFEN
		}
		b.gameLen = len(moves[b.gameID])
		out = append(out, b)
	}
	return out, moves
}

// replayBlunder 는 되무른 수의 착수 전 국면과 그 한 수를 복원한다.
//
// ply 는 그 수가 놓였을 자리의 번호다(session.go 의 len(st.usis)+1). 되무른 수는
// game_moves 에 안 남으므로 앞의 ply-1 수가 그대로 착수 전 국면이 된다.
func replayBlunder(b blunderRow, moves []string) (shogi.Position, shogi.Move, error) {
	if b.retracted == "" {
		return shogi.Position{}, shogi.Move{}, fmt.Errorf("retracted_usi 없음")
	}
	if b.ply < 1 || b.ply-1 > len(moves) {
		return shogi.Position{}, shogi.Move{}, fmt.Errorf("ply %d 가 수순 %d 를 벗어난다", b.ply, len(moves))
	}
	pos, err := positionAfter(b.startSFEN, moves[:b.ply-1])
	if err != nil {
		return pos, shogi.Move{}, fmt.Errorf("국면 복원: %w", err)
	}
	m, err := shogi.ParseUSIMove(b.retracted)
	if err != nil {
		return pos, m, fmt.Errorf("수 파싱: %w", err)
	}
	if err := pos.ValidateMove(m); err != nil {
		return pos, m, fmt.Errorf("복원한 국면에서 둘 수 없는 수: %w", err)
	}
	return pos, m, nil
}

// offlineCategory 는 복원한 사실로 프로덕션과 같은 분류기를 돌린다.
//
// classify 가 비공개라 Judge 를 지나간다. 낙폭을 확실히 임계치 위로 두면 분류만
// 남고, 여기서 갈릴 수 있는 유일한 분기인 shallow_trap 은 HasShallow=false 라
// 애초에 안 걸린다. 규칙을 베껴 오지 않는다 — 베끼면 calibrate 가
// 조건을 고치는 순간 측정만 조용히 옛 규칙을 잰다.
func offlineCategory(f intervene.Features) intervene.Category {
	return intervene.Judge(intervene.Input{
		BestCp:   30000,
		AfterCp:  -30000,
		Features: f,
		Level:    intervene.Beginner,
	}).Category
}

// TestMeasureBlunderOther 는 other 54건이 어느 길로 거기 갔는지를 가른다.
func TestMeasureBlunderOther(t *testing.T) {
	conn := measureDB(t)
	all, moves := loadBlunders(t, conn)
	if len(all) == 0 {
		t.Skip("개입 기록 없음")
	}

	type bucket struct {
		name  string
		rows  []blunderRow
		feats []intervene.Features
	}
	// other 가 어느 길로 갔는가와, 그 밖의 카테고리가 그대로 재현되는가는 다른
	// 질문이다. 한 표에 섞으면 「54건」 아래에 56줄이 찍혀 표가 자기 합계와 안 맞는다.
	buckets := map[string]*bucket{} // other 의 경로
	control := map[string]*bucket{} // 그 밖의 재현 대조
	adder := func(m map[string]*bucket) func(string, blunderRow, intervene.Features) {
		return func(name string, b blunderRow, f intervene.Features) {
			x, ok := m[name]
			if !ok {
				x = &bucket{name: name}
				m[name] = x
			}
			x.rows = append(x.rows, b)
			x.feats = append(x.feats, f)
		}
	}
	add, addControl := adder(buckets), adder(control)

	stored := map[string]int{}
	var others []blunderRow

	for _, b := range all {
		stored[b.category]++
		pos, m, err := replayBlunder(b, moves[b.gameID])
		if err != nil {
			add("복원실패: "+err.Error(), b, intervene.Features{})
			if b.category == "other" {
				others = append(others, b)
			}
			continue
		}
		f, _ := moveFacts(pos, m)
		// UnpromotedOnly · ShallowCp 는 엔진이 있어야 나온다. 여기서는 세우지 않는다 —
		// 저장된 카테고리가 그 둘이 아니라는 것이 이미 「그때 안 걸렸다」는 뜻이다.
		got := offlineCategory(f)

		if b.category == "other" {
			others = append(others, b)
			switch {
			case !f.Known:
				add("① !Known — 사실을 못 구했다", b, f)
			case got == intervene.CategoryOther:
				add("② default — 구했는데 안 맞았다", b, f)
			default:
				add("③ 재현 불일치: 지금 돌리면 "+string(got), b, f)
			}
			continue
		}
		if string(got) == b.category {
			addControl("일치: "+b.category, b, f)
		} else {
			addControl(fmt.Sprintf("불일치: 저장 %s → 지금 %s", b.category, got), b, f)
		}
	}

	t.Logf("== 저장된 카테고리 (kind=blunder %d건) ==", len(all))
	for _, k := range sortedKeys(stored) {
		t.Logf("  %-16s %3d  %5.1f%%", k, stored[k], 100*float64(stored[k])/float64(len(all)))
	}

	t.Logf("\n== `other` %d건이 어느 길로 갔나 ==", len(others))
	for _, k := range sortedBucketKeys(buckets) {
		t.Logf("  %-42s %3d", k, len(buckets[k].rows))
	}

	// 대조군이다. other 가 아닌 것이 오프라인에서 그대로 재현되면 복원 자체를
	// 믿어도 된다는 뜻이고, 위의 갈래도 같은 만큼 믿을 수 있다. shallow_trap 은
	// ShallowCp 가 엔진에서만 나오므로 어긋나는 것이 맞다 — 어긋나지 않으면
	// 오히려 그쪽을 의심해야 한다.
	t.Logf("\n== 대조: `other` 가 아닌 것이 그대로 재현되나 ==")
	for _, k := range sortedBucketKeys(control) {
		t.Logf("  %-42s %3d", k, len(control[k].rows))
	}

	// ② 로 떨어진 것들의 사실을 그대로 찍는다. 새 카테고리는 여기서 나온다 —
	// 어느 조건에 얼마나 못 미쳤는지가 보여야 「조건이 좁다」와 「분기가 없다」가 갈린다.
	if x := buckets["② default — 구했는데 안 맞았다"]; x != nil {
		t.Logf("\n== ② default %d건의 사실 ==", len(x.rows))
		t.Logf("  %-6s %-5s %-6s %-9s %-6s %-5s %-5s %-6s %-6s %-6s",
			"game", "ply", "手数", "Δwin", "잡음", "노림", "지킴", "駒값", "玉방어", "玉위협")
		for i, b := range x.rows {
			f := x.feats[i]
			t.Logf("  %-6d %-5d %-6d %-9.3f %-6d %-5t %-5t %-6d %-6d %-6d",
				b.gameID, b.ply, b.gameLen, b.deltaWin,
				f.CapturedValue, f.LandsAttacked, f.LandsDefended, f.MovedValue,
				f.ShieldLoss, f.ThreatGain)
		}
		summarizeOther(t, x.feats)
	}

	// 종반 가설. other 가 대국의 뒤쪽에 몰려 있으면 분류기의 실패가 아니라
	// 적용 범위 밖이라는 뜻이 된다.
	//
	// 「대국의 몇 % 지점인가」로 재지 않는다. 되무른 수는 game_moves 에 안 남고,
	// 개입 직후에 대국이 끝난 경우가 많아 ply 가 기록된 手数를 넘는다 — 그 비율은
	// 100%를 넘어가 뜻을 잃는다. 대신 ply 와 그 대국의 총 手数를 따로 찍고, 넘어간
	// 건수를 함께 센다. 넘어간 것 자체가 사실이다: 그 수에서 대국이 끝났다는 뜻이다.
	t.Logf("\n== 대국 안에서의 위치 ==")
	t.Logf("  %-16s %-6s %-10s %-12s %-10s", "카테고리", "건수", "중앙 ply", "중앙 총 手数", "끝에서 끝남")
	byCat := map[string][]blunderRow{}
	for _, b := range all {
		byCat[b.category] = append(byCat[b.category], b)
	}
	for _, k := range sortedKeys(stored) {
		rs := byCat[k]
		plies := make([]float64, 0, len(rs))
		lens := make([]float64, 0, len(rs))
		pastEnd := 0
		for _, r := range rs {
			plies = append(plies, float64(r.ply))
			lens = append(lens, float64(r.gameLen))
			if r.ply > r.gameLen {
				pastEnd++
			}
		}
		t.Logf("  %-16s %-6d %-10.0f %-12.0f %-10d", k, len(rs), median(plies), median(lens), pastEnd)
	}
}

// summarizeOther 는 ② 로 떨어진 사실들을 분기별로 세어 어디가 비었는지 본다.
func summarizeOther(t *testing.T, fs []intervene.Features) {
	t.Helper()
	var quiet, capturedNoCost, kingOneSide, nothing int
	for _, f := range fs {
		switch {
		case f.CapturedValue > 0:
			// 땄는데 greedy_capture 에 안 걸렸다 = 되따이지도 않고 玉도 안 밀렸다.
			capturedNoCost++
		case f.ShieldLoss > 0 || f.ThreatGain > 0:
			// 玉 주변이 한쪽만 움직였다. king_exposed 는 둘 다를 요구한다.
			kingOneSide++
		case f.CapturedValue == 0 && !f.GivesCheck && f.ShieldLoss <= 0 && f.ThreatGain <= 0:
			// 아무것도 안 땄고 王手도 아니고 玉 주변도 안 나빠졌다 — 조용한 악수다.
			quiet++
		default:
			nothing++
		}
	}
	t.Logf("\n  ② 안의 모양:")
	t.Logf("    땄는데 대가가 안 보인다      %3d", capturedNoCost)
	t.Logf("    玉 주변이 한쪽만 움직였다    %3d", kingOneSide)
	t.Logf("    조용한 악수 (아무 사실 없음) %3d", quiet)
	t.Logf("    그 밖                        %3d", nothing)
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return m[out[i]] > m[out[j]] })
	return out
}

func sortedBucketKeys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return s[len(s)/2]
}
