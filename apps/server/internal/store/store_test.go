package store

import (
	"context"
	"errors"
	"os"
	"testing"
)

// 진짜 postgres에 붙는다. 없으면 건너뛴다 — CI 러너에는 DB가 없다.
//
// 여기서 확인하는 규칙(더 얕은 결과가 깊은 결과를 못 덮는다)은 **SQL의 WHERE 절에만
// 있다.** Go 쪽에 옮겨 적지 않았으므로 가짜로는 검증할 수 없다.
//
//	docker compose up -d db
//	SHOWGI_TEST_DATABASE_URL=postgres://showgi:showgi@localhost:5432/showgi go test ./internal/store/ -v
func open(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("SHOWGI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SHOWGI_TEST_DATABASE_URL 미설정 — DB 테스트 건너뜀")
	}
	s, err := Open(t.Context(), url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

// 테스트마다 다른 키를 쓰고, **시작할 때 지운다.**
//
// 지우지 않으면 이전 실행이 남긴 행이 남아 두 번째부터 결과가 달라진다. CI는 매번 빈
// DB라 통과하고 로컬에서만 깨지는데, 그런 테스트는 있으나 마나다.
func key(t *testing.T, s *Store) string {
	t.Helper()
	k := "test/" + t.Name()
	if _, err := s.pool.Exec(t.Context(), `DELETE FROM positions WHERE sfen_key = $1`, k); err != nil {
		t.Fatalf("이전 실행 정리: %v", err)
	}
	return k
}

func TestPositionRoundTrip(t *testing.T) {
	s := open(t)
	k := key(t, s)

	if _, err := s.GetPosition(t.Context(), k); !errors.Is(err, ErrNoPosition) {
		t.Fatalf("빈 캐시에서 ErrNoPosition 기대, got %v", err)
	}

	want := Position{
		SFENKey:    k,
		SideToMove: "b",
		PlyHint:    12,
		Candidates: []Candidate{
			{USI: "7g7f", Cp: 143, PV: []string{"7g7f", "3c3d"}},
			{USI: "2g2f", Cp: 121},
		},
		ComputedDepth: 12,
	}
	stored, err := s.PutPosition(t.Context(), want)
	if err != nil || !stored {
		t.Fatalf("PutPosition: stored=%v err=%v", stored, err)
	}

	got, err := s.GetPosition(t.Context(), k)
	if err != nil {
		t.Fatalf("GetPosition: %v", err)
	}
	if got.SideToMove != "b" || got.PlyHint != 12 || got.ComputedDepth != 12 {
		t.Fatalf("스칼라 왕복 불일치: %+v", got)
	}
	if len(got.Candidates) != 2 || got.Candidates[0].USI != "7g7f" || got.Candidates[0].Cp != 143 {
		t.Fatalf("후보 왕복 불일치: %+v", got.Candidates)
	}
	if len(got.Candidates[0].PV) != 2 || got.Candidates[1].PV != nil {
		t.Fatalf("PV 왕복 불일치: %+v", got.Candidates)
	}
}

// **이 PR의 핵심.** 얕은 결과가 깊은 결과를 덮으면 개입 판정이 얕은 값 위에서 돈다.
func TestShallowerResultDoesNotOverwrite(t *testing.T) {
	s := open(t)
	k := key(t, s)

	deep := Position{
		SFENKey: k, SideToMove: "b", ComputedDepth: 14,
		Candidates: []Candidate{{USI: "7g7f", Cp: 100}},
	}
	if stored, err := s.PutPosition(t.Context(), deep); err != nil || !stored {
		t.Fatalf("깊은 결과 저장: stored=%v err=%v", stored, err)
	}

	shallow := Position{
		SFENKey: k, SideToMove: "b", ComputedDepth: 10,
		Candidates: []Candidate{{USI: "9g9f", Cp: -999}},
	}
	stored, err := s.PutPosition(t.Context(), shallow)
	if err != nil {
		t.Fatalf("얕은 결과 저장: %v", err)
	}
	if stored {
		t.Fatal("얕은 결과가 깊은 결과를 덮었다")
	}

	got, _ := s.GetPosition(t.Context(), k)
	if got.ComputedDepth != 14 || got.Candidates[0].USI != "7g7f" {
		t.Fatalf("깊은 결과가 남아야 한다: %+v", got)
	}

	// 같은 깊이도 덮지 않는다 — 같은 국면·같은 깊이는 같은 결과라 쓸 이유가 없다
	same := deep
	same.Candidates = []Candidate{{USI: "2g2f", Cp: 1}}
	if stored, err := s.PutPosition(t.Context(), same); err != nil || stored {
		t.Fatalf("같은 깊이가 덮였다: stored=%v err=%v", stored, err)
	}

	// 더 깊으면 덮는다
	deeper := deep
	deeper.ComputedDepth = 16
	deeper.Candidates = []Candidate{{USI: "2g2f", Cp: 200}}
	if stored, err := s.PutPosition(t.Context(), deeper); err != nil || !stored {
		t.Fatalf("더 깊은 결과가 안 덮였다: stored=%v err=%v", stored, err)
	}
	got, _ = s.GetPosition(t.Context(), k)
	if got.ComputedDepth != 16 || got.Candidates[0].USI != "2g2f" {
		t.Fatalf("더 깊은 결과로 안 바뀌었다: %+v", got)
	}
}

func TestPingAndCount(t *testing.T) {
	s := open(t)
	if err := s.Ping(t.Context()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if _, err := s.CountPositions(context.Background()); err != nil {
		t.Fatalf("CountPositions: %v", err)
	}
}

// ── 대국 기록 ────────────────────────────────────────────

// newGame 은 빈 대국을 하나 열고, 끝나면 지운다.
//
// 지우는 것은 **다음 실행이 이전 행을 세지 않게** 하기 위해서다. game_moves 와
// interventions 는 ON DELETE CASCADE 라 대국만 지우면 같이 지워진다.
func newGame(t *testing.T, s *Store) int64 {
	t.Helper()
	id, err := s.CreateGame(t.Context(), nil, "b", "startpos-for-"+t.Name())
	if err != nil {
		t.Fatalf("CreateGame: %v", err)
	}
	t.Cleanup(func() {
		if _, err := s.pool.Exec(context.Background(), `DELETE FROM games WHERE id = $1`, id); err != nil {
			t.Errorf("정리: %v", err)
		}
	})
	return id
}

// **로그인 전에도 남아야 한다.** user_id 가 NOT NULL이던 동안에는 한 판도 못 남겼고,
// 그 사이에 둔 판은 되살릴 수 없다 (002_anonymous_games.sql).
func TestGameWithoutUser(t *testing.T) {
	s := open(t)
	id := newGame(t, s)
	if id == 0 {
		t.Fatal("대국 id가 0이다")
	}

	if err := s.InsertMove(t.Context(), id, 1, "7g7f"); err != nil {
		t.Fatalf("InsertMove: %v", err)
	}
	if err := s.FinishGame(t.Context(), id, ResultWin); err != nil {
		t.Fatalf("FinishGame: %v", err)
	}

	var result string
	var finished *string
	row := s.pool.QueryRow(t.Context(), `SELECT result, finished_at::text FROM games WHERE id = $1`, id)
	if err := row.Scan(&result, &finished); err != nil {
		t.Fatalf("조회: %v", err)
	}
	if result != string(ResultWin) || finished == nil {
		t.Fatalf("result=%q finished=%v", result, finished)
	}
}

// **같은 ply에 여러 개입이 들어간다.**
//
// 한 국면에서 몇 수를 시도하고 전부 물러지는 일이 실제로 있다(docs/06-status.md §17).
// (game_id, ply) 가 유니크였다면 두 번째 시도부터 조용히 사라지고, 「그 국면이 그 사람에게
// 얼마나 어려웠나」가 통째로 없어진다.
func TestManyInterventionsAtOnePly(t *testing.T) {
	s := open(t)
	id := newGame(t, s)

	tried := []string{"8h3c+", "2g2f", "1i1g"}
	for _, usi := range tried {
		err := s.InsertIntervention(t.Context(), id, Intervention{
			Ply: 81, Kind: "blunder", Category: "hangs_piece",
			DeltaWin: 0.5, LevelBucket: "beginner", RetractedUSI: usi,
		})
		if err != nil {
			t.Fatalf("InsertIntervention(%s): %v", usi, err)
		}
	}

	rows, err := s.pool.Query(t.Context(),
		`SELECT retracted_usi FROM interventions WHERE game_id = $1 AND ply = 81 ORDER BY id`, id)
	if err != nil {
		t.Fatalf("조회: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		got = append(got, u)
	}
	if len(got) != len(tried) {
		t.Fatalf("시도 %d개 중 %d개만 남았다: %v", len(tried), len(got), got)
	}
	for i := range tried {
		if got[i] != tried[i] {
			t.Errorf("%d번째: %q 기대, got %q", i, tried[i], got[i])
		}
	}
}

// 기보는 **지금 판에 남아 있는 수순**이다. 물러진 뒤 다시 둔 수가 같은 ply를 덮어쓴다.
func TestReplayedPlyOverwritesTheKifu(t *testing.T) {
	s := open(t)
	id := newGame(t, s)

	if err := s.InsertMove(t.Context(), id, 81, "2g2f"); err != nil {
		t.Fatalf("InsertMove: %v", err)
	}
	if err := s.InsertMove(t.Context(), id, 81, "B*4a"); err != nil {
		t.Fatalf("다시 둔 수: %v", err)
	}

	var usi string
	var n int
	if err := s.pool.QueryRow(t.Context(),
		`SELECT usi, count(*) OVER () FROM game_moves WHERE game_id = $1 AND ply = 81`, id).Scan(&usi, &n); err != nil {
		t.Fatalf("조회: %v", err)
	}
	if n != 1 || usi != "B*4a" {
		t.Fatalf("행 %d개, usi=%q — 마지막 수 하나만 남아야 한다", n, usi)
	}
}

// interventions 는 kind별로 채우는 컬럼이 다르고 **DB가 그걸 막는다.**
// 섞이면 실력 추정이 조용히 틀어지므로 Go 쪽 실수가 여기서 걸려야 한다.
func TestInterventionKindConstraint(t *testing.T) {
	s := open(t)
	id := newGame(t, s)

	// 제안형(tesuji)에는 물러진 수가 있을 수 없다
	_, err := s.pool.Exec(t.Context(),
		`INSERT INTO interventions (game_id, ply, kind, retracted_usi) VALUES ($1, 1, 'tesuji', '7g7f')`, id)
	if err == nil {
		t.Fatal("tesuji 에 retracted_usi 가 들어갔다 — CHECK 제약이 안 걸린다")
	}
}

// 평가치는 **수를 덮지 않고** 그 행에만 들어간다.
//
// 한 질의로 upsert 하면 수까지 다시 쓰게 되고, 그러면 물러진 수로 기보를 덮는 길이
// 생긴다. 그리고 **없는 ply에는 행을 만들지 않는다** — 평가치가 수보다 먼저 오는 경로가
// 없으므로, 만들어 메우면 기보에 없던 행이 생긴다.
func TestSetMoveEvalFillsOnlyTheEval(t *testing.T) {
	s := open(t)
	id := newGame(t, s)

	if err := s.InsertMove(t.Context(), id, 1, "7g7f"); err != nil {
		t.Fatalf("InsertMove: %v", err)
	}
	if err := s.SetMoveEval(t.Context(), id, 1, -137); err != nil {
		t.Fatalf("SetMoveEval: %v", err)
	}

	var usi string
	var cp *int32
	row := s.pool.QueryRow(t.Context(), `SELECT usi, eval_cp FROM game_moves WHERE game_id=$1 AND ply=1`, id)
	if err := row.Scan(&usi, &cp); err != nil {
		t.Fatalf("조회: %v", err)
	}
	if usi != "7g7f" {
		t.Fatalf("수가 바뀌었다: %q", usi)
	}
	if cp == nil || *cp != -137 {
		t.Fatalf("평가치가 안 들어갔다: %v", cp)
	}

	// 없는 ply — 조용히 아무 일도 없어야 한다.
	if err := s.SetMoveEval(t.Context(), id, 99, 500); err != nil {
		t.Fatalf("없는 ply에서 에러: %v", err)
	}
	var n int
	if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM game_moves WHERE game_id=$1`, id).Scan(&n); err != nil {
		t.Fatalf("개수 조회: %v", err)
	}
	if n != 1 {
		t.Fatalf("행이 생겼다: %d", n)
	}
}

// 설명 캐시는 **히트를 세면서** 문장을 준다. 그 숫자가 발표의 캐시 히트율의 분자다.
//
// 그리고 **먼저 만들어진 문장을 덮지 않는다.** 같은 사실에 다른 문장이 나오기 시작하면
// 「같은 실수에는 같은 설명」이 깨지고, 문구를 고쳤을 때 무엇이 달라졌는지도 못 본다.
func TestExplainCacheCountsHitsAndKeepsTheFirstSentence(t *testing.T) {
	s := open(t)
	k := "test/" + t.Name()
	if _, err := s.pool.Exec(t.Context(), `DELETE FROM explain_cache WHERE key = $1`, k); err != nil {
		t.Fatalf("이전 실행 정리: %v", err)
	}
	t.Cleanup(func() {
		if _, err := s.pool.Exec(context.Background(), `DELETE FROM explain_cache WHERE key = $1`, k); err != nil {
			t.Errorf("정리: %v", err)
		}
	})

	// 없는 키는 에러가 아니다 — 정상 경로의 절반이다.
	if _, ok, err := s.CachedExplanation(t.Context(), k); err != nil || ok {
		t.Fatalf("빈 캐시: ok=%v err=%v", ok, err)
	}

	const first = "その銀を取れる相手の駒が2枚あります。"
	if err := s.SaveExplanation(t.Context(), k, first, "tiny-jp"); err != nil {
		t.Fatalf("SaveExplanation: %v", err)
	}
	// 같은 키로 다시 저장해도 덮이지 않는다.
	if err := s.SaveExplanation(t.Context(), k, "べつの文", "other-model"); err != nil {
		t.Fatalf("SaveExplanation(2): %v", err)
	}

	for i := 1; i <= 3; i++ {
		body, ok, err := s.CachedExplanation(t.Context(), k)
		if err != nil || !ok {
			t.Fatalf("%d번째 조회: ok=%v err=%v", i, ok, err)
		}
		if body != first {
			t.Fatalf("%d번째 조회에서 문장이 바뀌었다: %q", i, body)
		}
	}

	var hits int
	var model string
	row := s.pool.QueryRow(t.Context(), `SELECT hits, model FROM explain_cache WHERE key = $1`, k)
	if err := row.Scan(&hits, &model); err != nil {
		t.Fatalf("조회: %v", err)
	}
	if hits != 3 {
		t.Errorf("hits=%d, want 3 — 히트를 세지 않으면 히트율을 못 낸다", hits)
	}
	if model != "tiny-jp" {
		t.Errorf("model=%q — 먼저 만든 문장의 모델이어야 한다", model)
	}
}

// 개입 행에 **계층과 비용**이 남는다. 없으면 「1회당 ○엔」을 사후에 낼 수 없다.
//
// **LLM을 안 거친 개입은 NULL이다.** 0으로 적으면 「캐시 히트」와 섞이는데, 그 둘은
// 「호출을 아꼈다」와 「붙이지 않았다」로 뜻이 정반대다.
func TestInterventionRecordsTierAndCost(t *testing.T) {
	s := open(t)
	id := newGame(t, s)

	tier := 2
	if err := s.InsertIntervention(t.Context(), id, Intervention{
		Ply: 1, Kind: "blunder", Category: "hangs_piece", DeltaWin: 0.5,
		LevelBucket: "beginner", RetractedUSI: "8h3c+", ExplainTier: &tier, CostYen: 0.1234,
	}); err != nil {
		t.Fatalf("InsertIntervention: %v", err)
	}
	if err := s.InsertIntervention(t.Context(), id, Intervention{
		Ply: 3, Kind: "blunder", Category: "other", DeltaWin: 0.4,
		LevelBucket: "beginner", RetractedUSI: "2g2f", // 계층 없음 = 템플릿
	}); err != nil {
		t.Fatalf("InsertIntervention(템플릿): %v", err)
	}

	var gotTier *int
	var gotCost string
	row := s.pool.QueryRow(t.Context(),
		`SELECT explain_tier, cost_yen::text FROM interventions WHERE game_id = $1 AND ply = 1`, id)
	if err := row.Scan(&gotTier, &gotCost); err != nil {
		t.Fatalf("조회: %v", err)
	}
	if gotTier == nil || *gotTier != 2 {
		t.Errorf("explain_tier=%v, want 2", gotTier)
	}
	if gotCost != "0.1234" {
		t.Errorf("cost_yen=%q, want 0.1234", gotCost)
	}

	row = s.pool.QueryRow(t.Context(),
		`SELECT explain_tier, cost_yen::text FROM interventions WHERE game_id = $1 AND ply = 3`, id)
	if err := row.Scan(&gotTier, &gotCost); err != nil {
		t.Fatalf("조회(템플릿): %v", err)
	}
	if gotTier != nil {
		t.Errorf("explain_tier=%v — LLM을 안 거쳤으면 NULL이어야 한다", *gotTier)
	}
	if gotCost != "0.0000" {
		t.Errorf("cost_yen=%q, want 0.0000", gotCost)
	}
}

// ── 리뷰(읽기) ───────────────────────────────────────────

// 한 판을 넣고 그대로 꺼낸다. **읽는 쪽이 없어서 지금까지 아무도 확인하지 않던 자리다.**
func TestGameRecordRoundTrip(t *testing.T) {
	s := open(t)
	id := newGame(t, s)

	for ply, usi := range map[int]string{1: "7g7f", 2: "3c3d", 3: "8h2b+"} {
		if err := s.InsertMove(t.Context(), id, ply, usi); err != nil {
			t.Fatalf("InsertMove(%d): %v", ply, err)
		}
	}
	if err := s.SetMoveEval(t.Context(), id, 2, -120); err != nil {
		t.Fatalf("SetMoveEval: %v", err)
	}
	if err := s.InsertIntervention(t.Context(), id, Intervention{
		Ply: 3, Kind: "blunder", Category: "hangs_piece",
		DeltaWin: 0.507, LevelBucket: "beginner", RetractedUSI: "2h2d",
	}); err != nil {
		t.Fatalf("InsertIntervention: %v", err)
	}
	if err := s.FinishGame(t.Context(), id, ResultLoss); err != nil {
		t.Fatalf("FinishGame: %v", err)
	}

	got, err := s.GameRecord(t.Context(), id)
	if err != nil {
		t.Fatalf("GameRecord: %v", err)
	}

	// **手数 순서다.** 넣은 순서가 아니라 — 위에서 map으로 넣은 것이 그 확인이다.
	want := []string{"7g7f", "3c3d", "8h2b+"}
	if len(got.Moves) != len(want) {
		t.Fatalf("moves = %d, want %d", len(got.Moves), len(want))
	}
	for i, w := range want {
		if got.Moves[i].USI != w || got.Moves[i].Ply != i+1 {
			t.Errorf("moves[%d] = %+v, want ply %d %q", i, got.Moves[i], i+1, w)
		}
	}

	// 평가치는 붙은 手数에만 있다. 안 붙은 자리가 0이 되면 호각과 구별이 안 된다.
	if got.Moves[0].EvalCp != nil {
		t.Errorf("moves[0].EvalCp = %d, want nil", *got.Moves[0].EvalCp)
	}
	if got.Moves[1].EvalCp == nil || *got.Moves[1].EvalCp != -120 {
		t.Errorf("moves[1].EvalCp = %v, want -120", got.Moves[1].EvalCp)
	}

	if len(got.Interventions) != 1 {
		t.Fatalf("interventions = %d, want 1", len(got.Interventions))
	}
	iv := got.Interventions[0]
	if iv.Ply != 3 || iv.Category != "hangs_piece" || iv.RetractedUSI != "2h2d" {
		t.Errorf("intervention = %+v", iv)
	}
	if iv.DeltaWin < 0.5 || iv.DeltaWin > 0.51 {
		t.Errorf("deltaWin = %v, want ~0.507", iv.DeltaWin)
	}

	if got.Result != ResultLoss || got.FinishedAt.IsZero() {
		t.Errorf("result = %q finished = %v", got.Result, got.FinishedAt)
	}
	// 시작 국면이 없으면 기보만으로는 어디서 시작했는지 알 수 없다(002_anonymous_games.sql).
	if got.StartSFEN == "" {
		t.Error("startSFEN이 비었다 — 판을 되짚을 기준이 사라진다")
	}
	if got.MoveCount != 3 || got.InterventionCount != 1 {
		t.Errorf("moveCount=%d interventionCount=%d, want 3/1", got.MoveCount, got.InterventionCount)
	}
}

func TestGameRecordMissing(t *testing.T) {
	s := open(t)
	// 음수 id는 시퀀스가 절대 안 만든다.
	if _, err := s.GameRecord(t.Context(), -1); !errors.Is(err, ErrNoGame) {
		t.Fatalf("ErrNoGame 기대, got %v", err)
	}
}

// **한 수도 안 둔 판은 목록에 안 온다.** 연결만 열렸다 끊긴 판이 실제로 그렇게 남고,
// 되짚을 것이 없는 줄이 맨 위를 차지하면 진짜 대국이 아래로 밀린다.
func TestListGamesSkipsEmptyGames(t *testing.T) {
	s := open(t)
	empty := newGame(t, s)
	played := newGame(t, s)
	if err := s.InsertMove(t.Context(), played, 1, "7g7f"); err != nil {
		t.Fatalf("InsertMove: %v", err)
	}

	games, err := s.ListGames(t.Context(), 100)
	if err != nil {
		t.Fatalf("ListGames: %v", err)
	}

	var seenPlayed bool
	for _, g := range games {
		if g.ID == empty {
			t.Error("한 수도 안 둔 판이 목록에 있다")
		}
		if g.ID == played {
			seenPlayed = true
			if g.MoveCount != 1 {
				t.Errorf("moveCount = %d, want 1", g.MoveCount)
			}
		}
	}
	if !seenPlayed {
		t.Fatal("둔 판이 목록에 없다")
	}
	// 최신부터다. 방금 만든 판이 맨 위여야 한다.
	if len(games) > 0 && games[0].ID != played {
		t.Errorf("games[0].ID = %d, want %d (최신부터)", games[0].ID, played)
	}
}

// 낙폭을 만든 **두 원본**이 남는다.
//
// 낙폭만으로는 되돌릴 수 없다 — `WinRate(best) - WinRate(after)` 라서 미지수가 둘인데 식이
// 하나이고, 같은 cp 차이가 위치에 따라 다른 낙폭이 된다(06-status.md §39 ⑥). 그래서 이 두
// 칸이 없으면 K를 바꿔 다시 채점할 수도, 물러진 수를 최선수와 한 축에 놓을 수도 없다.
//
// **0과 없음을 갈라 둔다.** 개입이 안 걸린 행과 「정말로 0cp였다」가 섞이면 화면이 없는 값을
// 호각으로 그린다.
func TestInterventionKeepsBothCp(t *testing.T) {
	s := open(t)
	id := newGame(t, s)

	if err := s.InsertIntervention(t.Context(), id, Intervention{
		Ply: 41, Kind: "blunder", Category: "hangs_piece",
		DeltaWin: 0.42, RetractedUSI: "8h3c+", BestCp: 180, AfterCp: -640,
	}); err != nil {
		t.Fatalf("InsertIntervention: %v", err)
	}
	// 판정을 안 거친 행 — 두 칸이 NULL 로 남아야 한다.
	if err := s.InsertIntervention(t.Context(), id, Intervention{
		Ply: 43, Kind: "tesuji", Category: "両取り",
	}); err != nil {
		t.Fatalf("InsertIntervention(tesuji): %v", err)
	}

	rec, err := s.GameRecord(t.Context(), id)
	if err != nil {
		t.Fatalf("GameRecord: %v", err)
	}
	if len(rec.Interventions) != 2 {
		t.Fatalf("개입 2개 기대, got %d", len(rec.Interventions))
	}

	blunder := rec.Interventions[0]
	if blunder.BestCp == nil || blunder.AfterCp == nil {
		t.Fatalf("두 원본이 안 남았다: best=%v after=%v", blunder.BestCp, blunder.AfterCp)
	}
	if *blunder.BestCp != 180 || *blunder.AfterCp != -640 {
		t.Errorf("cp가 어긋났다: best=%d after=%d", *blunder.BestCp, *blunder.AfterCp)
	}

	if tesuji := rec.Interventions[1]; tesuji.BestCp != nil || tesuji.AfterCp != nil {
		t.Errorf("판정을 안 거친 행에 cp가 붙었다: best=%v after=%v", tesuji.BestCp, tesuji.AfterCp)
	}
}
