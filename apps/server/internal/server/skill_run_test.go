package server

import (
	"sync"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/skill"
)

// 처음 두는 사람에게는 「전」이 없어야 한다. 기준선을 그리면 아무도 안 잰 숫자가 사람에
// 대한 판정으로 화면에 선다(06-status.md §62).
func TestFirstGameHasNoBefore(t *testing.T) {
	r := newSkillRun(skill.Unknown)
	r.observing(nil)(skill.Estimate{Loss: 0.2, Samples: skill.MinSamples})

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
	before := skill.Estimate{Loss: 0.5, Samples: 12}
	r := newSkillRun(before)
	r.observing(nil)(skill.Estimate{Loss: 0.1, Samples: 30})

	got := r.change()
	if got == nil || got.Before == nil {
		t.Fatalf("판 전후가 다 있어야 한다: %+v", got)
	}
	if got.Before.NameJa != "8級" {
		t.Errorf("전 = %q, want 8級", got.Before.NameJa)
	}
	// 낙폭이 줄었으므로 段級은 세져야 한다. 부호가 뒤집히면 화면 전체가 반대로 간다.
	if got.After.Step <= got.Before.Step {
		t.Errorf("낙폭이 줄었는데 段級이 안 세졌다: %d → %d", got.Before.Step, got.After.Step)
	}
}

// 표본이 모자라면 블록 자체가 없어야 한다.
func TestNoSkillBlockBeforeEnoughSamples(t *testing.T) {
	r := newSkillRun(skill.Unknown)
	r.observing(nil)(skill.Estimate{Loss: 0.9, Samples: skill.MinSamples - 1})
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

	cb(skill.Estimate{Loss: 0.4, Samples: 5})
	cb(skill.Estimate{Loss: 0.3, Samples: 6})

	if len(saved) != 2 {
		t.Fatalf("저장이 %d번 불렸다, want 2", len(saved))
	}
	// 마지막 값이 총평으로 간다.
	if got := r.change(); got == nil || got.After.Step != mustRank(t, 0.3).Step {
		t.Errorf("마지막 값이 안 붙잡혔다: %+v", got)
	}
}

// 추정기 goroutine이 쓰고 총평 goroutine이 읽는다 — `-race` 가 잡는 자리다.
func TestConcurrentObserveAndRead(t *testing.T) {
	r := newSkillRun(skill.Estimate{Loss: 0.5, Samples: 9})
	cb := r.observing(nil)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 200 {
			cb(skill.Estimate{Loss: float64(i%100) / 100, Samples: 10 + i})
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

func mustRank(t *testing.T, loss float64) skill.Rank {
	t.Helper()
	got, ok := skill.RankOf(skill.Estimate{Loss: loss, Samples: skill.MinSamples})
	if !ok {
		t.Fatalf("loss=%v 에 이름이 없다", loss)
	}
	return got
}
