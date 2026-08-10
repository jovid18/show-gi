package game

import (
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// Recorder 는 대국을 남긴다. nil이면 기록하지 않고 대국은 그대로 된다.
//
// **모든 메서드는 즉시 돌아와야 한다.** 세션 goroutine에서 불리므로, 여기서 기다리면
// 그동안 착수도 투료도 스냅샷도 못 받는다. 구현은 큐에 넣고 자기 goroutine에서 쓴다.
//
// 기록이 실패해도 대국은 계속된다 — 개입 판정이 고장 나도 대국이 계속되는 것과 같은
// 판단이다. 대국이 본체이고 기록은 그 위에 얹힌다.
//
// `game` 은 DB를 모른다. 여기서 나가는 것은 전부 우리 타입이고, SQL로 옮기는 일은
// 구현 쪽이 한다 — `Analyst`·`Opponent` 와 같은 방식이다.
type Recorder interface {
	// Started 는 대국이 시작될 때 한 번.
	Started(startSFEN string, humanColor shogi.Color)

	// Moved 는 **확정된** 수만 받는다.
	//
	// 물러진 수는 여기 오지 않는다 — 기보에 남으면 롤백이 롤백이 아니게 된다.
	// 그쪽은 Retracted 로 간다.
	Moved(ply int, usi string, by Side)

	// Evaluated 는 그 手数의 국면 평가치를 **나중에** 채운다. **先手 관점 cp** 다.
	//
	// Moved 와 갈라져 있는 이유는 값을 아는 시점이 다르기 때문이다. 판정은 사람이 둔
	// **뒤에** 돌고, 그때 두 국면의 평가치가 한꺼번에 손에 들어온다 — 사람의 수 뒤와
	// 그 **직전 상대 수 뒤**다. 그래서 상대 수의 평가치는 한 수 늦게 채워진다.
	//
	// 관점을 先手로 고정하는 것은 `edges.eval_by_depth` 와 같은 규약이다. 「플레이어
	// 관점」으로 적으면 색이 다른 두 판을 나란히 못 놓는다.
	Evaluated(ply int, senteCp int)

	// Retracted 는 개입으로 물러진 수다.
	//
	// **개입에 오염되지 않은 유일한 실력 신호**다(01-core.md §5). 개입이 막지 않았다면
	// 실제로 뒀을 수이므로, 대국 결과보다 훨씬 밀도 높은 실력 추정 데이터가 된다.
	//
	// **같은 ply에 여러 번 올 수 있다.** 한 국면에서 몇 수를 시도하고 전부 물러지는 일이
	// 실제로 있고(06-status.md §17), 그 반복 자체가 기록할 값이다.
	Retracted(ply int, usi string, v intervene.Verdict)

	// Finished 는 대국이 끝날 때 한 번. 끝나지 않고 연결이 끊기면 오지 않는다 —
	// 그 경우를 어떻게 남길지는 구현이 정한다.
	Finished(status Status, winner Side)
}
