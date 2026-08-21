package server

import (
	"os"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/match"
	"github.com/jovid18/show-gi/apps/server/internal/rating"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/skill"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 진짜 DB가 필요하다. 여기서 재는 것이 「같은 행의 다른 칸을 서로 안 덮는다」이고,
// 그 규칙은 SQL 의 ON CONFLICT 절에만 있다(query/rating.sql).
//
//	SHOWGI_TEST_DATABASE_URL=postgres://showgi:showgi@localhost:5432/showgi go test ./internal/server/
func ratingRecords(t *testing.T) (*matchRecords, int64, int64) {
	t.Helper()
	url := os.Getenv("SHOWGI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SHOWGI_TEST_DATABASE_URL 미설정 — DB 테스트 건너뜀")
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)

	// 실행마다 다른 사람이어야 한다. 남은 행을 물려받으면 두 번째 실행부터 판 수가 다르다.
	stamp := t.Name() + "-" + time.Now().Format("150405.000000000")
	ids := make([]int64, 0, 2)
	for _, seat := range []string{"b", "w"} {
		u, err := st.UpsertUser(t.Context(), "test", stamp+"-"+seat, "テスト")
		if err != nil {
			t.Fatalf("upsert %s: %v", seat, err)
		}
		ids = append(ids, u)
	}
	return newMatchRecords(st, intervene.Beginner), ids[0], ids[1]
}

// entry 는 판이 끝난 방 하나를 손으로 세운다. 기록기는 안 만든다 — 여기서 재는 것은
// 그 뒤의 레이팅 갱신이다(match_test.go 의 곁장부와 같은 방식).
func finishedEntry(black, white int64, blackResult, whiteResult match.Result) *roomRecord {
	return &roomRecord{
		at: time.Now(),
		player: map[shogi.Color]match.Player{
			shogi.Black: {UserID: black, Name: "b"},
			shogi.White: {UserID: white, Name: "w"},
		},
		result: map[shogi.Color]match.Result{
			shogi.Black: blackResult,
			shogi.White: whiteResult,
		},
	}
}

// 승부가 난 판은 두 사람을 반대로 옮기고 판 수를 하나씩 올린다.
func TestADecidedMatchMovesBothRatings(t *testing.T) {
	records, black, white := ratingRecords(t)

	records.updateRatings(t.Context(), finishedEntry(black, white, match.ResultWin, match.ResultLoss))

	won, err := records.store.MatchRating(t.Context(), black)
	if err != nil {
		t.Fatalf("MatchRating(black): %v", err)
	}
	lost, err := records.store.MatchRating(t.Context(), white)
	if err != nil {
		t.Fatalf("MatchRating(white): %v", err)
	}

	if won.Games != 1 || lost.Games != 1 {
		t.Fatalf("판 수가 %d / %d, want 1 / 1", won.Games, lost.Games)
	}
	if won.Value <= rating.Default {
		t.Errorf("이긴 쪽이 %.1f, want %d 위", won.Value, rating.Default)
	}
	if lost.Value >= rating.Default {
		t.Errorf("진 쪽이 %.1f, want %d 아래", lost.Value, rating.Default)
	}
	// 판을 뒀으므로 불확실성이 줄어야 한다. 그것이 「알게 됐다」의 표현이고,
	// 매칭 밴드가 이 값을 그대로 더한다.
	if won.Deviation >= rating.MaxDeviation {
		t.Errorf("불확실성이 %.1f, want %d 아래", won.Deviation, rating.MaxDeviation)
	}
}

// 승부가 안 난 판은 아무것도 안 옮긴다. 옮기면 탭을 닫는 것이 레이팅 수단이 된다.
func TestAnAbandonedMatchMovesNothing(t *testing.T) {
	records, black, white := ratingRecords(t)

	entry := finishedEntry(black, white, match.ResultAbandoned, match.ResultAbandoned)
	records.updateRatings(t.Context(), entry)

	for name, id := range map[string]int64{"black": black, "white": white} {
		got, err := records.store.MatchRating(t.Context(), id)
		if err != nil {
			t.Fatalf("MatchRating(%s): %v", name, err)
		}
		if got.Games != 0 {
			t.Errorf("%s: 판이 %d, want 0", name, got.Games)
		}
	}
}

