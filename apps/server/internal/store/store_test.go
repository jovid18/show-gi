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
