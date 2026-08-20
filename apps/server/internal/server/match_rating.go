package server

import (
	"context"
	"log"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/match"
	"github.com/jovid18/show-gi/apps/server/internal/rating"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/skill"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 대인전이 끝나면 두 사람의 레이팅을 옮기는 자리다. 근거는 journal §92.
//
// 이 파일이 세 패키지를 한자리에서 아는 유일한 곳이다 — rating 은 갱신식만, store 는
// 칸만, skill 은 표본 하한만 안다. 셋 중 누구도 나머지를 모른다.
//
// 값이 밖으로 안 나간다. 화면도 API도 이 숫자를 안 받고, 쓰는 쪽은 앞으로 붙는 매칭
// 하나다 — 보여주면 사람이 그것을 지키려 두기 시작한다.

// updateRatings 는 끝난 판 하나를 두 사람의 레이팅에 반영한다.
//
// 판을 막지 않는다. collect 의 goroutine 에서 불리므로 이미 대국 밖이고, 실패하면
// 그 판이 레이팅에 안 들어가는 것으로 끝난다 — 기록·캐시와 같은 판단이다(Options.Store).
func (m *matchRecords) updateRatings(ctx context.Context, entry *roomRecord) {
	if m == nil || m.store == nil {
		return
	}

	m.mu.Lock()
	black, blackOK := entry.player[shogi.Black]
	white, whiteOK := entry.player[shogi.White]
	blackResult, blackDone := entry.result[shogi.Black]
	_, whiteDone := entry.result[shogi.White]
	m.mu.Unlock()

	if !blackOK || !whiteOK || !blackDone || !whiteDone {
		return
	}

	// 승부가 안 난 판은 안 센다. 서버가 내려가 접힌 판과 한 수도 안 둔 채 시간이 다 된
	// 판이 그렇고(match.ResultAbandoned), 그것으로 레이팅을 옮기면 탭을 닫는 것이
	// 수단이 된다.
	//
	// 先手 관점 하나만 본다. 갱신식이 두 사람을 같이 내므로 나머지는 그것의 뒤집기다.
	outcome, ok := ratingOutcomeOf(blackResult)
	if !ok {
		return
	}

	// 두 사람이 같을 수 없다. 자기 방에 손님으로 못 앉는다(match.Hub.Enter).
	if black.UserID == white.UserID {
		log.Printf("match rating: both seats are user %d, skipping", black.UserID)
		return
	}

	before := m.ratingOf(ctx, black.UserID)
	oppBefore := m.ratingOf(ctx, white.UserID)

	after, oppAfter := rating.Update(before, oppBefore, outcome)

	err := m.store.SaveMatchRatings(ctx,
		black.UserID, storeRating(after),
		white.UserID, storeRating(oppAfter),
	)
	if err != nil {
		// 이 판이 레이팅에 안 들어간 것으로 끝난다. 다음 판이 갱신 전 값 위에서 돈다.
		log.Printf("match rating: %v", err)
	}
}

// ratingOf 는 그 사람의 지금 레이팅이다. 못 읽으면 rating.Unrated 다 — 판을 막지 않는다.
//
// 두 가지를 여기서 얹는다. 레이팅이 없으면 엔진 대국의 추정치로 시드를 만들고,
// 있으면 안 둔 시간만큼 불확실성을 되돌린다.
func (m *matchRecords) ratingOf(ctx context.Context, userID int64) rating.Rating {
	got, err := m.store.MatchRating(ctx, userID)
	if err != nil {
		log.Printf("match rating: read %d: %v", userID, err)
		return rating.Unrated
	}

	if got.Games == 0 {
		// 사람과 한 판도 안 뒀다. 엔진 대국의 추정치가 있으면 그것에서 시작한다 —
		// 유저가 적은 동안 첫 매칭이 무작위가 아닌 것과 같은 말이다.
		//
		// 표본 하한은 skill 이 정한다. 그쪽이 밴드를 옮기지 않는 표본으로 시드를
		// 만들면 이름만 다른 두 하한이 생긴다.
		est := skill.Estimate{Loss: got.Skill.Loss, Samples: got.Skill.Samples}
		if got.SkillKnown && est.Ready() {
			return rating.SeedFromLoss(est.Loss)
		}
		return rating.Unrated
	}

	return rating.Inflate(
		rating.Rating{Value: got.Value, Deviation: got.Deviation},
		time.Since(got.UpdatedAt),
	)
}

// ratingOutcomeOf 는 대인전의 결과를 갱신식의 어휘로 옮긴다. 두 번째 값이 false 면
// 승부가 안 난 판이다.
//
// 옮기는 자리가 여기 하나다 — rating 이 match 를 모르고 match 가 rating 을 모르는 것이
// 두 패키지를 상수만으로 흔들어 볼 수 있게 하는 조건이다.
//
// summary.go 의 outcomeOf 와 이름을 갈라 둔다. 저쪽은 총평의 어휘로 옮기는 것이고
// 받는 타입도 store.GameResult 라, 같은 이름이면 어느 척도로 가는지가 안 보인다.
func ratingOutcomeOf(r match.Result) (rating.Outcome, bool) {
	switch r {
	case match.ResultWin:
		return rating.Win, true
	case match.ResultLoss:
		return rating.Loss, true
	case match.ResultDraw:
		return rating.Draw, true
	}
	return 0, false
}

// storeRating 은 갱신된 값을 저장할 모양으로 옮긴다. Games 와 시각은 질의가 정하므로 안 채운다.
func storeRating(r rating.Rating) store.MatchRating {
	return store.MatchRating{Value: r.Value, Deviation: r.Deviation}
}
