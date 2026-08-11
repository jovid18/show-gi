// Package store 는 PostgreSQL 접근을 담당한다.
//
// **스키마가 코드를 만든다.** `migrations/*.sql` 이 정본이고 sqlc가 거기서 `db/` 를 생성한다
// (`go tool sqlc generate`). 반대 방향(ORM이 스키마를 만드는 것)을 안 쓰는 이유는
// 001_init.sql 에 ORM으로 표현되지 않는 것이 여럿이기 때문이다 — interventions 의 CHECK
// 제약, kb_chunks 의 부분 인덱스, pgvector 의 vector 타입. 옮겨 적으면 스키마가 두 벌이 된다.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jovid18/show-gi/apps/server/internal/store/db"
)

// Store 는 커넥션 풀과 생성된 질의를 함께 들고 있다.
type Store struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

// Open 은 커넥션 풀을 열고 한 번 통신해 본다.
//
// **여는 것만으로는 붙었는지 모른다** — pgxpool은 지연 연결이라 첫 질의에서야 실패한다.
// 기동 시점에 알아야 /healthz 가 사실을 말할 수 있다.
func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Store{pool: pool, q: db.New(pool)}, nil
}

func (s *Store) Close() { s.pool.Close() }

// Ping 은 지금 붙어 있는지 본다.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// Candidate 는 한 국면의 후보 수 하나다. positions.candidates 에 JSON으로 들어간다.
//
// **Cp 는 수번 측 관점이다** — 엔진이 답하는 그대로다. 여기서 플레이어 관점으로 돌려놓으면
// 같은 국면이 사람의 색에 따라 두 행이 되어 캐시가 성립하지 않는다.
type Candidate struct {
	USI string   `json:"usi"`
	Cp  int      `json:"cp"`
	PV  []string `json:"pv,omitempty"`
	// MateIn 은 詰み까지의 手数다(수번 측이 이기면 양수). 詰み이 아니면 0.
	//
	// **cp만으로는 복원할 수 없다.** mate 는 30000에서 手数를 뺀 값으로 환산되어 들어오므로,
	// 캐시에서 꺼낼 때 그 숫자를 그대로 화면에 쓰면 「+29995」가 나간다.
	// 칸을 늘리는 것이 아니라 jsonb 안이라 **마이그레이션이 필요 없다.**
	MateIn int `json:"mate,omitempty"`
}

// Position 은 캐시된 국면 하나다.
type Position struct {
	SFENKey       string
	SideToMove    string
	PlyHint       int
	Candidates    []Candidate
	ComputedDepth int
}

// ErrNoPosition 은 캐시에 없을 때.
var ErrNoPosition = errors.New("store: position not cached")

// GetPosition 은 캐시된 국면을 읽는다. 없으면 ErrNoPosition.
func (s *Store) GetPosition(ctx context.Context, sfenKey string) (Position, error) {
	row, err := s.q.GetPosition(ctx, sfenKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return Position{}, ErrNoPosition
	}
	if err != nil {
		return Position{}, err
	}

	out := Position{
		SFENKey:       row.SFENKey,
		SideToMove:    row.SideToMove,
		ComputedDepth: int(row.ComputedDepth),
	}
	if row.PlyHint != nil {
		out.PlyHint = int(*row.PlyHint)
	}
	if len(row.Candidates) > 0 {
		if err := json.Unmarshal(row.Candidates, &out.Candidates); err != nil {
			return Position{}, fmt.Errorf("decode candidates for %s: %w", sfenKey, err)
		}
	}
	return out, nil
}

