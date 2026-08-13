package quiz

import (
	"context"
	"log"
	"sort"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// candidate 는 gap을 재 볼 후보 하나다. drop 은 사람 관점 낙폭(cp)이다.
type candidate struct {
	index int // posAt 의 자리 = 그 국면까지 둔 手数
	drop  int
}

// bestItems 는 「この局面の最善手は?」 문항을 고른다.
//
// **두 걸음이다.** 낙폭으로 후보를 좁히고(기록만 보므로 엔진 0회), 그 후보에만 MultiPV를
// 걸어 1위−2위 차를 잰다. 사람 수 전부를 재면 48초인데 이러면 12초다(§53).
//
// 두 기준이 서로 다른 것을 걸러 낸다 — **낙폭은 「사람이 여기서 틀렸다」이고, gap은 「정답이
// 하나뿐이다」다.** 낙폭만 쓰면 정답이 여럿인 국면이 문항이 되어 좋은 수를 둔 사람이
// 「不正解」를 받고, gap만 쓰면 사람이 이미 맞게 둔 국면이 뽑힌다.
// 두 번째 값은 엔진이 **답한 후보의 수**다(mateItem 과 같은 규약).
func (b *Builder) bestItems(
	ctx context.Context, in Input, posAt []shogi.Position, mate *MateItem,
) ([]BestItem, int) {
	// **詰み 문항과 같은 국면만 뺀다.**
	//
	// 한때 그 手数부터 **뒤를 통째로** 잘랐는데, `mateItem` 이 판에서 **가장 이른** 詰み을
	// 고르므로(§53) 120手 판의 25手째에서 놓친 3手詰め 하나가 中盤과 終盤 전부를 후보에서
	// 지웠다 — 진짜 블런더가 있는 구간이 그쪽이다.
	//
	// 자르려던 이유(「詰み 뒤의 국면은 최선수가 詰み 수순이라 두 문항이 같은 것을 묻는다」)는
	// **국면마다 `IsMate` 가 이미 거른다**(score). 앞자리를 통째로 자르는 것은 그 물음에
	// 너무 무딘 도구였다.
	skip := -1
	if mate != nil {
		skip = mate.Ply
	}

	cands := b.candidates(in, posAt, skip)
	if len(cands) > BestCandidates {
		cands = cands[:BestCandidates]
	}

	var items []BestItem
	answered := 0
	for _, c := range cands {
		item, ok, failed := b.score(ctx, in, posAt[c.index], c.index)
		if !failed {
			answered++
		}
		if !ok {
			continue
		}
		items = append(items, item)
	}

	// **gap이 큰 순이다**(문항이 뽑힌 기준이 그것이다). 동률이면 手数가 이른 쪽 —
	// map도 엔진 순서도 안 섞이게 두 축으로 완전히 정한다.
	sort.SliceStable(items, func(i, j int) bool {
		if gi, gj := items[i].Gap(), items[j].Gap(); gi != gj {
			return gi > gj
		}
		return items[i].Ply < items[j].Ply
	})
	if len(items) > BestMaxItems {
		items = items[:BestMaxItems]
	}
	return items, answered
}

// candidates 는 사람이 둔 수마다의 낙폭을 기록에서 세어 큰 순으로 준다. **엔진을 안 부른다.**
//
// skip 은 詰み 문항이 쓰는 手数다(없으면 -1). 그 하나만 뺀다.
func (b *Builder) candidates(in Input, posAt []shogi.Position, skip int) []candidate {
	var out []candidate
	// i=0(첫 수)은 **앞의 평가치가 없어서** 낙폭을 못 센다. 그 자리는 정석 구간이기도 하다.
	start := max(in.OpeningPlies, 1)

	// **`len(posAt)-1` 이다.** `replay` 가 읽을 수 없는 수에서 멈추므로(build.go) 마지막
	// 국면은 있어도 그 자리의 수는 **두어지지 않은 것**이다. 그것까지 도니 `Played` 가 판에
	// 없는 수가 되고, 「사람이 이미 최선수를 뒀다」를 그 수와 견주게 되어 실제로 최선수를
	// 둔 국면이 문항으로 나갈 수 있다. 재현이 온전하면 이 값은 `len(in.Moves)` 와 같다.
	for i := start; i < len(posAt)-1; i++ {
		if i == skip || posAt[i].Turn != in.Human {
			continue
		}
		before, ok := in.PlayerEval(i - 1)
		if !ok {
			continue
		}
		after, ok := in.PlayerEval(i)
		if !ok {
			continue
		}
		out = append(out, candidate{index: i, drop: before - after})
	}

	sort.SliceStable(out, func(x, y int) bool {
		if out[x].drop != out[y].drop {
			return out[x].drop > out[y].drop
		}
		return out[x].index < out[y].index
	})
	return out
}

// score 는 한 국면의 1위·2위를 재서 문항을 만든다.
//
// ok=false 는 「문항이 안 된다」이고, failed=true 는 **못 쟀다**이다. 갈라 두는 이유는
// 조건에 안 맞는 것은 흔한 결과이고 못 잰 것은 회차가 온전하지 않다는 뜻이라서다(Build).
func (b *Builder) score(ctx context.Context, in Input, pos shogi.Position, i int) (item BestItem, ok, failed bool) {
	res, err := b.search.SearchMultiPV(ctx, pos.SFEN(), nil, b.depth, BestMultiPV)
	if err != nil {
		// 한 국면을 못 쟀다고 나머지를 버리지 않는다. 문항이 하나 줄어들 뿐이다.
		log.Printf("quiz: best item at ply %d: %v", i, err)
		return BestItem{}, false, true
	}
	if len(res.Lines) < 2 {
		return BestItem{}, false, false // 후보가 둘 미만이면 「1위와 2위의 차」가 성립하지 않는다
	}
	top, second := res.Lines[0], res.Lines[1]
	if top.Move == "" || second.Move == "" {
		return BestItem{}, false, false // 순위가 비어서 온 자리 (usi.parseScore 의 방어)
	}
	// **mate 점수인 국면은 뺀다** — 「츠메 관련 제외」의 두 번째 그물이고, cp로 환산된
	// mate 점수(30000 - 10×手数)로 gap을 재면 手数 차가 cp 차로 보인다.
	if top.IsMate || second.IsMate {
		return BestItem{}, false, false
	}
	if top.ScoreCp-second.ScoreCp < BestMinGapCp {
		return BestItem{}, false, false
	}
	// 사람이 이미 최선수를 둔 국면은 문항이 아니다. 낙폭으로 좁혔으니 여기 올 일은 드물지만,
	// 오면 「あなたの手は正解でした」를 문제로 내는 셈이 된다.
	if in.Moves[i] == top.Move {
		return BestItem{}, false, false
	}

	// **정답을 정본 표기로 적어 둔다.** `top.Move` 는 엔진이 낸 문자열이고 채점 때 오는 수는
	// 룰 엔진이 만든 것이라(LegalMovesAt) 두 쪽이 서로 맞춰진 적이 없다 — 지금은 파서가
	// 정본만 받아 같지만(shogi.ParseUSIMove) 그 성질에 **정답 판정을 매어 두지 않는다.**
	// 못 읽는 수라면 표기도 채점도 성립하지 않으므로 문항으로 안 만든다.
	answer, err2 := shogi.ParseUSIMove(top.Move)
	if err2 != nil {
		log.Printf("quiz: best item at ply %d: engine gave an unreadable move %q", i, top.Move)
		return BestItem{}, false, false
	}

	return BestItem{
		Ply:  i,
		SFEN: pos.SFEN(),
		// cp는 **사람 관점**이다 — 그 국면의 수번이 사람이라 수번 관점이 곧 그것이다.
		Answer:   answer.USI(),
		AnswerCp: top.ScoreCp,
		SecondCp: second.ScoreCp,
		Played:   in.Moves[i],
	}, true, false
}
