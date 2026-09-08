package game

import (
	"context"

	"github.com/jovid18/show-gi/apps/server/internal/eval"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/tag"
)

// 제안형 手筋 힌트 — 아직 두지 않은 수가 만들 이름을 찾는다(tesuji.go 는 착수 後로 방향이 반대).
//
// 프로덕션에서는 꺼져 있다 — server/ws.go 가 Config.TesujiHint 에 nil 을 넘긴다(journal §78).
// 코드와 측정은 그대로 두고 배선만 끊은 것이라 여기 있는 것은 전부 지금도 도는 코드다.
//
// 게이트는 저쪽 것을 그대로 쓴다 — freshTesuji·enginePaidOff 를 나눠 쓰고 이 파일은 부르는
// 순서만 갖는다. 두 벌이 되면 힌트와 착수 뒤가 어긋난다(journal §34 ⑦).

// TesujiHintRootK 는 착수 전 국면 하나를 잴 때 읽는 줄 수다.
//
// 후보마다 착수 후 국면을 따로 재지 않는다. 한 탐색의 형제 줄을 견주면 뿌리가 같아서
// 낙폭이 지평선 효과에 기울지 않고(journal §41), 비용이 후보 수와 무관해진다.
//
// PV 도 함께 온다. 「이름은 붙었는데 딸 수가 없는」 형태를 가려내려면 최선 수순이
// 있어야 한다(§45 의 kaku_ryodori).
//
// 이 값이 정하는 것은 「어디까지가 확정 탈락인가」다 — k번째 줄이 이미 TesujiLossCp
// 밖이면 그 밖의 후보는 물어보지 않고 떨어뜨린다. 안이면 모르는 것이고, 모르면
// 이름을 붙이지 않는다.
//
// 3은 실측이다(§74). 큰 k 는 비용만 오르고 통과한 이름은 늘지 않는다 — MultiPV 는
// 형제 줄을 함께 유지하느라 가지치기가 k 마다 달라서, k 자체가 정의의 일부다.
const TesujiHintRootK = 3

// TesujiOption 은 지금 두면 새 手筋 이름이 생기는 수 하나다.
type TesujiOption struct {
	USI  string
	Tags []tag.Tag
}