// PutPosition 은 국면을 캐시에 넣는다.
//
// **더 얕게 계산한 결과는 깊은 결과를 덮지 않는다.** 그 판정은 SQL의 WHERE 절이 하고,
// 덮지 않았으면 stored=false 로 알려준다 — 호출 측이 "내 결과가 더 얕았다"를 알 수 있어야
// 조용히 버려지지 않는다.
func (s *Store) PutPosition(ctx context.Context, p Position) (stored bool, err error) {
	// **`null` 을 넣지 않는다.** 질의가 `jsonb_array_length` 로 후보 수를 견주는데
	// (같은 깊이면 많은 쪽이 이긴다) 그 함수는 배열이 아닌 값에서 에러를 낸다 —
	// 후보를 모르는 채로 국면만 남기는 자리가 실제로 있다(archive 의 부모 국면).
	if p.Candidates == nil {
		p.Candidates = []Candidate{}
	}
	payload, err := json.Marshal(p.Candidates)
	if err != nil {
		return false, fmt.Errorf("encode candidates: %w", err)
	}
	ply := int32(p.PlyHint)

	_, err = s.q.UpsertPosition(ctx, db.UpsertPositionParams{
		SFENKey:       p.SFENKey,
		SideToMove:    p.SideToMove,
		PlyHint:       &ply,
		Candidates:    payload,
		ComputedDepth: int32(p.ComputedDepth),
	})
	// 더 얕아서 갱신하지 않으면 RETURNING 이 아무 행도 안 준다. 에러가 아니다.
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// CountPositions 는 캐시에 쌓인 국면 수. 히트율 측정과 발표 숫자에 쓴다.
func (s *Store) CountPositions(ctx context.Context) (int64, error) {
	return s.q.CountPositions(ctx)
}

// Edge 는 국면 사이의 한 수다. **분석을 버리지 않기 위한 자리다.**
//
// 한 수의 사실이 두 번에 걸쳐 온다 — 후보를 잴 때는 깊이별 평가치를 알고, 그 수를 실제로
// 두어 자식 국면을 잴 때는 도착 국면과 태그를 안다. 그래서 **비어 있는 칸은 「모른다」**이고,
// 질의가 그 칸을 지우지 않는다(query/positions.sql).
type Edge struct {
	ParentKey string
	USI       string
	// ChildKey 는 도착 국면의 키다. 비어 있으면 아직 그 국면을 재지 않았다.
	//
	// **`positions` 를 참조한다.** 없는 키를 넣으면 FK가 거절하므로, 부르는 쪽이 자식
	// 국면을 먼저 넣어야 한다.
	ChildKey string
	// Tags 는 이 수가 **새로 만든** 囲い·전법·手筋의 코드다.
	Tags []string
	// EvalByDepth 는 깊이 1..N의 **先手 관점** cp다(schema 주석과 같은 규약).
	//
	// 추가 탐색이 없다 — `PvInterval=0` 덕에 depth N 탐색 한 번이 1..N을 전부 준다.
	EvalByDepth []int
}

// PutEdge 는 한 수의 분석을 남긴다. 이미 있는 칸은 덮지 않는다.
func (s *Store) PutEdge(ctx context.Context, e Edge) error {
	arg := db.UpsertEdgeParams{ParentKey: e.ParentKey, USI: e.USI, Tags: e.Tags}
	if e.ChildKey != "" {
		arg.ChildKey = &e.ChildKey
	}
	if arg.Tags == nil {
		arg.Tags = []string{} // NOT NULL 칸이다. nil을 보내면 거절된다
	}
	for _, cp := range e.EvalByDepth {
		arg.EvalByDepth = append(arg.EvalByDepth, int32(cp))
	}
	return s.q.UpsertEdge(ctx, arg)
}

// CountEdges 는 쌓인 수의 개수다. 캐시와 같은 자리에서 발표 숫자로 쓴다.
func (s *Store) CountEdges(ctx context.Context) (int64, error) { return s.q.CountEdges(ctx) }

// Edges 는 그 국면에서 나가는 수들이다.
//
// **깊이별 평가치를 되찾는 유일한 길이다.** `positions.candidates` 에는 마지막 깊이의 값만
// 있어서, 개입 판정이 보는 얕은 값(depth 2)은 여기서만 나온다 — 캐시로 탐색을 대신할 때
// 그 값이 빠지면 「얕은 이득에 낚임」 카테고리가 조용히 사라진다(01-core.md §3).
func (s *Store) Edges(ctx context.Context, parentKey string) ([]Edge, error) {
	rows, err := s.q.ListEdges(ctx, parentKey)
	if err != nil {
		return nil, err
	}
	out := make([]Edge, 0, len(rows))
	for _, r := range rows {
		e := Edge{ParentKey: r.ParentKey, USI: r.USI, Tags: r.Tags}
		if r.ChildKey != nil {
			e.ChildKey = *r.ChildKey
		}
		for _, cp := range r.EvalByDepth {
			e.EvalByDepth = append(e.EvalByDepth, int(cp))
		}
		out = append(out, e)
	}
	return out, nil
}

// ── 대국 기록 ────────────────────────────────────────────

// GameResult 는 games.result 에 들어가는 값이다. DDL의 주석과 같은 어휘를 쓴다.
type GameResult string

const (
	ResultWin       GameResult = "win"
	ResultLoss      GameResult = "loss"
	ResultDraw      GameResult = "draw"
	ResultAbandoned GameResult = "abandoned" // 끝나지 않고 연결이 끊겼다
)

// CreateGame 은 대국 하나를 연다. userID 가 nil이면 로그인 전 대국이다.
func (s *Store) CreateGame(ctx context.Context, userID *int64, myColor, startSFEN string) (int64, error) {
	id, err := s.q.CreateGame(ctx, db.CreateGameParams{
		UserID:    userID,
		MyColor:   myColor,
		StartSfen: &startSFEN,
	})
	if err != nil {
		return 0, fmt.Errorf("create game: %w", err)
	}
	return id, nil
}

// FinishGame 은 대국을 닫는다.
func (s *Store) FinishGame(ctx context.Context, gameID int64, result GameResult) error {
	r := string(result)
	if err := s.q.FinishGame(ctx, db.FinishGameParams{ID: gameID, Result: &r}); err != nil {
		return fmt.Errorf("finish game: %w", err)
	}
	return nil
}

// InsertMove 는 **확정된** 수 하나를 기보에 넣는다.
func (s *Store) InsertMove(ctx context.Context, gameID int64, ply int, usi string) error {
	if err := s.q.InsertMove(ctx, db.InsertMoveParams{
		GameID: gameID,
		Ply:    int32(ply),
		USI:    usi,
	}); err != nil {
		return fmt.Errorf("insert move: %w", err)
	}
	return nil
}

// SetMoveEval 은 그 手数의 평가치를 나중에 채운다. **先手 관점 cp** 다.
//
// 관점을 先手로 고정하는 것은 `edges.eval_by_depth` 와 같은 규약이다(02-architecture.md §4).
// 대국마다 사람의 색이 달라지므로 「플레이어 관점」으로 적으면 두 판을 나란히 못 놓는다 —
// 뒤집는 일은 질의하는 쪽이 `games.my_color` 로 한다.
//
// 수가 먼저 들어가 있어야 한다. 없는 ply면 아무 일도 일어나지 않는다.
func (s *Store) SetMoveEval(ctx context.Context, gameID int64, ply, cp int) error {
	v := int32(cp)
	if err := s.q.SetMoveEval(ctx, db.SetMoveEvalParams{
		GameID: gameID,
		Ply:    int32(ply),
		EvalCp: &v,
	}); err != nil {
		return fmt.Errorf("set move eval: %w", err)
	}
	return nil
}

// Intervention 은 기록할 개입 하나다.
type Intervention struct {
	Ply         int
	Kind        string
	Category    string
	DeltaWin    float64
	LevelBucket string
	// RetractedUSI 는 개입이 막지 않았다면 실제로 뒀을 수다.
	RetractedUSI string

	// ExplainTier 는 설명이 어느 계층에서 나왔나(0=캐시 1=소형 2=대형)다.
	//
	// **nil이면 NULL로 들어간다** — LLM을 아예 안 거친 것이다. 0으로 적으면 「캐시 히트」와
	// 섞이는데, 그 둘은 「호출을 아꼈다」와 「붙이지 않았다」로 뜻이 정반대다.
	ExplainTier *int
	// CostYen 은 그 설명 하나에 든 돈이다. 캐시 히트와 템플릿은 0이다.
	//
	// 칸이 numeric(10,4) 라 **0.0001엔 미만은 0으로 떨어진다.** 소형 모델 한 번이 그보다
	// 싸질 수 있으므로, 합계를 볼 때는 이 칸이 아니라 라우터의 analytics 를 본다.
	CostYen float64
}

// InsertIntervention 은 개입 하나를 남긴다.
//
// **같은 ply에 여러 번 불릴 수 있다.** 한 국면에서 몇 수를 시도하고 전부 물러지는 일이
// 실제로 있고, 그 반복이 곧 「그 국면이 그 사람에게 얼마나 어려웠나」다.
func (s *Store) InsertIntervention(ctx context.Context, gameID int64, iv Intervention) error {
	arg := db.InsertInterventionParams{
		GameID: gameID,
		Ply:    int32(iv.Ply),
		Kind:   iv.Kind,
	}
	if iv.Category != "" {
		arg.Category = &iv.Category
	}
	if iv.LevelBucket != "" {
		arg.LevelBucket = &iv.LevelBucket
	}
	if iv.RetractedUSI != "" {
		arg.RetractedUsi = &iv.RetractedUSI
	}
	d := iv.DeltaWin
	arg.DeltaWin = &d

	if iv.ExplainTier != nil {
		t := int16(*iv.ExplainTier)
		arg.ExplainTier = &t
	}
	if err := arg.CostYen.Scan(strconv.FormatFloat(iv.CostYen, 'f', 4, 64)); err != nil {
		return fmt.Errorf("insert intervention: cost %v: %w", iv.CostYen, err)
	}

	if err := s.q.InsertIntervention(ctx, arg); err != nil {
		return fmt.Errorf("insert intervention: %w", err)
	}
	return nil
}

// CachedExplanation 은 설명 캐시(Tier 0)를 찾는다. **찾으면서 히트를 센다.**
//
// 없는 키는 에러가 아니다 — 그것이 정상 경로의 절반이고, 부르는 쪽은 그때 LLM으로 내려간다.
func (s *Store) CachedExplanation(ctx context.Context, key string) (string, bool, error) {
	body, err := s.q.CachedExplanation(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("cached explanation: %w", err)
	}
	return body, true, nil
}

// SaveExplanation 은 만든 문장을 캐시에 넣는다. 같은 키가 있으면 아무 일도 안 한다.
func (s *Store) SaveExplanation(ctx context.Context, key, body, model string) error {
	arg := db.SaveExplanationParams{Key: key, Body: body}
	if model != "" {
		arg.Model = &model
	}
	if err := s.q.SaveExplanation(ctx, arg); err != nil {
		return fmt.Errorf("save explanation: %w", err)
	}
	return nil
}

// ExplainCacheStats 는 (항목 수, 누적 히트)다. 발표의 캐시 히트율이 여기서 나온다.
func (s *Store) ExplainCacheStats(ctx context.Context) (entries, hits int64, err error) {
	row, err := s.q.ExplainCacheStats(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("explain cache stats: %w", err)
	}
	return row.Entries, row.Hits, nil
}

// CountGames · CountInterventions 는 기록이 실제로 쌓이는지 확인하는 데 쓴다.
func (s *Store) CountGames(ctx context.Context) (int64, error) { return s.q.CountGames(ctx) }
func (s *Store) CountInterventions(ctx context.Context) (int64, error) {
	return s.q.CountInterventions(ctx)
}

// ── 리뷰(읽기) ───────────────────────────────────────────
//
// 여기까지가 쓰는 쪽이었다. 아래가 **꺼내는 쪽**이고, 리뷰 화면이 유일한 소비자다.

// GameSummary 는 리뷰 목록의 한 줄이다.
type GameSummary struct {
	ID      int64
	MyColor string
	// StartedAt·FinishedAt. **FinishedAt 은 zero 일 수 있다** — 아직 두는 중인 판이다.
	StartedAt  time.Time
	FinishedAt time.Time
	// Result 는 끝나지 않았으면 빈 문자열이다.
	Result            GameResult
	MoveCount         int
	InterventionCount int
}

// RecordedMove 는 기보의 한 수다.
type RecordedMove struct {
	Ply int
	USI string
	// EvalCp 는 **先手 관점** cp이고 nil일 수 있다 — 평가치는 수보다 늦게 오므로
	// 연결이 끊긴 판의 마지막 몇 수는 안 채워진 채로 남는다.
	EvalCp *int
}

// RecordedIntervention 은 남아 있는 개입 하나다.
//
// **문구는 없다.** 화면에 나갔던 일본어 문장은 기록하지 않으므로(카테고리만 남는다),
// 리뷰는 그 문장을 다시 만들어야 한다 — 카테고리가 정본이고 문장은 파생이다.
type RecordedIntervention struct {
	Ply          int
	Kind         string
	Category     string
	DeltaWin     float64
	LevelBucket  string
	RetractedUSI string
}

// GameRecord 는 한 판 전체다.
type GameRecord struct {
	GameSummary
	// StartSFEN 은 비어 있을 수 있다. 그때는 평수 초기 국면이다.
	StartSFEN     string
	Moves         []RecordedMove
	Interventions []RecordedIntervention
}

// ErrNoGame 은 그런 대국이 없을 때.
var ErrNoGame = errors.New("store: game not found")

// ListGames 는 최근 대국을 최신부터 준다. **한 수도 안 둔 판은 안 온다**(games.sql).
//
// limit 은 여기서 int32 범위로 자른다. **자르는 변환을 하는 자리가 스스로 막아야 한다** —
// 64비트에서 `int32(limit)` 은 큰 값을 조용히 음수로 만들고, 그러면 `LIMIT` 이 거짓말을 한다.
// 지금은 부르는 쪽(review.go)이 정책상의 상한을 이미 걸지만, 그건 그쪽의 정책이지
// 이 변환의 안전이 아니다 — 다음 호출자가 같은 데를 밟는다.
func (s *Store) ListGames(ctx context.Context, limit int) ([]GameSummary, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > math.MaxInt32 {
		limit = math.MaxInt32
	}

	rows, err := s.q.ListGames(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("list games: %w", err)
	}
	out := make([]GameSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, GameSummary{
			ID:                r.ID,
			MyColor:           r.MyColor,
			StartedAt:         r.StartedAt.Time,
			FinishedAt:        r.FinishedAt.Time,
			Result:            resultValue(r.Result),
			MoveCount:         int(r.MoveCount),
			InterventionCount: int(r.InterventionCount),
		})
	}
	return out, nil
}