// 한쪽이 아직 안 끝났으면 안 옮긴다. 반쪽으로 옮기면 상대의 결과가 나중에 와서
// 같은 판이 두 번 세어진다.
func TestAHalfFinishedMatchMovesNothing(t *testing.T) {
	records, black, white := ratingRecords(t)

	entry := finishedEntry(black, white, match.ResultWin, match.ResultLoss)
	delete(entry.result, shogi.White)
	records.updateRatings(t.Context(), entry)

	got, err := records.store.MatchRating(t.Context(), black)
	if err != nil {
		t.Fatalf("MatchRating: %v", err)
	}
	if got.Games != 0 {
		t.Errorf("판이 %d, want 0", got.Games)
	}
}

// 무승부는 같은 실력끼리라면 아무도 안 움직인다. 그래도 판 수는 는다 —
// 불확실성이 줄었기 때문이다.
func TestADrawStillCountsAsAGame(t *testing.T) {
	records, black, white := ratingRecords(t)

	records.updateRatings(t.Context(), finishedEntry(black, white, match.ResultDraw, match.ResultDraw))

	got, err := records.store.MatchRating(t.Context(), black)
	if err != nil {
		t.Fatalf("MatchRating: %v", err)
	}
	if got.Games != 1 {
		t.Errorf("판이 %d, want 1", got.Games)
	}
	if got.Value != rating.Default {
		t.Errorf("%.4f, want %d — 같은 실력끼리의 무승부는 안 움직인다", got.Value, rating.Default)
	}
}

// 사람과 한 판도 안 둔 사람은 엔진 대국의 추정치에서 시작한다. 유저가 적은 동안
// 첫 매칭이 무작위가 아닌 것과 같은 말이다.
func TestTheFirstRatingComesFromTheEngineEstimate(t *testing.T) {
	records, uid, _ := ratingRecords(t)

	const loss = 0.2 // 기준선(0.5)보다 잘 두는 사람
	err := records.store.SaveSkillEstimate(t.Context(), uid, store.SkillEstimate{Loss: loss, Samples: skill.MinSamples})
	if err != nil {
		t.Fatalf("SaveSkillEstimate: %v", err)
	}

	got := records.ratingOf(t.Context(), uid)
	if want := rating.SeedFromLoss(loss); got != want {
		t.Errorf("%+v, want %+v", got, want)
	}
	if got.Value <= rating.Default {
		t.Errorf("낙폭 %.1f 인 사람이 %.1f 에서 시작한다, want %d 위", loss, got.Value, rating.Default)
	}
}

// 표본이 모자라면 시드를 안 만든다. 하한은 skill 이 정한다 — 여기서 따로 정하면
// 이름만 다른 두 하한이 생긴다.
func TestTooFewSamplesStayUnrated(t *testing.T) {
	records, uid, _ := ratingRecords(t)

	err := records.store.SaveSkillEstimate(t.Context(), uid, store.SkillEstimate{Loss: 0.2, Samples: skill.MinSamples - 1})
	if err != nil {
		t.Fatalf("SaveSkillEstimate: %v", err)
	}

	if got := records.ratingOf(t.Context(), uid); got != rating.Unrated {
		t.Errorf("%+v, want %+v", got, rating.Unrated)
	}
}

// 대인전 한 판이 엔진 대국의 추정치를 안 지운다. 같은 행의 다른 칸이라, 덮으면
// 그 사람의 개입 임계치가 기준선으로 되돌아간다.
func TestRatingDoesNotClobberTheSkillEstimate(t *testing.T) {
	records, black, white := ratingRecords(t)

	want := store.SkillEstimate{Loss: 0.33, Samples: 11}
	if err := records.store.SaveSkillEstimate(t.Context(), black, want); err != nil {
		t.Fatalf("SaveSkillEstimate: %v", err)
	}

	records.updateRatings(t.Context(), finishedEntry(black, white, match.ResultWin, match.ResultLoss))

	got, ok, err := records.store.SkillProfile(t.Context(), black)
	if err != nil || !ok {
		t.Fatalf("SkillProfile: ok = %v, err = %v", ok, err)
	}
	if got != want {
		t.Errorf("%+v, want %+v", got, want)
	}
}