// tesujiOptions 는 합법수를 전부 둬 보고 새 이름이 생기는 것만 남긴다. 엔진을 부르지 않는다.
//
// 싸지 않다. 비용이 합법수 × FindTesuji 라 종반에는 초 단위로 간다(실측은 journal §56).
// 그래서 세션 goroutine 밖에서 돈다 — 안에서 부르면 그동안 스냅샷도 投了도 받을 수
// 없다(session.go 의 maybeTesujiHint).
//
// freshTesuji 를 후보마다 다시 부르므로 착수 전 국면의 이름을 매번 새로 센다. 한 번 구해
// 돌려쓰면 몇 밀리초를 아끼지만, 그러려면 이 파일이 「새 이름인가」의 규칙을 자기 손으로
// 다시 쓰게 된다. 아끼는 쪽이 훨씬 싸다 — 같은 판정이 두 벌이 되는 순간 한쪽만 고치는
// 날이 온다.
func tesujiOptions(pos shogi.Position, c shogi.Color) []TesujiOption {
	// LegalMoves 는 pos.Turn 쪽의 수만 돌려준다. 상대 차례에 물으면 경고 없이 빈 결과가 오므로
	// (에러 없이 「手筋이 없다」로 보인다) 여기서 따로 둔다 — tag.targetSquares 가
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

// gateTesujiOptions 는 후보 중 엔진이 이득으로 본 것만 남긴다.
//
// 탐색은 한 번이다. 착수 前 국면을 k 줄로 재고, 후보의 값을 그 형제 줄에서 꺼낸다.
// 사람이 둘 차례이므로 줄의 점수가 곧 사람 관점이고, 부호를 뒤집는 자리가 없다.
//
// k를 상수로 두지 않고 인자로 받는 것은 depth 와 같다 — 값을 흔들어 보는 데 세션이
// 필요 없어야 한다. 프로덕션이 넣는 값은 TesujiHintRootK.
//
// dropped 는 모르는 채로 남은 후보 수다 — 줄 밖이고 마지막 줄이 아직 상한 안이라
// 확정 탈락이라고 말할 수 없는 것들. 0이 아니면 결과에서 빠진 후보가 있다.
//
// 모르면 이름을 붙이지 않는다. 탐색이 실패하면 전부 빈 결과다 — 룰만으로
// 통과시키면 이 게이트가 없는 것과 같아진다(tesuji.go 의 같은 규약).
func gateTesujiOptions(
	ctx context.Context,
	s MultiSearcher,
	depth, k int,
	startSFEN string,
	moves []string,
	opts []TesujiOption,
	c shogi.Color,
) (kept []TesujiOption, dropped int, err error) {
	if s == nil || len(opts) == 0 {
		return nil, 0, nil
	}

	root, err := s.SearchMultiPV(ctx, startSFEN, moves, depth, k)
	if err != nil {
		return nil, len(opts), err
	}
	// Ranked 를 쓴다. Lines[0] 은 1위가 아닐 수 있다 — 아직 오지 않은 순위가 빈
	// 줄로 남아 있고, 그것을 최선으로 읽으면 낙폭 전체가 어긋난다(usi.SearchResult.Ranked).
	lines := root.Ranked()
	if len(lines) == 0 {
		return nil, len(opts), nil
	}

	best := lines[0].Score
	score := make(map[string]eval.Score, len(lines))
	for _, l := range lines {
		score[l.Move] = l.Score
	}
	// 줄 밖의 후보는 마지막 줄보다 나쁘다. 그 마지막 줄이 이미 상한 밖이면 밖은
	// 전부 탈락이 확정이고, 안이면 모르는 것이다 — 그 둘을 같은 침묵으로 섞지 않는다.
	//
	// k줄을 다 받았을 때만 그렇게 말할 수 있다. 중간 순위 하나가 오지 않으면 Ranked 가
	// 그것을 빼고 주므로, 「밖」에는 오지 않은 그 순위도 섞인다 — 그것은 마지막 줄보다
	// 나쁘지 않다. 덜 받았으면 경계를 모르는 것이고, 모르면 이름을 붙이지 않는다.
	//
	// 詰み이 섞이면 「모른다」다. 경계를 cp 뺄셈으로 재는 자리라 자가 다른 두 값의 차를
	// 임계치와 견줄 수 없고, 그때는 밖을 잘라도 되는지를 말할 수 없다(enginePaidOff 와
	// 같은 판단이다).
	decided := false
	if len(lines) == k {
		bestCp, okBest := best.Centipawns()
		lastCp, okLast := lines[len(lines)-1].Score.Centipawns()
		decided = okBest && okLast && bestCp-lastCp > TesujiLossCp
	}

	for _, o := range opts {
		after, ok := score[o.USI]
		if !ok {
			if !decided {
				dropped++
			}
			continue
		}
		// 두 값이 한 탐색의 형제 줄이라 뿌리가 같다. 개입 판정과 같은 기준을 쓰되
		// 임계치만 다른 것은 그대로다(enginePaidOff).
		j := Judgement{
			SenteBefore: senteScore(best, c),
			SenteAfter:  senteScore(after, c),
			HasEvals:    true,
		}
		if enginePaidOff(j, c) {
			kept = append(kept, o)
		}
	}
	return kept, dropped, nil
}

// tesujiHintTags 는 남은 후보들의 이름을 중복 없이 편다.
//
// 화면은 이름 단위로 한 번만 띄우므로(useTagAnnounce) 같은 이름을 만드는 수가 둘이어도
// 알릴 것은 하나다. 수를 짚지 않는 것이 계단 ①의 성질이다 — 「어디에 있는지」는 3회
// 실패해야 열린다(buildHint).
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
