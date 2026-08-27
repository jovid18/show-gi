package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jovid18/show-gi/apps/server/internal/store/db"
)

// 미리 재는 手의 큐(018)의 표 접근. 정책은 하나도 없다 — 리스 길이도 폴링 간격도 부르는
// 쪽이 준다(server.matchAnalyzer). 큐와 같은 규약이다.

// AnalysisPly 는 아직 안 잰 手 하나다. 재는 데 필요한 입력이 전부 여기 있다.
type AnalysisPly struct {
	MatchID string
	Ply     int
	// StartSFEN 은 그 판의 시작 국면이다. 手数를 안 뺀 원문이라 판정에 그대로 넘긴다.
	StartSFEN string
	// Moves 는 이 手까지의 수순이다(moves[:ply]).
	Moves []string
}

// MeasuredPly 는 미리 재 둔 手 하나다. server 의 judged 와 같은 칸이고, 그 타입을
// 안 쓰는 이유는 store 가 game·skill 을 모르는 채로 있어야 하기 때문이다.
//
// 평가치 둘은 先手 관점이다. 나머지 넷은 skill.Move 가 먹는 값 그대로다.
type MeasuredPly struct {
	Ply               int
	BeforeCp, AfterCp int
	Blunder           bool
	DeltaWin          float64
	Threshold         float64
	Decided           bool
}

// ErrNoAnalysisPly 는 지금 집을 手가 없다는 것 하나다.
var ErrNoAnalysisPly = errors.New("store: no ply to analyze")

// EnqueueAnalysisPly 는 방금 둔 手를 큐에 세운다. 같은 手를 두 번 세워도 한 행이다.
func (s *Store) EnqueueAnalysisPly(ctx context.Context, p AnalysisPly) error {
	err := s.q.EnqueueAnalysisPly(ctx, db.EnqueueAnalysisPlyParams{
		MatchID:   p.MatchID,
		Ply:       int32(p.Ply),
		StartSfen: p.StartSFEN,
		Moves:     p.Moves,
	})
	if err != nil {
		return fmt.Errorf("enqueue analysis ply: %w", err)
	}
	return nil
}

// ClaimAnalysisPly 는 안 잰 手 하나를 집는다. 없으면 ErrNoAnalysisPly.
//
// leaseBefore 보다 오래된 리스는 도로 집는다 — 워커가 사라진 手를 되찾는 자리다.
func (s *Store) ClaimAnalysisPly(ctx context.Context, leaseBefore time.Time) (AnalysisPly, error) {
	row, err := s.q.ClaimAnalysisPly(ctx, stamp(leaseBefore))
	if errors.Is(err, pgx.ErrNoRows) {
		return AnalysisPly{}, ErrNoAnalysisPly
	}
	if err != nil {
		return AnalysisPly{}, fmt.Errorf("claim analysis ply: %w", err)
	}
	return AnalysisPly{
		MatchID:   row.MatchID,
		Ply:       int(row.Ply),
		StartSFEN: row.StartSfen,
		Moves:     row.Moves,
	}, nil
}

// FinishAnalysisPly 는 잰 값을 그 행에 적는다. 행이 없으면 아무 일도 안 일어난다 —
// 판이 끝나 걷힌 뒤에 도착한 늦은 측정이 판을 되살리지 않는다(query/analysis.sql).
func (s *Store) FinishAnalysisPly(ctx context.Context, matchID string, m MeasuredPly) error {
	before, after := int32(m.BeforeCp), int32(m.AfterCp)
	err := s.q.FinishAnalysisPly(ctx, db.FinishAnalysisPlyParams{
		MatchID:   matchID,
		Ply:       int32(m.Ply),
		BeforeCp:  &before,
		AfterCp:   &after,
		Blunder:   &m.Blunder,
		DeltaWin:  &m.DeltaWin,
		Threshold: &m.Threshold,
		Decided:   &m.Decided,
	})
	if err != nil {
		return fmt.Errorf("finish analysis ply: %w", err)
	}
	return nil
}

