package game

import (
	"context"
	"fmt"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// MultiSearcher 는 후보 여러 개를 한 번에 받아오는 탐색이다. usi.Pool 이 만족한다.
//
// 적응형 상대에게 후보 풀이 없으면 성립하지 않는다 — **강함을 탐색 길이가 아니라
// 고르는 쪽에서 얻겠다는 설계가 곧 MultiPV를 요구한다**(01-core.md §6).
type MultiSearcher interface {
	SearchMultiPV(ctx context.Context, startSFEN string, moves []string, depth, multiPV int) (usi.SearchResult, error)
}

// Band 는 상대가 겨냥하는 형세 구간이다. **플레이어 관점 cp**다.
//
// 양수면 플레이어가 유리하다는 뜻이므로, 상대는 자기가 조금 지는 쪽을 겨냥한다.
type Band struct{ LoCp, HiCp int }

// DefaultBand 는 「조금씩 지고 있지만 아직 모른다」에 해당하는 구간이다.
//
// **[+100,+300] 그대로 둔다. 기록으로 처음 확인했다**(06-status.md §39. §26이 「지켜졌는지
// 알 수 없다」고 적어둔 자리다). 실제 대국 5판에서 **상대가 두고 넘겨준** 국면 228개의
// 사람 관점 cp를 세면 중앙값이 **+233**이고 밴드 안이 **43.4%**다.
//
// 중앙값이 구간 한가운데에 앉았으므로 **겨냥하는 자리는 맞다.** 절반 넘게 밖인 것은
// 어긋난 것이 아니라 설계 그대로다 — 밴드 위로 나가는 것은 플레이어가 이미 크게
// 이겨서 상대가 최선을 다하는 중이고(§16의 「불리할 때는 저절로 최선을 다한다」),
// 밴드가 좁힐 수 있는 구간이 애초에 아니다.
//
// **[미확정]** 레이팅이 붙으면 플레이어 실력에 따라 좁아진다(01-core.md §6).
var DefaultBand = Band{LoCp: 100, HiCp: 300}

// CandidateK 는 후보를 몇 개까지 받을지다. **실측으로 정했다**(06-status.md §10).
//
// 밴드 적중이 10에서 멈추고 20은 덮이는 국면이 안 늘면서 비용만 배가 된다.
// k=1은 0/6이다 — 후보가 하나면 밴드 제어라는 것이 성립할 수 없다.
const CandidateK = 10

type adaptiveOpponent struct {
	search MultiSearcher
	depth  int
	k      int
	band   Band
}

// NewAdaptiveOpponent 는 「지지만 던지지 않는」 상대를 만든다.
//
// 기존 약한 AI와 갈리는 지점은 **자살수 필터**다. 플레이어의 블런더는 개입이 막아주므로
// 컴퓨터가 무리해서 駒를 던질 필요가 없다 — 조금씩 안 좋은 선택을 쌓는 것만으로 진다.
//
// **약하게 만드는 일은 엔진이 아니라 우리가 한다.** 엔진의 `Skill Level` 류를 쓰지 않는
// 이유는, 엔진이 스스로 실수를 섞으면 **고른 수가 얼마나 나쁜지를 우리가 모르게 되기**
// 때문이다. 평가치가 오염되면 밴드 제어가 성립하지 않고, 「최선보다 180cp 나쁜 수였다」고
// 말할 근거도 같이 사라진다. 엔진은 늘 정직하게 최대 강도로 평가하고, 약화는 고르는
// 자리에서만 일어난다 (01-core.md §6).
func NewAdaptiveOpponent(s MultiSearcher, depth int, band Band) Opponent {
	if depth < 1 {
		depth = DefaultDepth
	}
	if band.LoCp == 0 && band.HiCp == 0 {
		band = DefaultBand
	}
	return &adaptiveOpponent{search: s, depth: depth, k: CandidateK, band: band}
}

func (o *adaptiveOpponent) Choose(ctx context.Context, startSFEN string, moves []string) (string, error) {
	res, err := o.search.SearchMultiPV(ctx, startSFEN, moves, o.depth, o.k)
	if err != nil {
		return "", err
	}
	if res.Best == "" {
		return "", fmt.Errorf("adaptive: engine returned no move")
	}

	pos, err := positionAfter(startSFEN, moves)
	if err != nil {
		// 판을 못 읽으면 고를 근거가 없다. **최선수로 물러선다** — 약화는 부가 기능이고
		// 대국이 본체다. 개입 판정이 실패해도 대국을 멈추지 않는 것과 같은 판단이다.
		return res.Best, nil
	}

	best, bestDist := res.Best, -1
	for _, line := range res.Lines {
		if line.Move == "" {
			continue
		}
		m, err := shogi.ParseUSIMove(line.Move)
		if err != nil {
			continue
		}
		// 엔진은 늘 수번 측(=상대) 관점으로 답한다. 밴드는 플레이어 관점이라 뒤집는다.
		playerCp := -line.ScoreCp

		// 「던지지 않는다」. 이 필터는 **엔진이 필요 없다** — 룰 엔진만으로 된다.
		if MoveFeatures(pos, m).HangsPiece() {
			continue
		}
		if d := o.band.distance(playerCp); bestDist < 0 || d < bestDist {
			best, bestDist = line.Move, d
		}
	}
	return best, nil
}

// distance 는 평가치가 밴드에서 얼마나 벗어났는가다. 안에 들면 0.
//
// **밴드 밖일 때 「가장 가까운 것」을 고르는 것으로 충분하다.** 설계 문서에는 밴드에 드는
// 후보가 없으면 그냥 최선수를 두라고 적혀 있었는데, 그 규칙은 필요 없을 뿐 아니라 한쪽에서
// 해롭다. 거리를 최소화하면 두 경우가 저절로 갈리기 때문이다.
//
//	상대가 크게 지고 있다   후보가 전부 밴드 위 → 제일 작은 것 = **엔진의 최선수**
//	상대가 크게 이기고 있다 후보가 전부 밴드 아래 → 제일 큰 것 = 플레이어에게 가장 너그러운 수
//
// 즉 불리할 때는 저절로 최선을 다하고(「던지지 않는다」), 유리할 때만 양보한다. 여기서
// 최선수로 물러서면 **이기고 있을 때 조절을 포기하는 것**이 되어, 조절이 가장 필요한
// 자리에서 상대가 초심자를 그대로 뭉갠다.
//
// 문서가 걱정한 「승부가 갈린 종반」은 후보들이 20~40cp에 몰려 어느 수를 골라도 같은
// 국면이라, 거기서 가장 가까운 것을 골라도 최선수와 사실상 같다.
func (b Band) distance(cp int) int {
	switch {
	case cp < b.LoCp:
		return b.LoCp - cp
	case cp > b.HiCp:
		return cp - b.HiCp
	default:
		return 0
	}
}

// positionAfter 는 startSFEN 에 수순을 전부 놓은 국면을 돌려준다.
func positionAfter(startSFEN string, moves []string) (shogi.Position, error) {
	pos, err := shogi.ParseSFEN(startSFEN)
	if err != nil {
		return pos, err
	}
	for _, u := range moves {
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			return pos, err
		}
		pos = pos.Apply(m)
	}
	return pos, nil
}
