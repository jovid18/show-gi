package server

import (
	"sync"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/skill"
)

// 처음 두는 사람에게는 「전」이 없어야 한다. 기준선을 그리면 아무도 안 잰 숫자가 사람에
// 대한 판정으로 화면에 선다(journal §62).
func TestFirstGameHasNoBefore(t *testing.T) {
	r := newSkillRun(skill.Unknown)
	r.observing(nil)(estimate(0.05, skill.MinSamples))

	got := r.change()
	if got == nil {
		t.Fatal("표본이 찼는데 段級이 없다")
	}
	if got.Before != nil {
		t.Errorf("첫 판인데 「전」이 붙었다: %+v", got.Before)
	}
	if got.After.NameJa == "" || got.After.Max != skill.RankMax {
		t.Errorf("After = %+v", got.After)
	}
}

// 이어 두는 사람은 판 전후가 둘 다 있어야 한다.
func TestReturningPlayerGetsBothEnds(t *testing.T) {
	before := estimate(0.1265, 12) // 15級 앵커(skill.rankAnchors)
	r := newSkillRun(before)
	r.observing(nil)(estimate(0.02, 30))

	got := r.change()
	if got == nil || got.Before == nil {
		t.Fatalf("판 전후가 다 있어야 한다: %+v", got)
	}
	if got.Before.NameJa != "15級" {
		t.Errorf("전 = %q, want 15級", got.Before.NameJa)
	}
	// 낙폭이 줄었으므로 段級은 세져야 한다. 부호가 뒤집히면 화면 전체가 반대로 간다.
	if got.After.Step <= got.Before.Step {
		t.Errorf("낙폭이 줄었는데 段級이 안 세졌다: %d → %d", got.Before.Step, got.After.Step)
	}
}

// 표본이 모자라면 블록 자체가 없어야 한다.
func TestNoSkillBlockBeforeEnoughSamples(t *testing.T) {
	r := newSkillRun(skill.Unknown)
	r.observing(nil)(estimate(0.2, skill.MinSamples-1))
	if got := r.change(); got != nil {
		t.Errorf("표본이 모자란데 段級이 나갔다: %+v", got)
	}
}

// 추정기가 없는 판(nil)에서도 총평이 그대로 나가야 한다.
func TestNilRunIsQuiet(t *testing.T) {
	var r *skillRun
	if got := r.change(); got != nil {
		t.Errorf("추정기가 없는데 段級이 나갔다: %+v", got)
	}
}

// 감싼 콜백이 원래 저장을 그대로 부르고, 붙잡는 일은 저장이 없어도 한다.
func TestObservingStillSaves(t *testing.T) {
	r := newSkillRun(skill.Unknown)
	var saved []skill.Estimate
	cb := r.observing(func(e skill.Estimate) { saved = append(saved, e) })

	cb(estimate(0.1, 5))
	cb(estimate(0.08, 6))

	if len(saved) != 2 {
		t.Fatalf("저장이 %d번 불렸다, want 2", len(saved))
	}
	// 마지막 값이 총평으로 간다.
	if got := r.change(); got == nil || got.After.Step != mustRank(t, 0.08).Step {
		t.Errorf("마지막 값이 안 붙잡혔다: %+v", got)
	}
}

// 추정기 goroutine이 쓰고 총평 goroutine이 읽는다 — -race 가 잡는 자리다.
func TestConcurrentObserveAndRead(t *testing.T) {
	r := newSkillRun(skill.Estimate{Loss: 0.5, Samples: 9})
	cb := r.observing(nil)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 200 {
			cb(estimate(0.02+float64(i%100)/1000, 10+i))
		}
	}()
	go func() {
		defer wg.Done()
		for range 200 {
			_ = r.change()
		}
	}()
	wg.Wait()
}

func mustRank(t *testing.T, absLoss float64) skill.Rank {
	t.Helper()
	got, ok := skill.RankOf(estimate(absLoss, skill.MinSamples))
	if !ok {
		t.Fatalf("absLoss=%v 에 이름이 없다", absLoss)
	}
	return got
}

// estimate 는 段級이 붙을 만한 추정치다. 이름은 절대 낙폭에서만 나오므로(skill.RankOf)
// 두 축을 같은 개수로 채운다 — 대국 중에 오는 값이 그 모양이다.
//
// 밴드가 보는 Loss 는 아무 값이나 둔다. 段級이 그 칸을 안 보는 것이 이 파일이 확인하는
// 것 중 하나다.
func estimate(absLoss float64, samples int) skill.Estimate {
	return skill.Estimate{Loss: 0.5, Samples: samples, AbsLoss: absLoss, AbsSamples: samples}
}
