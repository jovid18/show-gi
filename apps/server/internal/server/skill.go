package server

import (
	"context"
	"errors"
	"log"
	"sync"

	"github.com/jovid18/show-gi/apps/server/internal/skill"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 실력 추정치를 판 사이로 옮기는 두 자리. journal §47이 남긴 것이고, 붙는 조건이 둘이다 —
// 로그인했고 DB가 있어야 한다. 하나라도 없으면 지금까지처럼 판마다 초기화된다.

// priorSkill 은 지난 판까지의 추정치다. 없으면 skill.Unknown — 기준선 밴드로 시작한다.
//
// 못 읽어도 대국을 막지 않는다. 캐시·기록과 같은 판단이다(Options.Store).
func (h *gameHandler) priorSkill(ctx context.Context, userID *int64) skill.Estimate {
	if userID == nil || h.opts.Store == nil {
		return skill.Unknown
	}
	got, ok, err := h.opts.Store.SkillProfile(ctx, *userID)
	if err != nil {
		log.Printf("ws: skill profile %d: %v", *userID, err)
		return skill.Unknown
	}
	if !ok {
		return skill.Unknown
	}
	return skill.Estimate{Loss: got.Loss, Samples: got.Samples, AbsLoss: got.AbsLoss, AbsSamples: got.AbsSamples}
}

// saveSkill 은 판정마다 추정치를 덮는 콜백이다. 붙일 자리가 없으면 nil을 돌려준다 —
// 추정기가 그때 아무것도 안 부른다(skill.NewWorkerFrom).
//
// 추정기의 goroutine에서 불린다. 거기서 DB를 쓰는 것이 goroutine 소유 규약과 어긋나지
// 않는 근거는 skill.NewWorkerFrom 주석.
func (h *gameHandler) saveSkill(ctx context.Context, userID *int64) func(skill.Estimate) {
	if userID == nil || h.opts.Store == nil {
		return nil
	}
	id := *userID
	st := h.opts.Store
	return func(e skill.Estimate) {
		// 연결의 ctx 를 그대로 쓴다. 대국이 끝나면 그 뒤의 쓰기는 취소되는데, 그때는
		// 이미 마지막 판정까지 저장된 뒤다 — 매 수 쓰기 때문에 끝에 몰아 쓸 것이 없다.
		err := st.SaveSkillEstimate(ctx, id, store.SkillEstimate{Loss: e.Loss, Samples: e.Samples, AbsLoss: e.AbsLoss, AbsSamples: e.AbsSamples})
		switch {
		case err == nil:
		case errors.Is(err, context.Canceled):
			// 연결이 끊긴 것이다. 에러로 적지 않는다 — 위 이유로 잃은 것이 없는데
			// 「저장 실패」가 판마다 한 줄씩 쌓이면 진짜 실패를 그 안에서 못 찾는다.
		default:
			// 추정이 안 쌓이는 것은 다음 판의 첫 몇 수가 기준선이라는 뜻이고, 그 판은 그대로 된다.
			log.Printf("ws: save skill %d: %v", id, err)
		}
	}
}

// skillRun 은 한 판의 추정치를 처음과 끝만 붙잡아 둔다. 총평이 「이 판에서 어떻게
// 변했나」를 말하려면 두 값이 필요한데, 추정기는 마지막 값 하나만 갖고 그것도 자기
// goroutine 안이다.
//
// 저장(saveSkill)과 갈라 둔다. 익명 대국에는 저장할 자리가 없는데(002_anonymous_games.sql)
// 판 안에서의 변화는 익명에게도 있다.
type skillRun struct {
	// before 는 판이 시작할 때의 값이다. 만든 뒤로 안 바뀌므로 잠금 밖이다.
	before skill.Estimate

	// 추정기 goroutine이 쓰고 총평 goroutine이 읽는다. 그 둘은 세션과 별개로 도는
	// 서로 다른 goroutine이라, 여기가 이 파일에서 유일하게 잠금이 필요한 자리다.
	mu     sync.Mutex
	latest skill.Estimate
}

func newSkillRun(before skill.Estimate) *skillRun {
	return &skillRun{before: before, latest: before}
}

// observing 은 저장 콜백을 감싸 마지막 값을 붙잡는다. save 가 nil이어도(익명) 붙잡는
// 일은 그대로 한다 — 그래서 돌려주는 함수는 절대 nil이 아니다.
func (r *skillRun) observing(save func(skill.Estimate)) func(skill.Estimate) {
	return func(e skill.Estimate) {
		r.mu.Lock()
		r.latest = e
		r.mu.Unlock()
		if save != nil {
			save(e)
		}
	}
}

// change 는 총평에 실을 段級 변화다. 모르면 nil이다 — 표본이 모자라면 이름을 안 붙인다
// (skill.RankOf).
func (r *skillRun) change() *skillChange {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	after := r.latest
	r.mu.Unlock()

	now, ok := skill.RankOf(after)
	if !ok {
		return nil
	}
	c := &skillChange{After: rankView{Step: now.Step, Max: skill.RankMax, NameJa: now.NameJa}}
	// 처음 두는 사람에게는 「전」이 없다. 익명이거나 첫 판이면 기준선에서 시작하는데,
	// 그 값을 「이 판을 시작할 때의 실력」이라고 그리면 아무도 안 잰 숫자가 사람에 대한
	// 판정으로 화면에 선다.
	if was, ok := skill.RankOf(r.before); ok {
		c.Before = &rankView{Step: was.Step, Max: skill.RankMax, NameJa: was.NameJa}
	}
	return c
}
