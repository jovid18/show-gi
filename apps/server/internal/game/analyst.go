package game

import (
	"context"
	"fmt"
	"log"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// MateSearcher 는 詰み 탐색이다. **탐색부와 다른 바이너리**라 따로 받는다
// (docs/02-architecture.md §3). nil이면 종반 판정 없이 승률 낙폭만 본다.
type MateSearcher interface {
	SearchMate(ctx context.Context, startSFEN string, moves []string) (usi.MateResult, error)
}

type engineAnalyst struct {
	search Searcher
	mate   MateSearcher
	depth  int
	level  intervene.Level
}

// JudgeDepth 는 개입 판정에 쓰는 탐색 깊이다.
//
// 실측(06-status.md)에서 `depth 10 × k=1`이 최장 144ms, 12가 400ms다. 판정은 정밀도보다
// 속도가 중요하고 **착수 직후에 도는 유일한 탐색**이라 여기를 짧게 잡는다.
const JudgeDepth = 12

// ShallowDepth 는 **초보자의 시야를 모사하는** 깊이다.
//
// 「얕은 이득에 낚임」은 여기서 좋아 보이는데 JudgeDepth 에서 나쁜 수다(01-core.md §3).
// 2인 이유는 捨て駒가 얕게 보면 반드시 손해로 보이기 때문이다 — 그게 捨て駒의 정의이고,
// 그 거울상이 「한 수만 보면 이득」이다.
const ShallowDepth = 2

// NewEngineAnalyst 는 엔진으로 판정하는 Analyst 를 만든다.
//
// mate 가 nil이면 종반 판정을 건너뛴다 — 승률 낙폭은 그대로 돌므로 대국은 된다.
func NewEngineAnalyst(s Searcher, mate MateSearcher, level intervene.Level) Analyst {
	return &engineAnalyst{search: s, mate: mate, depth: JudgeDepth, level: level}
}

func (a *engineAnalyst) Judge(ctx context.Context, startSFEN string, moves []string, _ int) (Judgement, error) {
	if len(moves) == 0 {
		return Judgement{}, fmt.Errorf("judge: no move to judge")
	}
	before := moves[:len(moves)-1]

	// 착수 **전** 국면의 최선수. 두는 쪽(=사람) 관점이다.
	best, err := a.search.SearchDepth(ctx, startSFEN, before, a.depth)
	if err != nil {
		return Judgement{}, fmt.Errorf("judge: search before: %w", err)
	}

	// 착수 **후** 국면. 엔진은 늘 수번 측 관점으로 답하므로 지금은 상대 관점이다.
	after, err := a.search.SearchDepth(ctx, startSFEN, moves, a.depth)
	if err != nil {
		return Judgement{}, fmt.Errorf("judge: search after: %w", err)
	}

	in := intervene.Input{
		BestCp:  best.ScoreCp,
		AfterCp: -after.ScoreCp, // 사람 관점으로 뒤집는다
		Level:   a.level,
	}

	// 카테고리에 쓸 국면 사실. **판정 자체는 여기에 매이지 않는다** — 못 읽으면
	// Known 이 false로 남고 카테고리만 other 가 된다. 개입은 그대로 걸린다.
	if pos, m, err := replay(startSFEN, moves); err == nil {
		in.Features = MoveFeatures(pos, m)
		// 얕은 평가는 **이미 받아 둔 info 라인**에 있다. PvInterval=0 덕에 depth 12
		// 탐색 한 번이 depth 1~12를 전부 돌려주므로 추가 탐색이 없다(01-core.md §4).
		if cp, ok := after.ScoreAtDepth(ShallowDepth); ok {
			in.Features.ShallowCp, in.Features.HasShallow = -cp, true // 사람 관점
		}
	} else {
		// 판을 못 읽은 것은 우리 버그다. 판정은 계속하되 조용히 넘기지 않는다.
		log.Printf("game: could not replay for features, category will be other: %v", err)
	}

	if a.mate != nil {
		// 착수 **전**에 내가 詰み을 가지고 있었는가. solver 는 수번 측(=사람)의 詰み을
		// 모든 응수에 대해 증명하므로 이 질문에 정확히 답한다.
		if r, err := a.mate.SearchMate(ctx, startSFEN, before); err == nil && r.Found() {
			in.MateBefore = len(r.Moves)
		}
	}

	// 착수 **후**에도 남았는가는 solver 로 물으면 안 된다 — 그 국면의 수번은 상대라
	// `go mate` 가 「상대의 詰み」을 답한다. 내가 알아야 하는 것과 반대다.
	//
	// 대신 이미 구해둔 탐색 결과를 쓴다. 착수 후 국면이 **수번 측에게 불리한 mate**로
	// 나오면(MateIn < 0) 그것이 곧 「상대가 詰まされる」 = 내 詰み이 남았다는 뜻이다.
	if in.MateBefore > 0 && after.IsMate && after.MateIn < 0 {
		in.MateAfter = -after.MateIn
	}

	v := intervene.Judge(in)
	j := Judgement{Verdict: v}
	if v.Kind != intervene.KindNone {
		// 이미 손에 든 탐색의 PV가 그대로 「상대는 이렇게 벌한다」다. **추가 탐색이 없고
		// 분류도 필요 없다** — 카테고리가 이유를 못 대는 3분의 2(06-status.md §17)가
		// 여기서 설명을 갖는다.
		j.Refutation = refutationLine(startSFEN, moves, after.PV, RefutationPlies)
	}
	return j, nil
}

// RefutationPlies 는 반박 수순의 **상한**이다. 실제 길이는 국면이 정한다(trimRefutation).
//
// 깊이 12 탐색의 PV는 뒤로 갈수록 확실하지 않고, 화면에서는 「왜 나쁜가」가 아니라
// 강의가 된다. 여기는 그 두 가지를 막는 한도이고, 보통은 이보다 훨씬 앞에서 잘린다.
const RefutationPlies = 8

// refutationLine 은 착수 후 탐색의 PV를 棋譜 표기가 붙은 수순으로 옮긴다.
//
// **엔진 출력을 믿지 않는다.** PV의 각 수를 룰 엔진으로 검증하고, 못 두는 수가 나오면
// 거기서 끊어 그때까지의 수순만 돌려준다 — 대국 루프가 상대 수를 검증하는 것과 같은
// 이유다. 화면에 나가는 단언이라 틀린 것을 그리느니 짧게 그린다.
//
// 표기를 여기서 만드는 이유도 같다. 화면이 USI에서 다시 만들면 표기가 두 벌이 되고,
// 어긋났을 때 어느 쪽이 맞는지 알 수 없다(06-status.md §6 ④).
func refutationLine(startSFEN string, moves []string, pv []string, limit int) []Move {
	if len(pv) == 0 || limit <= 0 {
		return nil
	}

	pos, err := positionAfter(startSFEN, moves)
	if err != nil {
		log.Printf("game: could not replay for refutation: %v", err)
		return nil
	}
	last, err := shogi.ParseUSIMove(moves[len(moves)-1])
	if err != nil {
		return nil
	}
	// 물러진 수의 도착 칸. 벌하는 수는 대개 그 자리를 되따는 수라 「同」이 여기서 나온다.
	prevTo := int(last.To)

	// 판정하는 수는 늘 사람의 수이므로, 그 다음은 어느 색을 잡았든 상대다.
	by := SideEngine

	line := make([]Move, 0, limit)
	var steps []refutationStep // 자르는 자리를 여기서 찾는다
	for _, u := range pv {
		if len(line) == limit {
			break
		}
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			break
		}
		if err := pos.ValidateMove(m); err != nil {
			break
		}
		step := refutationStep{captureSq: -1}
		if !m.IsDrop() && !pos.Board[m.To].Empty() {
			step.captureSq = int(m.To)
		}
		line = append(line, Move{USI: u, Ja: pos.MoveJa(m, prevTo), By: by})

		pos = pos.Apply(m)
		prevTo = int(m.To)
		step.settles = step.captureSq >= 0 || pos.InCheck(pos.Turn)
		steps = append(steps, step)
		if by == SideEngine {
			by = SideHuman
		} else {
			by = SideEngine
		}
	}
	if len(line) == 0 {
		return nil
	}
	return line[:trimRefutation(steps)]
}

