package server

import (
	"context"
	"log"

	"github.com/jovid18/show-gi/apps/server/internal/skill"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 실력 추정치를 판 사이로 옮기는 두 자리. 06-status.md §47이 남긴 것이고, 붙는 조건이 둘이다 —
// **로그인했고 DB가 있어야 한다.** 하나라도 없으면 지금까지처럼 판마다 초기화된다.

// priorSkill 은 지난 판까지의 추정치다. 없으면 `skill.Unknown` — 기준선 밴드로 시작한다.
//
// **못 읽어도 대국을 막지 않는다.** 캐시·기록과 같은 판단이다(Options.Store).
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
	return skill.Estimate{Loss: got.Loss, Samples: got.Samples}
}

// saveSkill 은 판정마다 추정치를 덮는 콜백이다. 붙일 자리가 없으면 nil을 돌려준다 —
// 추정기가 그때 아무것도 안 부른다(skill.NewWorkerFrom).
//
// **추정기의 goroutine에서 불린다.** 거기서 DB를 쓰는 것이 goroutine 소유 규약과 어긋나지
// 않는 근거는 `skill.NewWorkerFrom` 주석.
func (h *gameHandler) saveSkill(ctx context.Context, userID *int64) func(skill.Estimate) {
	if userID == nil || h.opts.Store == nil {
		return nil
	}
	id := *userID
	st := h.opts.Store
	return func(e skill.Estimate) {
		// **연결의 ctx 를 그대로 쓴다.** 대국이 끝나면 그 뒤의 쓰기는 취소되는데, 그때는
		// 이미 마지막 판정까지 저장된 뒤다 — 매 수 쓰기 때문에 끝에 몰아 쓸 것이 없다.
		if err := st.SaveSkillEstimate(ctx, id, store.SkillEstimate{Loss: e.Loss, Samples: e.Samples}); err != nil {
			// 추정이 안 쌓이는 것은 다음 판의 첫 몇 수가 기준선이라는 뜻이고, 그 판은 그대로 된다.
			log.Printf("ws: save skill %d: %v", id, err)
		}
	}
}