// StopAnalysisAhead 는 그 판을 미리 재는 것을 그만둔다. 이미 잰 값은 남는다.
func (s *Store) StopAnalysisAhead(ctx context.Context, matchID string) error {
	if err := s.q.StopAnalysisAhead(ctx, matchID); err != nil {
		return fmt.Errorf("stop measuring ahead: %w", err)
	}
	return nil
}

// MeasuredAnalysisPlies 는 그 판에서 미리 재 둔 것을 手数 순으로 준다.
//
// NULL 인 칸은 제로값으로 온다. 그런 행은 만들어지지 않는다 — 일곱 칸이 한 UPDATE 에서
// 같이 차고 done_at 이 그 증거다(FinishAnalysisPly).
func (s *Store) MeasuredAnalysisPlies(ctx context.Context, matchID string) ([]MeasuredPly, error) {
	rows, err := s.q.MeasuredAnalysisPlies(ctx, matchID)
	if err != nil {
		return nil, fmt.Errorf("measured analysis plies: %w", err)
	}
	out := make([]MeasuredPly, 0, len(rows))
	for _, r := range rows {
		out = append(out, MeasuredPly{
			Ply:       int(r.Ply),
			BeforeCp:  derefInt32(r.BeforeCp),
			AfterCp:   derefInt32(r.AfterCp),
			Blunder:   derefBool(r.Blunder),
			DeltaWin:  derefFloat(r.DeltaWin),
			Threshold: derefFloat(r.Threshold),
			Decided:   derefBool(r.Decided),
		})
	}
	return out, nil
}

// CountMeasuredAnalysisPlies 는 그 판에서 미리 재 둔 手数다.
func (s *Store) CountMeasuredAnalysisPlies(ctx context.Context, matchID string) (int, error) {
	n, err := s.q.CountMeasuredAnalysisPlies(ctx, matchID)
	if err != nil {
		return 0, fmt.Errorf("count measured analysis plies: %w", err)
	}
	return int(n), nil
}

// CountAnalysisBacklog 는 아직 안 잰 手数다. AnalysisBacklogPlies 가 이 값이다.
func (s *Store) CountAnalysisBacklog(ctx context.Context) (int, error) {
	n, err := s.q.CountAnalysisBacklog(ctx)
	if err != nil {
		return 0, fmt.Errorf("count analysis backlog: %w", err)
	}
	return int(n), nil
}

// DiscardAnalysisMatch 는 그 판의 행을 걷는다.
func (s *Store) DiscardAnalysisMatch(ctx context.Context, matchID string) error {
	if err := s.q.DiscardAnalysisMatch(ctx, matchID); err != nil {
		return fmt.Errorf("discard analysis match: %w", err)
	}
	return nil
}

// SweepAnalysisPlies 는 그 시각보다 오래된 행을 걷는다. 판이 비정상으로 끝나 걷는 쪽이
// 안 돌았을 때 남는 행이 이 표의 유일한 누수다.
func (s *Store) SweepAnalysisPlies(ctx context.Context, before time.Time) error {
	if err := s.q.SweepAnalysisPlies(ctx, stamp(before)); err != nil {
		return fmt.Errorf("sweep analysis plies: %w", err)
	}
	return nil
}

func derefInt32(v *int32) int {
	if v == nil {
		return 0
	}
	return int(*v)
}

func derefBool(v *bool) bool { return v != nil && *v }

// 끝난 판의 큐(019)의 표 접근. 미리 재는 큐와 같은 규약이다 — 정책은 부르는 쪽이 든다.

// AnalysisJob 은 집어 온 판 하나다.
type AnalysisJob struct {
	MatchID string
	// Plies 는 아직 안 잰 手数다. 판의 길이가 아니라 이 큐에서 엔진을 부르는 양이다.
	Plies int
}

// MatchSeat 는 대인전 한 판의 한 자리다. games 행 하나가 곧 한 자리다.
//
// server 의 analysisSeat 와 같은 칸이고, 그 타입을 안 쓰는 이유는 store 가 shogi 를
// 모르는 채로 있어야 하기 때문이다 — 색이 코드 문자열로 온다.
type MatchSeat struct {
	GameID int64
	UserID int64
	// Color 는 b·w 다. games.my_color 그대로다.
	Color string
}

