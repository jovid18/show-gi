package store

import (
	"testing"
)

// 탐색 시간 한 행이 실제 스키마에 들어가고 depth·k 로 다시 묶인다. 가짜로는 확인할 수
// 없는 자리다 — 컬럼 이름과 타입이 SQL 에만 있다.
//
//	docker compose up -d db
//	SHOWGI_TEST_DATABASE_URL=postgres://showgi:showgi@localhost:5432/showgi go test ./internal/store/ -v
func TestSearchTimingsGroupByDepthAndK(t *testing.T) {
	s := open(t)
	k := "test/" + t.Name()
	if _, err := s.pool.Exec(t.Context(), `DELETE FROM search_timings WHERE sfen_key = $1`, k); err != nil {
		t.Fatalf("이전 실행 정리: %v", err)
	}

	rows := []SearchTiming{
		{SFENKey: k, Depth: 14, K: 1, Ms: 900, Cached: false},
		{SFENKey: k, Depth: 14, K: 3, Ms: 3300, Cached: false},
		{SFENKey: k, Depth: 14, K: 3, Ms: 4700, Cached: false},
		// 캐시 히트도 같은 표에 들어간다. 비용을 볼 때 빼고 보려면 갈라져 있어야 한다.
		{SFENKey: k, Depth: 14, K: 3, Ms: 20, Cached: true},
	}
	for _, r := range rows {
		if err := s.PutSearchTiming(t.Context(), r); err != nil {
			t.Fatalf("PutSearchTiming %+v: %v", r, err)
		}
	}

	var n int
	var avg int
	err := s.pool.QueryRow(t.Context(), `
		SELECT count(*), avg(ms)::int FROM search_timings
		WHERE sfen_key = $1 AND depth = 14 AND k = 3 AND NOT cached`, k).Scan(&n, &avg)
	if err != nil {
		t.Fatalf("집계: %v", err)
	}
	if n != 2 || avg != 4000 {
		t.Errorf("k=3 계산분 = %d행 평균 %dms, want 2행 4000ms", n, avg)
	}
}