// refutationStep 은 반박 수순의 한 수에서 **자를 자리를 정하는 데 필요한 사실**이다.
type refutationStep struct {
	// settles 는 그 수에서 손익이 바뀌는가 — 駒를 따거나 王手를 건다.
	settles bool
	// captureSq 는 딴 칸. 안 땄으면 -1. **교환은 한 칸에서 벌어지는 것**이라 이어지는지를
	// 이 값이 정한다.
	captureSq int
}

// trimRefutation 은 **처음 벌어지는 교환이 끝나는 자리**에서 수순을 끊는다.
//
// 길이를 상수로 박으면 국면마다 틀린다. 角을 던지면 되따는 한 수로 이유가 끝나는데
// 거기에 세 수를 더 붙이면 뒤는 정보가 아니라 잡음이고, 반대로 몇 수 앞에서 벌어지는
// 국면(06-status.md §17)에서는 그 상수가 이유가 나오기 전에 끊는다.
//
// 손익이 바뀌는 수(駒를 따거나 王手를 거는 수)를 처음 만날 때까지가 준비 수순이다 —
// 그 전에는 판 위에서 아직 아무 일도 안 일어난다. 거기서 교환이 시작되고, **같은 칸에서
// 주고받는 동안**이 그 교환이다.
//
// **교환을 중간에 끊으면 거짓말이 된다.** `△7六飛成` 만 보여주면 飛가 馬를 그냥 딴 것으로
// 읽히는데 실제로는 `▲同角` 으로 되딴다. 반쪽이 틀린 것보다 한 수 긴 편이 낫다.
//
// **「같은 칸」이 조건인 것이 요점이다.** 그냥 「따는 수가 이어지는 동안」으로 두면 다른
// 자리에서 벌어지는 별개의 교환까지 붙어서, 마지막 따는 수까지 가는 것과 같아진다 —
// 실측에서 두 규칙이 8수짜리 같은 줄을 냈다(06-status.md §20).
//
// 판단에 엔진이 필요 없다. 룰 엔진으로 재생하면서 공짜로 알 수 있고, 그래서 개입
// 판정에 비용이 붙지 않는다.
//
// 손익이 바뀌는 수가 하나도 없으면 벌하는 첫 수만 남긴다. 조용한 수를 늘어놓아 봐야
// 「왜 나쁜가」가 거기 없기는 마찬가지이고, **모르는 것을 길이로 메우지 않는다.**
func trimRefutation(steps []refutationStep) int {
	for i, s := range steps {
		if !s.settles {
			continue
		}
		// 王手는 교환이 아니다. captureSq 가 -1이라 아래 조건이 바로 거짓이 된다.
		end := i + 1
		for end < len(steps) && steps[end].captureSq >= 0 && steps[end].captureSq == steps[end-1].captureSq {
			end++
		}
		return end
	}
	return 1
}
