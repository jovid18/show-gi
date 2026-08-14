package game

import (
	"github.com/jovid18/show-gi/apps/server/internal/explain"
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
	//
	// e 는 화면에 나간 설명이 **어느 계층에서 얼마에** 나왔는지다. 문장 자체는 안 남긴다 —
	// 그것은 `explain_cache` 에 키로 들어 있고, 여기 또 적으면 두 벌이 된다.
	// `Evaluated` 와 달리 나중에 오지 않는다: 판정이 끝난 뒤 카드가 뜨기 전에 이미 정해진다.
	Retracted(ply int, usi string, v intervene.Verdict, e explain.Result)

	// Undone 은 사람이 **스스로** 무른 수다. ply 는 그 사람 수의 手数.
	//
	// **Retracted 와 갈라 둔다.** 판이 되돌아간 것은 같지만 시작한 쪽이 반대다 —
	// 저쪽은 「개입에 오염되지 않은 실력 신호」로 정의돼 있어서(01-core.md §5), 사람이
	// 무르고 싶었던 수를 그 칸에 섞으면 그 정의가 그 자리에서 거짓이 된다.
	//
	// **기보를 자르는 것도 여기다.** 무른 수와 그 뒤 상대의 응수가 기보에서 사라져야
	// 「지금 판에 남아 있는 수순」이라는 game_moves 의 뜻이 유지된다(store.RecordUndo).
	Undone(ply int, usi string)

	// Named 는 사람이 그 판에서 **처음 짜낸** 囲い·전법·戦型의 코드다.
	//
	// **한 판에 코드마다 한 번이다.** 囲い는 판에서 매번 다시 세어지므로(styleTags) 같은
	// 이름이 수십 번 나오는데, 남길 값은 「짰다」 하나다.
	//
	// **手筋은 안 온다.** 이름의 정확도가 아직 프로덕션 수준이 아니고(06-status.md §45),
	// 「당신이 쓴 手筋」으로 세우면 그 오진이 사람의 기록으로 굳는다. 나머지 셋은 판과
	// 수순만으로 정해져 엔진 평가치에 안 걸린다.
	Named(code string)

	// Hinted 는 사람이 **불러서** 받은 최선수 힌트 한 번이다.
	//
	// **개입과 갈라 둔다.** 저쪽은 앱이 먼저 말을 건 자리이고 이쪽은 사람이 부른 자리라,
	// 섞으면 「개입 N회」가 사람이 스스로 물어본 횟수까지 세게 된다 — 待った를 갈라 둔 것과
	// 같은 이유다(010_game_hints.sql).
	//
	// key 는 그 국면의 `shogi.PositionKey` 다. **手数가 아니라 이것이 「같은 국면」의 자다** —
	// 되돌아온 자리가 手数로는 갈리지만 국면으로는 같아야 3회째가 막힌다.
	Hinted(ply int, key string, stage int, bestUSI string)

	// HintTaken 은 알려준 그 수를 사람이 실제로 뒀는가다. **답까지 본 국면에만 온다** —
	// 1단계는 駒만 짚으므로 「알려준 대로 뒀다」와 뜻이 같지 않다.
	//
	// 01-core.md §5의 `interventions.taken` 이 정의한 신호가 이것이다: 「알려줬는데도 못 찾은
	// 좋은 수」가 그대로 약점이 된다.
	HintTaken(key string, taken bool)

	// Finished 는 대국이 끝날 때 한 번. 끝나지 않고 연결이 끊기면 오지 않는다 —
	// 그 경우를 어떻게 남길지는 구현이 정한다.
	Finished(status Status, winner Side)
}
