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
type Candidate struct {
	USI string   `json:"usi"`
	Cp  int      `json:"cp"`
	PV  []string `json:"pv,omitempty"`
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