// ErrNoAnalysisJob 은 지금 집을 판이 없다는 것 하나다.
var ErrNoAnalysisJob = errors.New("store: no game to analyze")

// HoldAnalysisJob 은 판의 자리를 미리 세운다. 두 번 불려도 한 행이다.
func (s *Store) HoldAnalysisJob(ctx context.Context, matchID string) error {
	if err := s.q.HoldAnalysisJob(ctx, matchID); err != nil {
		return fmt.Errorf("hold analysis job: %w", err)
	}
	return nil
}

// ReadyAnalysisJob 은 자리가 다 찼다고 적는다. 그때부터 집힌다.
func (s *Store) ReadyAnalysisJob(ctx context.Context, matchID string, plies int) error {
	n := int32(plies)
	if err := s.q.ReadyAnalysisJob(ctx, db.ReadyAnalysisJobParams{MatchID: matchID, Plies: &n}); err != nil {
		return fmt.Errorf("ready analysis job: %w", err)
	}
	return nil
}

// ClaimAnalysisJob 은 자리가 찬 판 하나를 집는다. 없으면 ErrNoAnalysisJob.
func (s *Store) ClaimAnalysisJob(ctx context.Context, leaseBefore time.Time) (AnalysisJob, error) {
	row, err := s.q.ClaimAnalysisJob(ctx, stamp(leaseBefore))
	if errors.Is(err, pgx.ErrNoRows) {
		return AnalysisJob{}, ErrNoAnalysisJob
	}
	if err != nil {
		return AnalysisJob{}, fmt.Errorf("claim analysis job: %w", err)
	}
	return AnalysisJob{MatchID: row.MatchID, Plies: derefInt32(row.Plies)}, nil
}

// DropAnalysisJob 은 그 판을 큐에서 걷는다.
func (s *Store) DropAnalysisJob(ctx context.Context, matchID string) error {
	if err := s.q.DropAnalysisJob(ctx, matchID); err != nil {
		return fmt.Errorf("drop analysis job: %w", err)
	}
	return nil
}

// AnalysisJobBacklog 은 아직 안 집힌 판의 수와 그 판들이 안 잰 手数다.
func (s *Store) AnalysisJobBacklog(ctx context.Context, leaseBefore time.Time) (games, plies int, err error) {
	row, err := s.q.AnalysisJobBacklog(ctx, stamp(leaseBefore))
	if err != nil {
		return 0, 0, fmt.Errorf("analysis job backlog: %w", err)
	}
	return int(row.Games), int(row.Plies), nil
}

// IsGameAnalyzing 은 그 판이 아직 큐에 있거나 도는 중인가다.
func (s *Store) IsGameAnalyzing(ctx context.Context, gameID int64) (bool, error) {
	ok, err := s.q.IsGameAnalyzing(ctx, gameID)
	if err != nil {
		return false, fmt.Errorf("is game analyzing: %w", err)
	}
	return ok, nil
}

// MatchSeats 는 그 판의 자리들을 색 순으로 준다.
func (s *Store) MatchSeats(ctx context.Context, matchID string) ([]MatchSeat, error) {
	rows, err := s.q.MatchSeats(ctx, &matchID)
	if err != nil {
		return nil, fmt.Errorf("match seats: %w", err)
	}
	out := make([]MatchSeat, 0, len(rows))
	for _, r := range rows {
		// 익명 판은 여기 안 온다 — 대인전은 로그인이 전제라(journal §83) user_id 가 늘 있다.
		var user int64
		if r.UserID != nil {
			user = *r.UserID
		}
		out = append(out, MatchSeat{GameID: r.ID, UserID: user, Color: r.MyColor})
	}
	return out, nil
}

// SweepAnalysisJobs 는 그 시각보다 오래된 행을 걷는다. 자리가 영영 안 차는 반쪽 판이
// 이 표의 누수다.
func (s *Store) SweepAnalysisJobs(ctx context.Context, before time.Time) error {
	if err := s.q.SweepAnalysisJobs(ctx, stamp(before)); err != nil {
		return fmt.Errorf("sweep analysis jobs: %w", err)
	}
	return nil
}