// GameRecord 는 한 판을 통째로 읽는다. 없으면 ErrNoGame.
//
// **트랜잭션으로 묶지 않는다.** 기록은 덧붙이기만 하므로 끝난 판은 어차피 안 변하고,
// 두는 중인 판에서 최악이 「기보보다 개입이 한 수 앞선다」인데 그건 부르는 쪽이
// 감당할 수 있다(그 개입은 아직 화면에 그릴 국면이 없을 뿐이다). 그 하나를 막자고
// 대국 중인 판에 트랜잭션을 거는 쪽이 비싸다.
func (s *Store) GameRecord(ctx context.Context, gameID int64) (GameRecord, error) {
	head, err := s.q.GetGame(ctx, gameID)
	if errors.Is(err, pgx.ErrNoRows) {
		return GameRecord{}, ErrNoGame
	}
	if err != nil {
		return GameRecord{}, fmt.Errorf("get game %d: %w", gameID, err)
	}

	moves, err := s.q.ListGameMoves(ctx, gameID)
	if err != nil {
		return GameRecord{}, fmt.Errorf("list moves of game %d: %w", gameID, err)
	}
	ivs, err := s.q.ListGameInterventions(ctx, gameID)
	if err != nil {
		return GameRecord{}, fmt.Errorf("list interventions of game %d: %w", gameID, err)
	}

	out := GameRecord{
		GameSummary: GameSummary{
			ID:                head.ID,
			MyColor:           head.MyColor,
			StartedAt:         head.StartedAt.Time,
			FinishedAt:        head.FinishedAt.Time,
			Result:            resultValue(head.Result),
			MoveCount:         len(moves),
			InterventionCount: len(ivs),
		},
		Moves:         make([]RecordedMove, 0, len(moves)),
		Interventions: make([]RecordedIntervention, 0, len(ivs)),
	}
	if head.StartSfen != nil {
		out.StartSFEN = *head.StartSfen
	}

	for _, m := range moves {
		rec := RecordedMove{Ply: int(m.Ply), USI: m.USI}
		if m.EvalCp != nil {
			cp := int(*m.EvalCp)
			rec.EvalCp = &cp
		}
		out.Moves = append(out.Moves, rec)
	}
	for _, iv := range ivs {
		out.Interventions = append(out.Interventions, RecordedIntervention{
			Ply:          int(iv.Ply),
			Kind:         iv.Kind,
			Category:     deref(iv.Category),
			DeltaWin:     derefFloat(iv.DeltaWin),
			LevelBucket:  deref(iv.LevelBucket),
			RetractedUSI: deref(iv.RetractedUsi),
		})
	}
	return out, nil
}

func resultValue(s *string) GameResult {
	if s == nil {
		return ""
	}
	return GameResult(*s)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefFloat(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

// Pool 은 생성된 질의로 표현되지 않는 것을 테스트가 직접 물어보는 통로다.
// 프로덕션 코드는 쓰지 않는다 — 규칙은 SQL 파일에 있어야 한다.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }
