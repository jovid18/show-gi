package usi

import (
	"os"
	"testing"
)

// 이미 詰んでいる 국면에 엔진이 무엇을 찍는가. score mate 0 을 내보내지 않는다는 것이 답이고,
// eval.Mate 의 「0 은 만들지 않는 값이다」가 그 사실 위에 서 있다(journal §131).
//
// 부호가 없는 手数는 관점을 옮길 수 없다(-0 = 0). 엔진이 그 값을 내기 시작하면 여기가
// 먼저 빨개져야 한다 — 그때는 부르는 쪽에서 막지 말고 표현을 고쳐야 한다.
func TestRealEngineNeverScoresAMatedPositionAsMateZero(t *testing.T) {
	cmd := os.Getenv("SHOWGI_USI_CMD")
	if cmd == "" {
		t.Skip("SHOWGI_USI_CMD 미설정 — 실엔진 테스트 건너뜀")
	}
	e, err := New(cmd, nil)
	if err != nil {
		t.Fatalf("엔진 기동: %v", err)
	}
	t.Cleanup(e.Close)

	// 先手가 G*5b 로 詰ませた 뒤. 수번인 後手에게 합법수가 없다.
	const mated = "3lkl3/4G4/5S3/9/9/9/9/9/8K w - 2"
	res, err := e.SearchDepth(t.Context(), mated, nil, 14)
	if err != nil {
		t.Fatalf("탐색: %v", err)
	}
	n, ok := res.Score.MateIn()
	if !ok {
		t.Fatalf("詰み 점수가 아니다: %+v", res.Score)
	}
	if n == 0 {
		t.Fatalf("엔진이 score mate 0 을 냈다 — eval.Score 가 그 값을 못 든다")
	}
	if n > 0 {
		t.Errorf("詰まされた 쪽인데 mate %d 다 — 부호가 반대다", n)
	}
	t.Logf("best=%q score=mate %d", res.Best, n)
}
