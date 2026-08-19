package quiz

import (
	"context"
	"log"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// mateSolver 는 트리 하나를 짓는 동안의 상태다 — memo와 예산을 들고 있다.
//
// memo 의 키는 RepetitionKey(手数를 뗀 SFEN)다. 詰みまでの手数는 국면의 성질이라
// 거기까지 온 순서와 무관하고, 그래서 전치가 같은 행으로 합쳐져도 값이 맞는다.
type mateSolver struct {
	mate MateSearcher
	memo map[string]int
	// unknown 은 solver 가 결론을 못 낸 국면이다. memo 와 갈라 둔다 — 저쪽의 0은
	// 「詰み이 없다」는 결론이고 이쪽은 결론이 아니다.
	unknown map[string]struct{}
	budget  int
	// answered 는 solver 가 결론을 준 횟수다. 0이면 엔진이 통째로 답하지 않았다는 뜻이고,
	// 그 하나가 「이 판에 문항이 없다」와 「못 봤다」를 가른다(Build).
	answered int
}

func newMateSolver(m MateSearcher) *mateSolver {
	return &mateSolver{
		mate:    m,
		memo:    make(map[string]int),
		unknown: make(map[string]struct{}),
		budget:  MateSearchBudget,
	}
}

// distance 는 그 국면에서 수번 측이 詰ます 手数를 준다. 0이면 詰み이 없다.
//
// ok=false 는 「모른다」다 — 예산이 끝났거나, solver 가 한계 안에서 결론을 못 냈거나
// (checkmate timeout), 엔진이 실패했다. 「모른다」를 「없다」로 쓰면 있는 詰み을 놓치고,
// 그 위에서 채점하면 정답을 오답이라고 말한다(01-core.md §2).
func (s *mateSolver) distance(ctx context.Context, pos shogi.Position) (int, bool) {
	key := pos.RepetitionKey()
	if d, ok := s.memo[key]; ok {
		return d, true
	}
	if _, unknown := s.unknown[key]; unknown {
		return 0, false
	}
	if s.budget <= 0 {
		return 0, false
	}
	s.budget--

	// 국면을 SFEN으로 넘긴다. 手数 경로로 부르면 트리가 깊어질수록 경로가 길어지고,
	// 여기서 쓰는 키와도 어긋난다.
	r, err := s.mate.SearchMate(ctx, pos.SFEN(), nil)
	if err != nil || !r.Proven {
		// 「모른다」도 기억한다. solver 는 같은 DepthLimit 에서 결정적이라 두 번째도
		// 같은 답이고, 그 답은 가장 오래 걸리는 종류다(한계까지 다 찾아본 뒤의 timeout).
		// 전치로 같은 국면에 다시 오는 것이 詰み 트리에서는 흔해서, 안 적어 두면 그때마다
		// 예산이 한 칸씩 빠진다.
		s.unknown[key] = struct{}{}
		return 0, false
	}
	d := len(r.Moves)
	s.memo[key] = d
	s.answered++
	return d, true
}

// buildTree 는 뿌리에서 자라는 노드 전부를 짓는다. ok=false 면 문항을 버린다 —
// 반쪽 트리는 완전하지 않고, 완전하지 않은 트리로 채점하면 정답을 오답이라고 말한다.
func (s *mateSolver) buildTree(ctx context.Context, root shogi.Position, plies int) (map[string]MateNode, bool) {
	nodes := make(map[string]MateNode)
	if !s.expand(ctx, nodes, root, plies) {
		return nil, false
	}
	return nodes, true
}

// expand 는 사람이 둘 차례인 국면 하나를 채우고, 정답 수마다 다음 노드로 내려간다.
func (s *mateSolver) expand(ctx context.Context, nodes map[string]MateNode, pos shogi.Position, plies int) bool {
	key := pos.RepetitionKey()
	if _, done := nodes[key]; done {
		return true
	}
	// 자리를 먼저 잡아 둔다. 전치로 같은 키에 다시 오는 것을 여기서 끊는다 —
	// 手数가 줄어드는 것이 재귀의 종료 조건이지만, 그것만으로는 같은 국면을 여러 갈래에서
	// 다시 펼치는 것을 막지 못한다.
	nodes[key] = MateNode{Plies: plies}

	node := MateNode{Plies: plies, Moves: make(map[string]MateVerdict)}
	type branch struct {
		pos   shogi.Position
		plies int
	}
	var next []branch

	for _, m := range checkingMoves(pos) {
		np := pos.Apply(m)
		usiMove := m.USI()

		if np.IsCheckmate() {
			node.Moves[usiMove] = MateVerdict{Mated: true, Correct: true}
			continue
		}

		// 1手 노드에서는 응수를 물어보지 않는다.
		//
		// 정답의 조건이 2+rest <= plies 이고 rest >= 1 이므로 plies == 1 에서는
		// 2+rest >= 3 > 1 — 詰み이 아닌 王手는 절대 정답이 될 수 없다. 그런데 그 노드가
		// 트리에서 가장 많고(정답 하나마다 하나씩 달린다) 노드마다 王手 × 응수만큼 solver 를
		// 부르므로, 여기가 예산의 대부분을 쓰고 있었다. 물어봐도 답이 안 바뀌는 자리다.
		if plies == 1 {
			node.Moves[usiMove] = MateVerdict{}
			continue
		}

		rest, defense, after, ok := s.defend(ctx, np)
		if !ok {
			return false
		}

		v := MateVerdict{Rest: rest}
		// rest=0 은 어떤 응수로 詰み이 사라진다는 뜻이라 그 자리에서 오답이다.
		if rest > 0 && 2+rest <= plies {
			v.Correct = true
			v.Defense = defense
			next = append(next, branch{pos: after, plies: rest})

			// 2+rest < plies 는 뿌리 手数가 최소가 아니었다는 뜻이고 solver 가 최소를
			// 준다는 전제와 어긋난다. 일어나면 그 수는 더 빠른 詰み이므로 정답으로 두고
			// (맞은 것을 틀렸다고 말하지 않는다) 여기에 소리를 남긴다.
			if 2+rest < plies {
				log.Printf("quiz: mate tree: %s cuts %d-ply mate to %d — the root distance was not minimal", usiMove, plies, 2+rest)
			}
		}
		node.Moves[usiMove] = v
	}

	node.Best = pickBestMate(node.Moves)

	// 정답이 하나도 없는 노드는 트리가 깨진 것이다. 그 국면에는 증명된 詰み이 있으므로
	// 어떤 王手 하나는 手数를 줄여야 하고, 그런 수가 없다는 것은 solver 가 최소 手数를
	// 준다는 전제가 깨졌다는 뜻이다(go mate 는 df-pn이라 최소를 보장하지 않는다 — §53).
	//
	// 그대로 내보내면 사람이 무엇을 둬도 「この手では…」만 받고, Best 가 비어 있으니
	// 세 번째 오답의 「무엇을 움직이나」까지 빈다(§61) — 답이 없는 문제를 내는 셈이고
	// 생성이 판당 한 번뿐이라 영구히 남는다. 다른 불완전한 트리와 같이 버린다.
	if node.Best == "" {
		log.Printf("quiz: mate tree: no correct move at a %d-ply node (%s) — dropping the item", plies, key)
		return false
	}

	nodes[key] = node

	for _, b := range next {
		if !s.expand(ctx, nodes, b.pos, b.plies) {
			return false
		}
	}
	return true
}

// defend 는 玉方의 최장 방어를 고른다.
//
// rest 는 그 방어 뒤에 남는 詰みまでの手数다. 0이면 詰み이 사라지는 응수가 있다 —
// 하나라도 있으면 그 王手는 반박되므로 나머지를 더 물어볼 이유가 없고, 그 조기 종료가
// 오답인 수의 비용을 응수 하나로 줄인다.
func (s *mateSolver) defend(
	ctx context.Context,
	after shogi.Position,
) (rest int, defense string, next shogi.Position, ok bool) {
	best := -1
	for _, r := range after.LegalMoves() {
		rp := after.Apply(r)
		d, known := s.distance(ctx, rp)
		if !known {
			return 0, "", shogi.Position{}, false
		}
		if d == 0 {
			return 0, "", shogi.Position{}, true
		}
		// 동률이면 USI 순서로 정한다. 매번 다르게 응수하면 같은 문제가 열 때마다
		// 다르게 흘러가고, 사람은 그것을 고장으로 읽는다. 手数가 같으니 문제의 성질은
		// 안 변한다.
		u := r.USI()
		if d > best || (d == best && u < defense) {
			best, defense, next = d, u, rp
		}
	}
	if best < 0 {
		// 王手를 받고 있는데 응수가 없으면 詰み이고, 그건 부르는 쪽이 이미 걸렀다.
		// 여기 오는 것은 룰 엔진과 이 파일이 어긋난 것이다.
		log.Printf("quiz: mate tree: a checked position has no legal reply but is not mate: %s", after.SFEN())
		return 0, "", shogi.Position{}, false
	}
	return best, defense, next, true
}

// pickBestMate 는 오답에 보여줄 정답 수를 결정적으로 고른다 — 詰み이 먼저, 그다음 USI 순서다.
//
// map 순회는 순서가 없다. 그냥 첫 정답을 잡으면 같은 트리가 열 때마다 다른 수를
// 「최선수」로 말한다.
func pickBestMate(moves map[string]MateVerdict) string {
	best, bestMated := "", false
	for u, v := range moves {
		if !v.Correct {
			continue
		}
		if best == "" || preferMate(u, v.Mated, best, bestMated) {
			best, bestMated = u, v.Mated
		}
	}
	return best
}

func preferMate(usiMove string, mated bool, cur string, curMated bool) bool {
	if mated != curMated {
		return mated
	}
	return usiMove < cur
}

// mateItem 은 詰み 문항을 고른다.
//
// 사람 차례 국면을 手数 순으로 훑어 처음으로 MateMaxPlies 안에 들어온 것이 문제다.
// 뒤에 더 짧은 詰み이 있어도 그쪽을 안 고른다 — 최초가 판에서 詰み이 처음 성립한 자리이고,
// 늦은 국면일수록 승부가 이미 갈려 배울 것이 적다(§53).
// 두 번째 값은 solver 가 결론을 준 횟수다. 0이면 엔진이 통째로 답하지 않았다는 뜻이고,
// 부르는 쪽이 그것으로 「문항이 없다」와 「못 봤다」를 가른다(Build).
func (b *Builder) mateItem(ctx context.Context, in Input, posAt []shogi.Position) (*MateItem, int) {
	sol := newMateSolver(b.mate)

	// 「詰み이 없었다」와 「solver 가 답을 못 했다」를 갈라 센다. 둘을 뭉쳐 로그에 「문항
	// 0개」로만 남기면, 엔진이 통째로 답하지 않는 배포에서도 그림이 똑같아서 기능이
	// 조용히 사라진 것을 알 수 없다.
	scanned, unanswered := 0, 0
	defer func() {
		if scanned == 0 {
			return // 사람 차례 국면이 아예 없었다. 훑을 것이 없던 것이지 실패가 아니다
		}
		if unanswered > 0 {
			log.Printf("quiz: the mate solver did not answer for %d of %d human-turn positions", unanswered, scanned)
		}
	}()

	// 정석 구간도 훑는다. OpeningPlies 는 「최선수는?」 문항의 것이고(둘 만한 수가
	// 여럿인 자리를 문항으로 안 내려는 값이다) 詰み에는 그 이유가 없다 — 棒銀·早石田의
	// 함정으로 20手 안에 끝나는 판이 이 기능이 겨냥하는 초심자 그 자체다.
	for i := 0; i < len(posAt); i++ {
		pos := posAt[i]
		if pos.Turn != in.Human {
			continue
		}
		scanned++

		d, known := sol.distance(ctx, pos)
		if !known {
			unanswered++
			// 예산이 끝났거나 ctx가 죽었으면 뒤도 마찬가지다. 한 국면을 못 잰 것이면
			// 그 국면만 건너뛴다 — 「모른다」가 「없다」는 아니지만 훑기는 이어진다.
			if sol.budget <= 0 || ctx.Err() != nil {
				return nil, sol.answered
			}
			continue
		}
		if d == 0 || d > MateMaxPlies {
			continue
		}

		converted := b.converted(in, posAt, i, d)
		if converted && d < MateMinPliesIfConverted {
			continue // 이미 決めた 1手詰め을 다시 내는 것은 문항이 아니다
		}

		nodes, ok := sol.buildTree(ctx, pos, d)
		if !ok {
			// 버린 이유를 갈라 적는다. 예산이 남았는데 버렸다면 어딘가에서 solver 가
			// 결론을 못 냈다는 뜻이고, 그것은 DepthLimit(기본 11) 밖으로 늘어난 갈래일 수
			// 있다 — 7手 뿌리에서 한 手 낭비하면 13手가 된다. 뭉쳐 적으면 프로덕션에서
			// 詰み 문항이 늘 사라지는 이유를 첫 판에서 못 읽는다(§53).
			why := "the solver did not conclude somewhere in the tree"
			if sol.budget <= 0 {
				why = "the search budget ran out"
			}
			log.Printf("quiz: dropped the mate item at ply %d (%d-ply mate): %s", i, d, why)
			return nil, sol.answered
		}
		return &MateItem{Ply: i, SFEN: pos.SFEN(), Plies: d, Converted: converted, Nodes: nodes}, sol.answered
	}
	return nil, sol.answered
}

// converted 는 사람이 그 詰み을 대국에서 실제로 決めた가.
//
// 수를 견주지 않고 手数로 센다. 「엔진이 준 수순의 첫 수와 같은가」로 보면 余詰(같은
// 手数의 다른 詰み筋)을 놓친 것으로 세는데 실전 국면에서는 흔하다. 어느 筋으로 詰ましても
// 판은 그 手数 안에 끝나므로, 그 뒤로 더 길게 이어졌다가 곧 놓친 것이다.
//
// 이긴 것만으로는 모자라다. 엔진은 質 때문에 投了하기도 하고, 못 두는 수를 내놓아도
// 사람의 승리로 닫힌다(game/session.go 의 applyEngineMove).
//
// 그때 手数만 보면 「あなたが決めた詰みです」가 두어진 적 없는 詰み을 두고 나간다.
// 그래서 마지막 국면이 실제로 詰み인지를 함께 본다 — 사람이 詰ましたら 그 국면의
// 수번은 상대이고 벗어날 수가 없다.
func (b *Builder) converted(in Input, posAt []shogi.Position, i, plies int) bool {
	// 手数도 마지막 국면도 같은 재현에서 센다. replay 는 읽을 수 없는 수에서 멈추므로
	// (build.go) 기보에 그런 수가 있으면 posAt 이 판의 실제 끝보다 앞에서 끊긴다. 한쪽을
	// len(in.Moves) 로 세면 手数는 넘치고 마지막 국면은 詰み이 아니어서, 사람이 실제로
	// 決めた 詰み이 「놓쳤다」로 나간다 — 이 기능이 피하려는 거짓 그 자체다.
	// 手数가 정확히 맞아야 한다. 짧으면 상대가 잘못 받아 빨리 끝난 것이고, 그때 사람이
	// 둔 것은 이 문항의 수순이 아니다.
	//
	// 길 수는 없다 — solver 가 無駄合い까지 세므로 제대로 決めた 詰み은 plies 를 안 넘는다.
	// 그래서 이 조건이 좁아지는 방향은 「놓쳤다」쪽이고, 그쪽은 참이다.
	if rest := len(posAt) - 1 - i; !in.Won || rest != plies {
		return false
	}
	return posAt[len(posAt)-1].IsCheckmate()
}
