// Package store 는 PostgreSQL 접근을 담당한다.
//
// 스키마가 코드를 만든다. migrations/*.sql 이 정본이고 sqlc가 거기서 db/ 를 생성한다
// (go tool sqlc generate). 반대 방향(ORM이 스키마를 만드는 것)을 안 쓰는 이유는
// 001_init.sql 에 ORM으로 표현되지 않는 것이 여럿이기 때문이다 — interventions 의 CHECK
// 제약과 부분 인덱스·GIN 인덱스. 옮겨 적으면 스키마가 두 벌이 된다.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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
// 여는 것만으로는 붙었는지 모른다 — pgxpool은 지연 연결이라 첫 질의에서야 실패한다.
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
// Cp 는 수번 측 관점이다 — 엔진이 답하는 그대로다. 여기서 플레이어 관점으로 돌려놓으면
// 같은 국면이 사람의 색에 따라 두 행이 되어 캐시가 성립하지 않는다.
type Candidate struct {
	USI string   `json:"usi"`
	Cp  int      `json:"cp"`
	PV  []string `json:"pv,omitempty"`
	// MateIn 은 詰み까지의 手数다(수번 측이 이기면 양수). 詰み이 아니면 0.
	//
	// cp만으로는 복원할 수 없다. mate 는 30000에서 手数를 뺀 값으로 환산되어 들어오므로,
	// 캐시에서 꺼낼 때 그 숫자를 그대로 화면에 쓰면 「+29995」가 나간다.
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

// PutPosition 은 국면을 캐시에 넣는다. 덮을지 말지는 SQL의 WHERE 절이 정한다
// (query/positions.sql).
//
// 덮지 않았으면 stored=false 다 — 호출 측이 "내 결과가 더 얕았다"를 알 수 있어야
// 조용히 버려지지 않는다.
func (s *Store) PutPosition(ctx context.Context, p Position) (stored bool, err error) {
	// null 을 넣지 않는다. 질의가 jsonb_array_length 로 후보 수를 견주는데
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

// Edge 는 국면 사이의 한 수다. 분석을 버리지 않기 위한 자리다.
//
// 비어 있는 칸은 「모른다」이고, 질의가 그 칸을 지우지 않는다 — 한 수의 사실이 두 번에
// 걸쳐 오기 때문이다(query/positions.sql 의 UpsertEdge).
type Edge struct {
	ParentKey string
	USI       string
	// ChildKey 는 도착 국면의 키다. 비어 있으면 아직 그 국면을 재지 않았다.
	//
	// positions 를 참조한다. 없는 키를 넣으면 FK가 거절하므로, 부르는 쪽이 자식
	// 국면을 먼저 넣어야 한다.
	ChildKey string
	// Tags 는 이 수가 새로 만든 囲い·전법·手筋의 코드다.
	Tags []string
	// EvalByDepth 는 깊이 1..N의 先手 관점 cp다(schema 주석과 같은 규약).
	//
	// 추가 탐색이 없다 — PvInterval=0 덕에 depth N 탐색 한 번이 1..N을 전부 준다.
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
// 깊이별 평가치를 되찾는 유일한 길이다 — positions.candidates 에는 마지막 깊이의
// 값만 있고, 개입 판정이 보는 얕은 값(depth 2)은 여기서만 나온다(01-core.md §3).
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

// Mate 는 캐시된 詰み 답 하나다.
//
// 증명된 것만 들어온다. checkmate timeout 은 행을 만들지 않으므로 「행이 없다」가
// 그대로 「모른다」다(017_mate_positions.sql).
type Mate struct {
	SFENKey string
	// DepthLimit 은 이 답을 낸 solver 의 手数 한계다. 읽는 쪽이 자기 한계와 견준다 —
	// 얕은 한계의 「없다」는 깊은 한계의 「없다」가 아니다.
	DepthLimit int
	// Moves 는 증명된 詰み 수순이다. 비어 있으면 증명된 「詰み이 없다」다.
	Moves []string
}

// ErrNoMate 는 캐시에 없을 때다. 「詰み이 없다」가 아니라 「아직 안 물어봤다」다 —
// 그 둘을 한 값으로 만들면 있는 詰み을 놓친다.
var ErrNoMate = errors.New("store: mate not cached")

// GetMate 는 캐시된 詰み 답을 읽는다. 없으면 ErrNoMate.
func (s *Store) GetMate(ctx context.Context, sfenKey string) (Mate, error) {
	row, err := s.q.GetMate(ctx, sfenKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return Mate{}, ErrNoMate
	}
	if err != nil {
		return Mate{}, err
	}
	return Mate{SFENKey: row.SFENKey, DepthLimit: int(row.DepthLimit), Moves: row.Moves}, nil
}

// PutMate 는 증명된 詰み 답을 캐시에 넣는다. 덮을지 말지는 SQL의 WHERE 절이 정한다
// (query/mate.sql).
//
// 덮지 않았으면 stored=false 다. PutPosition 과 같은 규약이다 — 조용히 버려지는 것과
// 이미 더 깊은 답이 있는 것을 부르는 쪽이 가를 수 있어야 한다.
func (s *Store) PutMate(ctx context.Context, m Mate) (stored bool, err error) {
	moves := m.Moves
	if moves == nil {
		moves = []string{} // NOT NULL 칸이다. nil을 보내면 거절된다
	}
	_, err = s.q.UpsertMate(ctx, db.UpsertMateParams{
		SFENKey:    m.SFENKey,
		DepthLimit: int32(m.DepthLimit),
		Moves:      moves,
	})
	// 같거나 얕은 한계라 갱신하지 않으면 RETURNING 이 아무 행도 안 준다. 에러가 아니다.
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// CountMatePositions 는 캐시에 쌓인 詰み 답의 개수다. CountPositions 와 같은 자리에 쓴다.
func (s *Store) CountMatePositions(ctx context.Context) (int64, error) {
	return s.q.CountMatePositions(ctx)
}

// ── 사용자 ───────────────────────────────────────────────

// UpsertUser 는 로그인한 사람을 찾거나 만든다. 어느 쪽이든 id 를 돌려준다.
func (s *Store) UpsertUser(ctx context.Context, provider, providerUID, displayName string) (int64, error) {
	id, err := s.q.UpsertUser(ctx, db.UpsertUserParams{
		Provider:    provider,
		ProviderUid: providerUID,
		DisplayName: displayName,
	})
	if err != nil {
		return 0, fmt.Errorf("upsert user: %w", err)
	}
	return id, nil
}

// SkillEstimate 는 판 사이로 넘기는 실력 추정치다. skill.Estimate 와 같은 네 값이고,
// 그 패키지를 여기서 들여오지 않는 것은 추정기가 DB를 모르게 두기 위해서다(skill 패키지).
type SkillEstimate struct {
	// Loss 는 정규화된 낙폭(0~1).
	Loss float64
	// Samples 는 지금까지 본 판정 수의 누계.
	Samples int
	// AbsLoss 는 임계치로 나누지 않은 낙폭의 누적 평균이다. AbsSamples 가 0이면 뜻이 없다 —
	// 그 칸이 014_skill_absolute_loss.sql 뒤에 생겼다.
	AbsLoss float64
	// AbsSamples 는 AbsLoss 에 들어간 수의 개수다.
	AbsSamples int
}

// SkillProfile 은 그 사람의 지난 추정치다. 두 번째 값이 false면 「아직 모른다」다 —
// Loss 0은 「매 수 최선」이라 뜻이 정반대이므로 없는 것을 0으로 메우지 않는다.
func (s *Store) SkillProfile(ctx context.Context, userID int64) (SkillEstimate, bool, error) {
	row, err := s.q.GetSkillProfile(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return SkillEstimate{}, false, nil
	}
	if err != nil {
		return SkillEstimate{}, false, fmt.Errorf("get skill profile: %w", err)
	}
	if row.SkillLoss == nil {
		return SkillEstimate{}, false, nil
	}
	e := SkillEstimate{Loss: *row.SkillLoss, Samples: int(row.SkillSamples)}
	// 절대 낙폭은 따로 본다. 이 칸 없이 쌓인 행이 있어서(014_skill_absolute_loss.sql)
	// 하나가 있다고 다른 하나가 있는 것이 아니다.
	if row.SkillAbsLoss != nil {
		e.AbsLoss, e.AbsSamples = *row.SkillAbsLoss, int(row.SkillAbsSamples)
	}
	return e, true, nil
}

// SaveSkillEstimate 는 추정치를 덮는다. 판정 한 건마다 불린다(query/skill.sql).
func (s *Store) SaveSkillEstimate(ctx context.Context, userID int64, e SkillEstimate) error {
	loss := e.Loss
	params := db.SaveSkillEstimateParams{
		UserID:       userID,
		SkillLoss:    &loss,
		SkillSamples: int32(e.Samples),
	}
	// 표본이 없으면 NULL로 남긴다. 0을 적으면 「매 수 최선」이 되고, 그것이 段級의
	// 가장 센 이름이다(skill.RankOf).
	if e.AbsSamples > 0 {
		abs := e.AbsLoss
		params.SkillAbsLoss, params.SkillAbsSamples = &abs, int32(e.AbsSamples)
	}
	err := s.q.SaveSkillEstimate(ctx, params)
	if err != nil {
		return fmt.Errorf("save skill estimate: %w", err)
	}
	return nil
}

// SaveSkillEstimateIfSamples 는 저장된 표본 수가 expected 그대로일 때만 덮는다. 두 번째
// 값이 false면 그 사이에 다른 쪽이 썼다는 뜻이고, 아무것도 안 바뀐 것이다.
//
// 지난 값 위에 얹는 갱신을 오래 들고 있는 쪽이 쓴다(server/match_analysis.go). 그냥
// 덮으면 그 사이에 끝난 엔진 대국의 판정을 통째로 지운다.
func (s *Store) SaveSkillEstimateIfSamples(ctx context.Context, userID int64, e SkillEstimate, expected int) (bool, error) {
	loss := e.Loss
	params := db.SaveSkillEstimateIfSamplesParams{
		UserID:          userID,
		SkillLoss:       &loss,
		SkillSamples:    int32(e.Samples),
		ExpectedSamples: int32(expected),
	}
	// 표본이 없으면 NULL로 남긴다 — SaveSkillEstimate 와 같은 이유다.
	if e.AbsSamples > 0 {
		abs := e.AbsLoss
		params.SkillAbsLoss, params.SkillAbsSamples = &abs, int32(e.AbsSamples)
	}
	n, err := s.q.SaveSkillEstimateIfSamples(ctx, params)
	if err != nil {
		return false, fmt.Errorf("save skill estimate if samples: %w", err)
	}
	return n > 0, nil
}

// ── 매칭 레이팅 ──────────────────────────────────────────

// MatchRating 은 사람끼리 둔 판으로 움직이는 레이팅 한 사람 몫이다. rating.Rating 과
// 같은 두 값에 그것을 해석하는 데 필요한 셋이 붙는다 — 그 패키지를 여기서 들여오지 않는
// 것은 갱신식이 DB를 모르게 두기 위해서다(rating 패키지).
type MatchRating struct {
	// Value·Deviation 은 레이팅과 그 불확실성이다. Games 가 0이면 둘 다 뜻이 없다.
	Value     float64
	Deviation float64
	// Games 는 반영된 대인전 판 수다. 0이 「아직 레이팅이 없다」다(013_match_rating.sql).
	Games int
	// UpdatedAt 은 레이팅이 마지막으로 움직인 시각이다. 안 둔 시간만큼 불확실성을
	// 되돌리는 데 쓴다. Games 가 0이면 제로값이다.
	UpdatedAt time.Time
	// Skill 은 레이팅이 없을 때 시드를 만드는 재료다. SkillKnown 이 false 면 그것도 없다.
	//
	// 절대 낙폭 칸은 여기서 안 채운다 — 시드가 정규화값을 보고(rating.SeedFromLoss)
	// 질의도 그 둘만 읽는다(query/rating.sql). 段級을 여기서 붙이면 언제나 「모른다」다.
	Skill      SkillEstimate
	SkillKnown bool
}

// MatchRating 은 그 사람의 레이팅이다. 행이 없어도 에러가 아니다 — Games 0으로 온다.
//
// 시드도 불확실성 복원도 여기서 안 한다. 그건 갱신식과 같은 자리에 있어야 하고
// (internal/rating) 여기가 그것을 하면 상수가 두 벌이 된다.
func (s *Store) MatchRating(ctx context.Context, userID int64) (MatchRating, error) {
	row, err := s.q.GetRating(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return MatchRating{}, nil
	}
	if err != nil {
		return MatchRating{}, fmt.Errorf("get rating %d: %w", userID, err)
	}
	out := MatchRating{
		Value:     row.RatingEst,
		Deviation: row.RatingSd,
		Games:     int(row.RatingGames),
		UpdatedAt: row.RatingUpdatedAt.Time,
	}
	if row.SkillLoss != nil {
		out.Skill = SkillEstimate{Loss: *row.SkillLoss, Samples: int(row.SkillSamples)}
		out.SkillKnown = true
	}
	return out, nil
}

// SaveMatchRatings 는 한 판의 두 사람을 같이 옮긴다. Games 와 시각은 질의가 정한다.
//
// 두 id 가 같으면 부르지 않는다. 그러면 질의가 거절하고(query/rating.sql) 한 판이
// 반영되지 않은 채 에러 한 줄만 남는다.
func (s *Store) SaveMatchRatings(ctx context.Context, aID int64, a MatchRating, bID int64, b MatchRating) error {
	if aID == bID {
		return fmt.Errorf("save match ratings: both sides are user %d", aID)
	}
	err := s.q.SaveMatchRatings(ctx, db.SaveMatchRatingsParams{
		UserID:      aID,
		RatingEst:   a.Value,
		RatingSd:    a.Deviation,
		UserID_2:    bID,
		RatingEst_2: b.Value,
		RatingSd_2:  b.Deviation,
	})
	if err != nil {
		return fmt.Errorf("save match ratings for %d and %d: %w", aID, bID, err)
	}
	return nil
}

// ── 대국 기록 ────────────────────────────────────────────

// GameResult 는 games.result 에 들어가는 값이다.
//
// 셋만 「끝난 판」이다 — win·loss·draw. 화면이 읽는 질의가 그 셋으로 거르므로
// (query/games.sql), 아래 둘은 클라이언트에 아예 안 나간다(journal §51).
//
// 칸에 CHECK 가 없어서 값을 늘리는 데 마이그레이션이 필요 없다. 대신 여기가 유일한
// 어휘 목록이다 — 001_init.sql 의 칸 주석은 declined 를 모른다(적용된 마이그레이션은
// 안 고친다).
type GameResult string

const (
	ResultWin       GameResult = "win"
	ResultLoss      GameResult = "loss"
	ResultDraw      GameResult = "draw"
	ResultAbandoned GameResult = "abandoned" // 끝나지 않고 연결이 끊겼다. 이어할 수 있다
	// ResultDeclined 는 abandoned 인 판을 사람이 안 이어하겠다고 답한 것이다.
	// 갈라 두는 이유는 하나뿐이다 — 다시 물어보지 않기 위해서다(ResumableGame).
	ResultDeclined GameResult = "declined"
)

// CreateGame 은 대국 하나를 연다. userID 가 nil이면 로그인 전 대국이다.
//
// openingID 는 사람이 고른 상대의 진형(internal/book)이다. 빈 값이면 「おまかせ」.
func (s *Store) CreateGame(ctx context.Context, userID *int64, myColor, startSFEN, openingID string) (int64, error) {
	arg := db.CreateGameParams{
		UserID:    userID,
		MyColor:   myColor,
		StartSfen: &startSFEN,
	}
	if openingID != "" {
		arg.OpeningTag = &openingID
	}
	id, err := s.q.CreateGame(ctx, arg)
	if err != nil {
		return 0, fmt.Errorf("create game: %w", err)
	}
	return id, nil
}

// CreateMatchGame 은 대인전 한 판의 한쪽 몫을 연다. 같은 대국에서 두 번 불려
// 행 두 개가 된다 — 그 둘을 다시 묶는 열쇠가 matchID 다(012_match_games.sql).
//
// CreateGame 과 갈라 둔 이유는 채우는 칸이 다르기 때문이다. 저쪽은 opening_tag
// (컴퓨터의 진형)를 채우고 이쪽은 match_id 를 채운다 — 한 함수로 두면 부르는 쪽마다
// 「이번엔 어느 칸을 비우나」를 알아야 한다.
func (s *Store) CreateMatchGame(ctx context.Context, userID int64, myColor, startSFEN, matchID string) (int64, error) {
	id, err := s.q.CreateMatchGame(ctx, db.CreateMatchGameParams{
		UserID:    &userID,
		MyColor:   myColor,
		StartSfen: &startSFEN,
		MatchID:   &matchID,
	})
	if err != nil {
		return 0, fmt.Errorf("create match game: %w", err)
	}
	return id, nil
}

// FinishGame 은 대국을 닫는다.
// CreateImportedGame 은 밖에서 둔 판을 취해 온 자리다. 자리가 하나다 — 상대의 몫은
// 안 만든다(대인전이 행 둘인 것과 갈리는 자리다).
//
// notation 은 무엇으로 읽었는가다(kifu.Notation). 이 칸이 곧 「취해 온 판인가」이기도
// 해서 빈 값으로 오면 안 된다 — 그러면 여기서 둔 판과 구별이 없어진다.
func (s *Store) CreateImportedGame(ctx context.Context, userID int64, myColor, startSFEN, notation string) (int64, error) {
	if notation == "" {
		return 0, errors.New("store: an imported game needs a notation")
	}
	id, err := s.q.CreateImportedGame(ctx, db.CreateImportedGameParams{
		UserID:       &userID,
		MyColor:      myColor,
		StartSfen:    &startSFEN,
		ImportedFrom: &notation,
	})
	if err != nil {
		return 0, fmt.Errorf("create imported game: %w", err)
	}
	return id, nil
}

// CountImportsSince 는 그 사람이 그 시각 이후로 취해 온 판 수다. 하루 몫의 벽이 이
// 값으로 선다(server/kifu_import.go).
func (s *Store) CountImportsSince(ctx context.Context, userID int64, since time.Time) (int, error) {
	n, err := s.q.CountImportsSince(ctx, db.CountImportsSinceParams{UserID: &userID, StartedAt: stamp(since)})
	if err != nil {
		return 0, fmt.Errorf("count imports: %w", err)
	}
	return int(n), nil
}

func (s *Store) FinishGame(ctx context.Context, gameID int64, result GameResult) error {
	r := string(result)
	if err := s.q.FinishGame(ctx, db.FinishGameParams{ID: gameID, Result: &r}); err != nil {
		return fmt.Errorf("finish game: %w", err)
	}
	return nil
}

// ── 이어하기 ─────────────────────────────────────────────
// 방향은 journal §46, 정한 것은 §51. 세션을 살려 두지 않는다 — 여기 있는
// 것은 전부 기록 쪽이고, 국면은 기보에서 다시 만든다(server/ws.go).

// ResumableGame 은 이어할 수 있는 판의 머리다. 화면이 물음 카드를 그리는 데 쓴다.
type ResumableGame struct {
	ID        int64
	MyColor   string
	StartedAt time.Time
	// OpeningID 는 그때 고른 상대의 진형이다. 「おまかせ」였으면 빈 값.
	OpeningID string
	// StartSFEN 은 그 판의 0手目다. 비어 있으면 평수 — 카드가 手合割을 말하는 근거다
	// (GameSummary.StartSFEN 과 같은 규약).
	StartSFEN string
	MoveCount int
}

// ResumableGame 은 이 사람이 이어할 수 있는 가장 최근 판을 준다. 없으면 ErrNoGame.
//
// 익명은 부를 자리가 없다 — userID 를 값으로 받으므로 부르는 쪽이 로그인을 이미
// 확인했다는 뜻이다(server/resume.go).
func (s *Store) ResumableGame(ctx context.Context, userID int64) (ResumableGame, error) {
	row, err := s.q.ResumableGameForOwner(ctx, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ResumableGame{}, ErrNoGame
	}
	if err != nil {
		return ResumableGame{}, fmt.Errorf("resumable game: %w", err)
	}
	out := ResumableGame{
		ID:        row.ID,
		MyColor:   row.MyColor,
		StartedAt: row.StartedAt.Time,
		StartSFEN: deref(row.StartSfen),
		MoveCount: int(row.MoveCount),
	}
	if row.OpeningTag != nil {
		out.OpeningID = *row.OpeningTag
	}
	return out, nil
}

// ClaimedGame 은 이어하기가 점유한 판이다. 새 세션을 세우는 데 필요한 것 전부다.
type ClaimedGame struct {
	ID        int64
	MyColor   string
	StartSFEN string
	OpeningID string
}

// ClaimGameForResume 은 판 하나를 이어하기로 점유하고 되연다. 없거나 남의 것이거나
// 이미 누가 점유했으면 ErrNoGame — 셋을 구별해서 돌려주지 않는다(GameRecord 와 같다).
//
// 점유가 곧 되열기다(query/games.sql). 그래서 이 함수가 성공한 뒤 세션이 서지 못하면
// 그 판은 result 가 NULL인 채로 남는데, 기록 쪽이 ctx 취소에서 다시 abandoned 로
// 닫는다(server/recorder.go) — 되돌리는 코드를 따로 두지 않는 이유다.
func (s *Store) ClaimGameForResume(ctx context.Context, gameID, userID int64) (ClaimedGame, error) {
	row, err := s.q.ClaimGameForResume(ctx, db.ClaimGameForResumeParams{ID: gameID, UserID: &userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ClaimedGame{}, ErrNoGame
	}
	if err != nil {
		return ClaimedGame{}, fmt.Errorf("claim game %d: %w", gameID, err)
	}
	out := ClaimedGame{ID: row.ID, MyColor: row.MyColor}
	if row.StartSfen != nil {
		out.StartSFEN = *row.StartSfen
	}
	if row.OpeningTag != nil {
		out.OpeningID = *row.OpeningTag
	}
	return out, nil
}

// DeclineResume 은 「いいえ」를 남긴다. 없거나 남의 것이거나 이미 답한 판이면 ErrNoGame.
func (s *Store) DeclineResume(ctx context.Context, gameID, userID int64) error {
	n, err := s.q.DeclineResume(ctx, db.DeclineResumeParams{ID: gameID, UserID: &userID})
	if err != nil {
		return fmt.Errorf("decline resume %d: %w", gameID, err)
	}
	if n == 0 {
		return ErrNoGame
	}
	return nil
}

// RecordUndo 는 사람이 스스로 무른 수를 남기고, 그 手数부터의 기보를 지운다.
//
// 순서가 규약이다. InsertUndo 가 game_moves 에서 평가치를 옮겨 담으므로
// (query/games.sql), 지우는 것이 먼저면 그 칸이 영영 NULL로 남는다.
//
// 지우는 범위가 무른 수 하나가 아닌 이유는 그 뒤에 상대의 응수가 이미 확정돼
// 있기 때문이다 — 사람의 수를 되돌리려면 그 응수도 같이 사라져야 판이 사람 차례로
// 돌아온다(game.state.undo 와 같은 자리).
func (s *Store) RecordUndo(ctx context.Context, gameID int64, ply int, usi string) error {
	if err := s.q.InsertUndo(ctx, db.InsertUndoParams{
		GameID: gameID,
		Ply:    int32(ply),
		USI:    usi,
	}); err != nil {
		return fmt.Errorf("insert undo: %w", err)
	}
	if err := s.q.DeleteMovesFrom(ctx, db.DeleteMovesFromParams{
		GameID: gameID,
		Ply:    int32(ply),
	}); err != nil {
		return fmt.Errorf("delete moves from %d: %w", ply, err)
	}
	return nil
}

// CountUndos 는 그 판에서 이미 무른 횟수다. 이어하는 판이 제한을 리셋하지 않게 한다
// (game.Config.UndoUsed).
func (s *Store) CountUndos(ctx context.Context, gameID int64) (int, error) {
	n, err := s.q.CountGameUndos(ctx, gameID)
	if err != nil {
		return 0, fmt.Errorf("count undos of game %d: %w", gameID, err)
	}
	return int(n), nil
}

// InsertMove 는 확정된 수 하나를 기보에 넣는다.
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

// SetMoveEval 은 그 手数의 평가치를 나중에 채운다. 先手 관점 cp 다(journal §26).
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
	// BestCp·AfterCp 는 낙폭을 만든 두 원본이다(수번 측 관점). 제지형만.
	//
	// 둘 다 0이면 안 적는다 — 판정을 안 거친 행과 「정말로 0cp였다」를 섞지 않기 위해서다.
	// 호각인 국면에서 개입이 걸릴 일은 없으므로 이 규칙이 실제 값을 버리지는 않는다.
	BestCp  int
	AfterCp int
}

// InsertIntervention 은 개입 하나를 남긴다.
//
// 같은 ply에 여러 번 불릴 수 있다. 그 반복이 곧 「그 국면이 그 사람에게 얼마나
// 어려웠나」이고, 그래서 (game_id, ply) 는 유니크가 아니다(journal §17).
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
	if iv.BestCp != 0 || iv.AfterCp != 0 {
		b, a := int32(iv.BestCp), int32(iv.AfterCp)
		arg.BestCp, arg.AfterCp = &b, &a
	}

	if err := s.q.InsertIntervention(ctx, arg); err != nil {
		return fmt.Errorf("insert intervention: %w", err)
	}
	return nil
}

// CountGames 는 games 행 수다. 기록이 실제로 쌓이는지 확인하는 데 쓴다.
func (s *Store) CountGames(ctx context.Context) (int64, error) { return s.q.CountGames(ctx) }

// CountInterventions 는 interventions 행 수다. CountGames 와 같은 자리에서 쓴다.
func (s *Store) CountInterventions(ctx context.Context) (int64, error) {
	return s.q.CountInterventions(ctx)
}

// ── 리뷰(읽기) ───────────────────────────────────────────
//
// 여기까지가 쓰는 쪽이었다. 아래가 꺼내는 쪽이고, 리뷰 화면이 유일한 소비자다.

// GameSummary 는 리뷰 목록의 한 줄이다.
type GameSummary struct {
	ID      int64
	MyColor string
	// StartedAt·FinishedAt. FinishedAt 은 zero 일 수 있다 — 아직 두는 중인 판이다.
	StartedAt  time.Time
	FinishedAt time.Time
	// Result 는 끝나지 않았으면 빈 문자열이다.
	Result            GameResult
	MoveCount         int
	InterventionCount int
	// MatchID 가 비어 있지 않으면 사람 대 사람 대국이다(012_match_games.sql).
	//
	// 그 판에는 평가치도 개입도 없다. 대인전은 엔진을 안 부르므로(internal/match)
	// GameRecord.Moves[].EvalCp 가 전부 nil 이고 Interventions 가 빈 목록이다 —
	// 읽는 쪽이 그것을 「블런더가 0건인 좋은 판」으로 그리면 거짓이 되므로, 총평과 퀴즈가
	// 이 값을 보고 그 자리를 닫는다(server/review.go · quiz.go).
	MatchID string
	// StartSFEN 은 그 판의 0手目다. 비어 있으면 평수 초기 국면이다(game.Config.StartSFEN
	// 과 같은 규약).
	//
	// 手合割을 되짚는 유일한 칸이다(internal/handicap 의 Of). 이름을 따로 저장하지
	// 않으므로 이 값과 실제 판이 갈릴 자리가 없고, 그래서 마이그레이션도 필요 없었다.
	StartSFEN string
	// Imported 는 밖에서 둔 판을 취해 온 것인가다(020_imported_games.sql).
	//
	// 그 판에도 평가치와 개입이 있다 — 사후 분석이 채운다(server/kifu_analysis.go).
	// 갈리는 것은 그 개입을 아무도 안 막았다는 것뿐이고, 화면이 그 값으로 표기를
	// 「止められた手」에서 「悪手」로 옮긴다.
	Imported bool
}

// RecordedMove 는 기보의 한 수다.
type RecordedMove struct {
	Ply int
	USI string
	// EvalCp 는 先手 관점 cp이고 nil일 수 있다 — 평가치는 수보다 늦게 오므로
	// 연결이 끊긴 판의 마지막 몇 수는 안 채워진 채로 남는다.
	EvalCp *int
}

// RecordedIntervention 은 남아 있는 개입 하나다.
//
// 문구는 없다. 화면에 나갔던 일본어 문장은 기록하지 않으므로(카테고리만 남는다),
// 리뷰는 그 문장을 다시 만들어야 한다 — 카테고리가 정본이고 문장은 파생이다.
type RecordedIntervention struct {
	Ply          int
	Kind         string
	Category     string
	DeltaWin     float64
	LevelBucket  string
	RetractedUSI string
	// BestCp·AfterCp 는 낙폭을 만든 두 원본이다(수번 측 관점). 없을 수 있다 —
	// migrations/005 앞에 기록된 판에는 영원히 없다. 버린 값은 되찾을 수 없고,
	// 화면은 그 자리를 다시 재서 채운다.
	BestCp  *int
	AfterCp *int
}

// RecordedUndo 는 사람이 스스로 무른 수 하나다.
//
// 개입(RecordedIntervention)과 갈라 둔다. 판이 되돌아간 것은 같지만 시작한 쪽이
// 반대라, 한 목록에 섞으면 「AI가 막았다」와 「내가 무르고 싶었다」가 같은 줄이 된다 —
// 되짚기에서 그 둘은 정반대의 이야기다(008_game_undos.sql).
type RecordedUndo struct {
	Ply int
	USI string
	// EvalCp 는 先手 관점 cp이고 nil일 수 있다 — 무를 때 판정이 아직 그 手数를
	// 안 채웠으면 옮겨 담을 값이 없다(RecordUndo).
	EvalCp *int
}

// GameRecord 는 한 판 전체다.
type GameRecord struct {
	GameSummary
	// OpeningID 는 그 판에서 사람이 고른 상대의 진형 id다(internal/book).
	// 「おまかせ」였으면 빈 값이다.
	OpeningID     string
	Moves         []RecordedMove
	Interventions []RecordedIntervention
	// Undos 는 사람이 스스로 무른 수들이다. 기보에는 없다 — 무르기가 지웠다.
	Undos []RecordedUndo
}

// ErrNoGame 은 그런 대국이 없을 때.
var ErrNoGame = errors.New("store: game not found")

// ListGames 는 그 사람의 최근 대국을 최신부터 준다. ownerID 가 nil이면 익명 판이다.
// 한 수도 안 둔 판은 안 온다(games.sql).
//
// 화면이 쓰는 것은 이쪽이다. 주인을 안 보는 ListGamesAnyOwner 는 측정 전용이고,
// 안전한 쪽이 짧은 이름을 갖는다 — 나중에 손이 먼저 닿는 것이 그쪽이어야 한다.
//
// limit 을 여기서 자른다 — 자르는 변환을 하는 자리가 스스로 막아야 한다. int32(limit)
// 이 큰 값을 조용히 음수로 만들면 LIMIT 이 거짓말을 한다.
func (s *Store) ListGames(ctx context.Context, limit int, ownerID *int64) ([]GameSummary, error) {
	rows, err := s.q.ListGamesForOwner(ctx, db.ListGamesForOwnerParams{
		Limit:   listLimit(limit),
		OwnerID: ownerID,
	})
	if err != nil {
		return nil, fmt.Errorf("list games: %w", err)
	}
	out := make([]GameSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, summaryOf(gameHead{
			ID: r.ID, MyColor: r.MyColor,
			StartedAt: r.StartedAt.Time, FinishedAt: r.FinishedAt.Time,
			Result: r.Result, StartSFEN: r.StartSfen, MatchID: r.MatchID,
			ImportedFrom: r.ImportedFrom,
		}, r.MoveCount, r.InterventionCount))
	}
	return out, nil
}

// ListGamesAnyOwner 는 주인을 안 보고 전부 준다. 측정 전용이다 —
// 상수 재채점이 기록된 판을 가로질러 읽는다(journal §39). HTTP 표면에서 부르면
// 그 순간 남의 기보가 열린다(02-architecture.md §7 위협 2).
func (s *Store) ListGamesAnyOwner(ctx context.Context, limit int) ([]GameSummary, error) {
	rows, err := s.q.ListGames(ctx, listLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list games: %w", err)
	}
	out := make([]GameSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, summaryOf(gameHead{
			ID: r.ID, MyColor: r.MyColor,
			StartedAt: r.StartedAt.Time, FinishedAt: r.FinishedAt.Time,
			Result: r.Result, StartSFEN: r.StartSfen, MatchID: r.MatchID,
			ImportedFrom: r.ImportedFrom,
		}, r.MoveCount, r.InterventionCount))
	}
	return out, nil
}

// PlayerTally 는 마이페이지가 읽는 두 벌의 세기다. 모집단이 하나다 — 두 질의가 같은
// 조건으로 걸러서(query/games.sql), 「12판 뒀는데 약점은 30판에서 나온 것」이 될 수 없다.
type PlayerTally struct {
	// Results 는 결과별 판 수다. 키는 GameResult 이고 끝난 셋뿐이다.
	Results map[GameResult]int
	// Categories 는 카테고리별 개입 횟수다. 키는 intervene.Category 의 코드 문자열 —
	// 이 패키지가 그쪽을 들여오지 않는 것은 SkillEstimate 와 같은 이유다.
	Categories map[string]int
	// StyleTags 는 이름별 판 수다. 키는 tag.Tag.Code 이고, 위 둘과 달리 횟수가
	// 아닌 것은 한 판에 같은 이름이 한 번만 담기기 때문이다(AddGameStyleTag).
	StyleTags map[string]int
}

// PlayerTally 는 그 사람의 전적과 약점을 한 번에 센다. ownerID 가 nil이면 익명 판이다.
//
// 한 함수인 이유는 같은 모집단에서 나와야 하기 때문이다 — 갈라 두면 나중에 한쪽 질의의
// 조건만 고쳐지고, 그때 화면의 두 숫자가 조용히 다른 것을 세게 된다(server/summary.go 의
// factsOf 가 같은 이유로 한 함수다).
func (s *Store) PlayerTally(ctx context.Context, ownerID *int64) (PlayerTally, error) {
	out := PlayerTally{
		Results:    map[GameResult]int{},
		Categories: map[string]int{},
		StyleTags:  map[string]int{},
	}

	results, err := s.q.CountGameResultsForOwner(ctx, ownerID)
	if err != nil {
		return out, fmt.Errorf("count game results: %w", err)
	}
	for _, r := range results {
		// result 는 질의가 셋으로 걸렀으므로 NULL이 안 온다. 그래도 확인하는 것은
		// 컬럼이 nullable 이라 생성 타입이 포인터이기 때문이다.
		if r.Result == nil {
			continue
		}
		out.Results[GameResult(*r.Result)] = int(r.Games)
	}

	cats, err := s.q.CountInterventionCategoriesForOwner(ctx, ownerID)
	if err != nil {
		return out, fmt.Errorf("count intervention categories: %w", err)
	}
	for _, c := range cats {
		if c.Category == nil {
			continue
		}
		out.Categories[*c.Category] = int(c.Hits)
	}

	styles, err := s.q.CountGameStyleTagsForOwner(ctx, ownerID)
	if err != nil {
		return out, fmt.Errorf("count game style tags: %w", err)
	}
	for _, t := range styles {
		out.StyleTags[t.Code] = int(t.Games)
	}
	return out, nil
}

// AddStyleTag 는 그 판에서 사람이 짠 이름 하나를 남긴다. 같은 이름을 두 번 담지 않는다 —
// 거르는 자리가 질의에도 있는 이유는 query/games.sql.
func (s *Store) AddStyleTag(ctx context.Context, gameID int64, code string) error {
	return s.q.AddGameStyleTag(ctx, db.AddGameStyleTagParams{GameID: gameID, Code: code})
}

func listLimit(limit int) int32 {
	if limit < 1 {
		return 1
	}
	if limit > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(limit)
}

// summaryOf 는 머리와 두 세기를 한 줄로 만든다.
//
// 머리를 구조체로 받는다. *string 이 셋이라(result·match_id·start_sfen) 위치 인자로
// 늘어놓으면 두 개를 바꿔 넣어도 컴파일이 되고, 그 버그는 목록 화면에서 「전부 平手」로만
// 드러난다.
func summaryOf(h gameHead, moves, ivs int64) GameSummary {
	return GameSummary{
		ID:                h.ID,
		MyColor:           h.MyColor,
		StartedAt:         h.StartedAt,
		FinishedAt:        h.FinishedAt,
		Result:            resultValue(h.Result),
		MoveCount:         int(moves),
		InterventionCount: int(ivs),
		MatchID:           deref(h.MatchID),
		StartSFEN:         deref(h.StartSFEN),
		Imported:          h.ImportedFrom != nil,
	}
}

// gameHead 는 한 판의 머리다. 주인을 보는 질의와 안 보는 질의가 같은 칸을 다른
// 행 타입으로 주므로, 아래 읽는 코드를 한 벌로 두려고 여기서 만난다 —
// 목록 두 질의도 같은 이유로 여기서 만난다(summaryOf).
type gameHead struct {
	ID         int64
	MyColor    string
	StartedAt  time.Time
	FinishedAt time.Time
	Result     *string
	StartSFEN  *string
	OpeningTag *string
	MatchID    *string
	// ImportedFrom 은 무엇으로 읽었는가다. 값은 밖으로 안 나가고 「NULL 이 아닌가」만
	// 나간다(020_imported_games.sql).
	ImportedFrom *string
}

// GameRecord 는 그 사람의 한 판을 통째로 읽는다. ownerID 가 nil이면 익명 판이다.
// 없거나 남의 것이면 ErrNoGame — 둘을 구별해서 돌려주지 않는다. 「없다」와 「당신 것이
// 아니다」가 갈리면 그것만으로 남의 판이 몇 번까지 있는지 세어 볼 수 있다.
func (s *Store) GameRecord(ctx context.Context, gameID int64, ownerID *int64) (GameRecord, error) {
	head, err := s.q.GetGameForOwner(ctx, db.GetGameForOwnerParams{ID: gameID, OwnerID: ownerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return GameRecord{}, ErrNoGame
	}
	if err != nil {
		return GameRecord{}, fmt.Errorf("get game %d: %w", gameID, err)
	}
	return s.recordOf(ctx, gameHead{
		ID: head.ID, MyColor: head.MyColor,
		StartedAt: head.StartedAt.Time, FinishedAt: head.FinishedAt.Time,
		Result: head.Result, StartSFEN: head.StartSfen, OpeningTag: head.OpeningTag,
		MatchID: head.MatchID, ImportedFrom: head.ImportedFrom,
	})
}

// GameRecordAnyOwner 는 주인을 안 본다. 측정 전용이다 — ListGamesAnyOwner 와 같은 자리다.
func (s *Store) GameRecordAnyOwner(ctx context.Context, gameID int64) (GameRecord, error) {
	head, err := s.q.GetGame(ctx, gameID)
	if errors.Is(err, pgx.ErrNoRows) {
		return GameRecord{}, ErrNoGame
	}
	if err != nil {
		return GameRecord{}, fmt.Errorf("get game %d: %w", gameID, err)
	}
	return s.recordOf(ctx, gameHead{
		ID: head.ID, MyColor: head.MyColor,
		StartedAt: head.StartedAt.Time, FinishedAt: head.FinishedAt.Time,
		Result: head.Result, StartSFEN: head.StartSfen, OpeningTag: head.OpeningTag,
		MatchID: head.MatchID, ImportedFrom: head.ImportedFrom,
	})
}

// recordOf 는 머리에 기보와 개입을 채운다.
//
// 트랜잭션으로 묶지 않는다. 기록은 덧붙이기만 하고, 두는 중인 판에서 최악이
// 「기보보다 개입이 한 수 앞선다」다 — 그 하나를 막자고 대국 중인 판을 잠그는 쪽이 비싸다.
func (s *Store) recordOf(ctx context.Context, head gameHead) (GameRecord, error) {
	gameID := head.ID

	moves, err := s.q.ListGameMoves(ctx, gameID)
	if err != nil {
		return GameRecord{}, fmt.Errorf("list moves of game %d: %w", gameID, err)
	}
	ivs, err := s.q.ListGameInterventions(ctx, gameID)
	if err != nil {
		return GameRecord{}, fmt.Errorf("list interventions of game %d: %w", gameID, err)
	}
	undos, err := s.q.ListGameUndos(ctx, gameID)
	if err != nil {
		return GameRecord{}, fmt.Errorf("list undos of game %d: %w", gameID, err)
	}

	// 개입 횟수에 무르기를 안 더한다. 목록의 그 숫자는 「AI가 몇 번 막았나」이고
	// (journal §72), 사람이 스스로 무른 것을 섞으면 개입이 실제보다 잦아 보인다.
	out := GameRecord{
		GameSummary:   summaryOf(head, int64(len(moves)), int64(len(ivs))),
		OpeningID:     deref(head.OpeningTag),
		Moves:         make([]RecordedMove, 0, len(moves)),
		Interventions: make([]RecordedIntervention, 0, len(ivs)),
		Undos:         make([]RecordedUndo, 0, len(undos)),
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
		rec := RecordedIntervention{
			Ply:          int(iv.Ply),
			Kind:         iv.Kind,
			Category:     deref(iv.Category),
			DeltaWin:     derefFloat(iv.DeltaWin),
			LevelBucket:  deref(iv.LevelBucket),
			RetractedUSI: deref(iv.RetractedUsi),
		}
		// 없는 것과 0을 갈라 둔다. 0cp는 호각이고, 없는 것은 migrations/005 앞의 행이다.
		if iv.BestCp != nil {
			cp := int(*iv.BestCp)
			rec.BestCp = &cp
		}
		if iv.AfterCp != nil {
			cp := int(*iv.AfterCp)
			rec.AfterCp = &cp
		}
		out.Interventions = append(out.Interventions, rec)
	}
	for _, u := range undos {
		rec := RecordedUndo{Ply: int(u.Ply), USI: u.USI}
		if u.EvalCp != nil {
			cp := int(*u.EvalCp)
			rec.EvalCp = &cp
		}
		out.Undos = append(out.Undos, rec)
	}
	return out, nil
}

// ErrNoQuiz 는 그 판에 쓸 수 있는 퀴즈가 없을 때.
var ErrNoQuiz = errors.New("store: quiz not generated")

// GameQuiz 는 저장된 퀴즈를 판이 맞을 때만 준다. 안 맞으면 ErrNoQuiz 다 —
// 「옛 판을 무시한다」를 여기 한 자리에서 걸어야 부르는 쪽마다 다시 비교하지 않는다
// (migrations/007).
//
// 바이트를 그대로 준다. 문항의 모양은 internal/quiz 것이고, 이 패키지가 그걸 알면
// 채점 규약이 두 곳에 적힌다.
func (s *Store) GameQuiz(ctx context.Context, gameID int64, version int) ([]byte, error) {
	row, err := s.q.GetGameQuiz(ctx, gameID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoQuiz
	}
	if err != nil {
		return nil, err
	}
	if int(row.Version) != version {
		return nil, ErrNoQuiz
	}
	return row.Payload, nil
}

// SaveGameQuiz 는 만든 퀴즈를 남긴다.
func (s *Store) SaveGameQuiz(ctx context.Context, gameID int64, version int, payload []byte) error {
	return s.q.UpsertGameQuiz(ctx, db.UpsertGameQuizParams{
		GameID:  gameID,
		Version: int32(version),
		Payload: payload,
	})
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

// ─── 부른 힌트 ───────────────────────────────────────────────
// 사람이 불러서 받은 최선수 힌트. 개입과 갈라 두는 이유는 010_game_hints.sql.

// HintUse 는 이어하는 판이 되찾아야 하는 것 전부다.
//
// 둘이 필요하다. 예산은 판 전체의 수이고(Used), 「이 국면은 어디까지 봤나」는
// 국면마다다(Stages) — 하나만으로는 3회째를 막지도, 예산을 이어받지도 못한다.
type HintUse struct {
	Used int
	// Stages 는 sfen_key 마다 열린 마지막 단계다.
	Stages map[string]int
}

// RecordHint 는 부른 힌트 한 번을 남긴다.
func (s *Store) RecordHint(ctx context.Context, gameID int64, ply int, sfenKey string, stage int, bestUSI string) error {
	return s.q.InsertHint(ctx, db.InsertHintParams{
		GameID:  gameID,
		Ply:     int32(ply),
		SFENKey: sfenKey,
		Stage:   int32(stage),
		BestUsi: bestUSI,
	})
}

// HintsUsed 는 그 판이 지금까지 쓴 힌트다. 이어하기가 세션을 세우기 전에 읽는다.
func (s *Store) HintsUsed(ctx context.Context, gameID int64) (HintUse, error) {
	out := HintUse{Stages: map[string]int{}}

	n, err := s.q.CountGameHints(ctx, gameID)
	if err != nil {
		return out, fmt.Errorf("count game hints: %w", err)
	}
	out.Used = int(n)

	stages, err := s.q.CountGameHintStages(ctx, gameID)
	if err != nil {
		return out, fmt.Errorf("count game hint stages: %w", err)
	}
	for _, r := range stages {
		out.Stages[r.SFENKey] = int(r.Stage)
	}
	return out, nil
}

// MarkHintTaken 은 알려준 수를 실제로 뒀는지를 나중에 채운다.
func (s *Store) MarkHintTaken(ctx context.Context, gameID int64, sfenKey string, taken bool) error {
	return s.q.MarkHintTaken(ctx, db.MarkHintTakenParams{GameID: gameID, SFENKey: sfenKey, Taken: &taken})
}

// ─── 검토에서 저장한 국면 ─────────────────────────────────────
// 手合割 id 하나와 0手目부터의 수순 한 줄이다. SFEN 칸이 없는 이유는 015 마이그레이션에 있다.

// ExploreSnapshot 은 저장된 국면 하나다. 이름에 Explore 가 붙는 것은 game.Snapshot 과
// 갈라야 해서다 — 저쪽은 두는 중인 판이 화면에 보내는 지금 국면이다.
type ExploreSnapshot struct {
	ID   int64
	Name string
	// Handicap 은 手合割 id다. 빈 값이 平手다(internal/handicap).
	Handicap  string
	Moves     []string
	CreatedAt time.Time
}

// ErrNoSnapshot 은 그 번호의 국면이 없거나 남의 것일 때. 둘을 안 가르는 이유는
// ErrNoGame 과 같다.
var ErrNoSnapshot = errors.New("store: explore snapshot not found")

// SaveExploreSnapshot 은 국면 하나를 남긴다. 개수를 안 막는다(query/explore.sql).
func (s *Store) SaveExploreSnapshot(ctx context.Context, userID int64, name, handicap string, moves []string) (ExploreSnapshot, error) {
	// nil 을 빈 배열로 바꾼다. moves 가 text[] NOT NULL 이라 nil 슬라이스는 pgx 가 NULL 로
	// 보내고 삽입이 제약에서 떨어진다 — 0手目 저장이 그 자리다.
	if moves == nil {
		moves = []string{}
	}
	row, err := s.q.CreateExploreSnapshot(ctx, db.CreateExploreSnapshotParams{
		UserID:   userID,
		Name:     name,
		Handicap: handicap,
		Moves:    moves,
	})
	if err != nil {
		return ExploreSnapshot{}, fmt.Errorf("create explore snapshot: %w", err)
	}
	return ExploreSnapshot{
		ID:        row.ID,
		Name:      name,
		Handicap:  handicap,
		Moves:     moves,
		CreatedAt: row.CreatedAt.Time,
	}, nil
}

// ExploreSnapshots 는 그 사람이 저장한 국면 전부다. 최근에 저장한 것이 앞이다.
func (s *Store) ExploreSnapshots(ctx context.Context, userID int64) ([]ExploreSnapshot, error) {
	rows, err := s.q.ListExploreSnapshots(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list explore snapshots: %w", err)
	}
	out := make([]ExploreSnapshot, 0, len(rows))
	for _, r := range rows {
		// 빈 줄도 빈 배열로 준다. nil 은 JSON 에서 null 이라, 화면이 0手目 국면과 「수순이
		// 없다」를 가르는 분기를 하나 더 갖게 된다.
		moves := r.Moves
		if moves == nil {
			moves = []string{}
		}
		out = append(out, ExploreSnapshot{
			ID:        r.ID,
			Name:      r.Name,
			Handicap:  r.Handicap,
			Moves:     moves,
			CreatedAt: r.CreatedAt.Time,
		})
	}
	return out, nil
}

// RenameExploreSnapshot 은 이름만 고친다. 없거나 남의 것이면 ErrNoSnapshot.
//
// 수순을 같은 행에 덮어쓰지 않는다 — 옛 이름이 가리키던 국면이 조용히 달라진다.
func (s *Store) RenameExploreSnapshot(ctx context.Context, id, userID int64, name string) error {
	n, err := s.q.RenameExploreSnapshot(ctx, db.RenameExploreSnapshotParams{ID: id, UserID: userID, Name: name})
	if err != nil {
		return fmt.Errorf("rename explore snapshot %d: %w", id, err)
	}
	if n == 0 {
		return ErrNoSnapshot
	}
	return nil
}

// DeleteExploreSnapshot 은 국면 하나를 지운다. 없거나 남의 것이면 ErrNoSnapshot.
func (s *Store) DeleteExploreSnapshot(ctx context.Context, id, userID int64) error {
	n, err := s.q.DeleteExploreSnapshot(ctx, db.DeleteExploreSnapshotParams{ID: id, UserID: userID})
	if err != nil {
		return fmt.Errorf("delete explore snapshot %d: %w", id, err)
	}
	if n == 0 {
		return ErrNoSnapshot
	}
	return nil
}
