package skill

import "context"

// queueSize 는 아직 추정에 안 들어간 착수를 몇 건까지 물고 있을지다.
//
// 넘치면 **버린다.** 기록과 같은 규약이고(game.Recorder) 이유도 같다 — 세션 goroutine이
// 추정기를 기다리면 그동안 착수도 투료도 스냅샷도 못 받는다.
const queueSize = 16

// Worker 는 추정을 세션 밖에서 돌린다. **입력은 논블로킹, 출력은 채널이다.**
//
// 롤링 통계라면 인라인으로도 되는데 갈라 두는 것은 추정기를 갈아끼울 자리를 남기기
// 위해서다 — 모델(수백 ms)이나 LLM(초)로 올라가도 생산자·소비자 배선이 그대로다
// (06-status.md §21 ①). 인라인으로 만들면 그때 세션 상태머신을 다시 건드리게 된다.
type Worker struct {
	in  chan Move
	out chan Estimate
}

// NewWorker 는 goroutine 하나를 띄운다. ctx 가 끝나면 그것도 끝난다.
func NewWorker(ctx context.Context) *Worker {
	w := &Worker{
		in: make(chan Move, queueSize),
		// 버퍼 1 + 최신 값만 남긴다(push). 밴드는 「지금 얼마나 헤매는가」로 정하는 것이라
		// 낡은 추정치를 순서대로 읽게 하면 한 수 늦은 값으로 상대를 고르게 된다.
		out: make(chan Estimate, 1),
	}
	go w.run(ctx)
	return w
}

func (w *Worker) run(ctx context.Context) {
	t := NewTrack()
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-w.in:
			w.push(t.Observe(m))
		}
	}
}

// Observe 는 착수 한 건을 넘긴다. **즉시 돌아온다.** 큐가 차 있으면 버린다.
func (w *Worker) Observe(m Move) {
	select {
	case w.in <- m:
	default:
	}
}

// Estimates 는 추정치가 오는 채널이다. **읽는 쪽은 세션이 소유한다** — worker 가 공유
// 변수를 직접 쓰면 「대국 상태는 goroutine 하나가 소유한다」가 깨진다(06-status.md §21 ①).
func (w *Worker) Estimates() <-chan Estimate { return w.out }

// push 는 최신 추정치만 남긴다.
//
// 버퍼에 값을 넣는 것은 이 goroutine 하나뿐이고 세션은 꺼내 갈 뿐이라, 한 번 비우면
// 그다음 넣기는 반드시 성공한다.
func (w *Worker) push(e Estimate) {
	select {
	case w.out <- e:
		return
	default:
	}
	select {
	case <-w.out:
	default:
	}
	select {
	case w.out <- e:
	default:
	}
}
