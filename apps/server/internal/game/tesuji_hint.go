package game

import (
	"context"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/tag"
)

// 제안형 手筋 힌트 — **아직 안 둔 수**가 만들 이름을 찾는다.
//
// `tesuji.go` 와 묻는 방향이 반대다. 저쪽은 방금 둔 수에 이름을 붙이는 것(착수 後)이고,
// 이쪽은 둘 수 있는 수 중에 이름이 붙을 것이 있는지(착수 前)다. `tag/tesuji.go:210` 이
// 「후보 수를 전부 둬 보며 찾는 쪽이 제안형 힌트가 원하는 것」이라고 적어둔 자리다.
//
// **게이트는 저쪽 것을 그대로 쓴다.** 이름을 통과시키는 조건이 두 곳에서 갈리면 같은 수가
// 힌트에서는 手筋이고 착수 뒤에는 아니게 된다 — 화면이 자기 자신과 어긋나고, 그게
// 06-status.md §34 ⑦이 잡은 실패다. 그래서 `freshTesuji` 와 `enginePaidOff` 를 나눠 쓰고
// 이 파일은 **부르는 순서만** 갖는다.

// TesujiHintMultiPV 는 후보를 재는 탐색의 MultiPV다.
//
// 1이 아닌 이유는 이 값이 **후보의 cp만 쓰는 것이 아니기 때문**이다 — 최선 수순(PV)이
// 함께 와야 「이름은 붙었는데 딸 수가 없는」 형태를 나중에 가려낼 수 있다(계획 1b의
// 十字飛車 사례: 飛가 銀 둘을 노렸는데 어느 쪽을 먹어도 飛가 죽는다).
//
// 3인 것은 그 이상이 비용만 늘리기 때문이다. 상대의 후보 폭을 재는 `CandidateK` 와 값이
// 다른데, 저쪽은 **고르는** 수의 폭이고 이쪽은 **읽는** 줄의 수라 같을 이유가 없다.
const TesujiHintMultiPV = 3

// TesujiHintMaxCandidates 는 엔진에 물어볼 후보의 상한이다.
//
// 실측으로는 안 걸린다 — 새 이름이 생기는 수는 국면당 0~2개이고, 실 기보 102수에서
// 형태가 새로 생긴 수가 통틀어 6개였다(06-status.md §34). 그래도 상한을 두는 것은
// 종반에 합법수가 몰릴 때 사람 차례가 통째로 멈추는 것을 막기 위해서다.
//
// **넘치면 조용히 자르지 않고 세어서 돌려준다**(dropped). 잘린 것을 안 세면 「手筋이
// 없었다」와 「못 봤다」가 같은 화면이 된다.
const TesujiHintMaxCandidates = 6

// TesujiOption 은 지금 두면 **새 手筋 이름이 생기는** 수 하나다.
type TesujiOption struct {
	USI  string
	Tags []tag.Tag
}

// tesujiOptions 는 합법수를 전부 둬 보고 새 이름이 생기는 것만 남긴다. **엔진을 안 부른다.**
//
// 비용은 합법수 × `FindTesuji` 이고 초기 국면에서 90µs 남짓이라, 80수를 훑어도 10ms 안쪽이다.
//
// `freshTesuji` 를 후보마다 다시 부르므로 착수 전 국면의 이름을 매번 새로 센다. 한 번 구해
// 돌려쓰면 몇 밀리초를 아끼지만, 그러려면 이 파일이 「새 이름인가」의 규칙을 자기 손으로
// 다시 쓰게 된다. **아끼는 쪽이 훨씬 싸다** — 같은 판정이 두 벌이 되는 순간 한쪽만 고치는
// 날이 온다.
func tesujiOptions(pos shogi.Position, c shogi.Color) []TesujiOption {
	// `LegalMoves` 는 `pos.Turn` 쪽의 수만 낸다. 상대 차례에 물으면 조용히 빈 결과가 오므로
	// (에러가 아니라 「手筋이 없다」로 보인다) 여기서 갈라 둔다 — `tag.targetSquares` 가
	// 같은 함정에 물렸던 자리다.
	if pos.Turn != c {
		return nil
	}

	var out []TesujiOption
	for _, m := range pos.LegalMoves() {
		usi := m.USI()
		if names := freshTesuji(pos, pos.Apply(m), c, usi); len(names) > 0 {
			out = append(out, TesujiOption{USI: usi, Tags: names})
		}
	}
	return out
}

// gateTesujiOptions 는 후보 중 **엔진이 이득으로 본 것만** 남긴다.
//
// 후보마다 착수 후 국면을 한 번 재고, 착수 전 국면은 한 번만 잰다. 그래서 탐색 횟수가
// `1 + len(opts)` 이고 후보가 보통 0~2개다.
//
// dropped 는 상한에 걸려 **묻지 못한** 후보 수다. 0이 아니면 결과가 「전부」가 아니다.
//
// **모르면 이름을 붙이지 않는다.** 탐색이 실패하면 그 후보를 빼고 계속한다 — 룰만으로
// 통과시키면 이 게이트가 없는 것과 같아진다(`tesuji.go` 의 같은 규약).
func gateTesujiOptions(
	ctx context.Context,
	s MultiSearcher,
	depth int,
	startSFEN string,
	moves []string,
	opts []TesujiOption,
	c shogi.Color,
) (kept []TesujiOption, dropped int, err error) {
	if s == nil || len(opts) == 0 {
		return nil, 0, nil
	}
	if len(opts) > TesujiHintMaxCandidates {
		dropped = len(opts) - TesujiHintMaxCandidates
		opts = opts[:TesujiHintMaxCandidates]
	}

	// 착수 **전** 국면. 사람이 둘 차례이므로 엔진의 답이 곧 사람 관점이다.
	before, err := s.SearchMultiPV(ctx, startSFEN, moves, depth, TesujiHintMultiPV)
	if err != nil {
		return nil, dropped, err
	}
	senteBefore := senteCp(before.ScoreCp, c)

	for _, o := range opts {
		next := append(append([]string(nil), moves...), o.USI)

		after, err := s.SearchMultiPV(ctx, startSFEN, next, depth, TesujiHintMultiPV)
		if err != nil {
			// 한 후보를 못 쟀다고 나머지를 버리지 않는다. 대국이 본체이고 힌트는 부가다.
			continue
		}

		// 착수 후에는 **상대**가 수번이라 엔진의 답이 상대 관점이다. 부호를 한 번 뒤집고
		// 나서 先手 관점으로 옮긴다 — `analyst.go:126` 과 같은 두 걸음이다.
		j := Judgement{
			SenteCpBefore: senteBefore,
			SenteCpAfter:  senteCp(-after.ScoreCp, c),
			HasEvals:      true,
		}
		if enginePaidOff(j, c) {
			kept = append(kept, o)
		}
	}
	return kept, dropped, nil
}

// tesujiHintTags 는 남은 후보들의 이름을 **중복 없이** 편다.
//
// 화면은 이름 단위로 한 번만 띄우므로(useTagAnnounce) 같은 이름을 만드는 수가 둘이어도
// 알릴 것은 하나다. 수를 짚지 않는 것이 계단 ①의 성질이다 — 「어디에 있는지」는 3회
// 실패해야 열린다(`buildHint`).
func tesujiHintTags(opts []TesujiOption) []tag.Tag {
	seen := map[string]bool{}
	var out []tag.Tag
	for _, o := range opts {
		for _, t := range o.Tags {
			if !seen[t.Code] {
				seen[t.Code] = true
				out = append(out, t)
			}
		}
	}
	return out
}
