// Package quiz 는 끝난 판 하나에서 문항을 뽑는다. 근거는 docs/06-status.md §53.
//
// **문항을 고르는 것도 채점하는 것도 엔진과 룰 엔진이다.** LLM은 여기 없다 — 「이 국면의
// 최선수」와 「詰み인가」는 검증할 수 없는 문장으로 답하면 안 되는 물음이고, 초심자는 그
// 문장이 틀렸는지 알 수단이 없다(CLAUDE.md).
//
// 만드는 것은 **판이 끝나는 자리에서 한 번**이고, 결과는 통째로 저장된다. 되짚기가 열 때는
// 읽기만 한다 — 되짚기에서 만들면 그 탐색이 진행 중인 다른 대국의 착수를 기다리게 한다.
//
// # 詰み 문항이 왜 완전한 트리인가
//
// 실전 국면은 詰将棋 작품이 아니다. 玉方의 持駒가 「나머지 전부」가 아니고, **같은 手数의
// 다른 詰み筋(余詰)이 흔하다.** 그래서 「엔진이 준 수순과 다르면 오답」으로 채점하면
// 맞은 사람에게 틀렸다고 말한다.
//
// 대신 입력을 **王手인 수로 닫는다.** 화면이 王手만 빛내고 서버가 그 밖을 거절하므로,
// 사람이 낼 수 있는 입력이 유한하고 미리 셀 수 있다 — 玉方의 응수는 우리가 고르므로
// 트리가 **모든 경우를 덮고, 채점에 엔진이 필요 없다.**
package quiz

import (
	"context"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// Version 은 문항 생성기의 판이다. **올리면 옛 행이 무시된다** — 문항 기준이 바뀌면 옛
// 문항은 그 기준으로 만든 것이 아니라서 채점 규약이 어긋난다(migrations/007).
const Version = 1

const (
	// MateMaxPlies 는 문항으로 삼을 詰み의 상한이다. 문제는 **사람 차례 국면을 手数 순으로
	// 훑어 처음으로 이 안에 들어온 것**이다.
	//
	// 「가장 어려운 것」이 아니라 「최초」인 이유는 그것이 판에서 詰み이 처음 성립한 자리이고,
	// 늦은 국면일수록 이미 승부가 갈려 배울 것이 적기 때문이다.
	MateMaxPlies = 7

	// MateMinPliesIfConverted 는 사람이 그 詰み을 대국에서 **실제로 決めた** 경우의 하한이다.
	//
	// 놓친 詰み은 1手라도 문항이 된다 — 초심자는 실제로 1手詰め을 놓친다. 하지만 이미
	// 決めた 1手詰め을 다시 내는 것은 문항이 아니라 되풀이다.
	MateMinPliesIfConverted = 3

	// MateSearchBudget 는 詰み 탐색 횟수의 상한이다. 탐색 하나가 실측 107ms라(§31) 이 값은
	// 약 4.3분이고, **바깥의 `quizTimeout`(5분)이 여전히 마지막에 걸리도록** 그 아래로 잡았다.
	//
	// 회차를 자르는 자리를 **하나로 두려는 것**이 이 값의 뜻이다. 예산이 먼저 걸리면
	// 「몇 번까지」와 「몇 분까지」 둘이 서로를 가려서, 무엇 때문에 잘렸는지가 로그에서 안 갈린다.
	//
	// **실전 종반의 트리가 몇 번을 쓰는지는 아직 안 쟀다** [미확정]. 잰 것은 시험의 합성
	// 국면뿐이고(3手·11회, §53) 그쪽은 駒가 거의 없어 바닥값이다 — 持駒가 있으면 王手 후보와
	// 合駒가 둘 다 늘고 그 둘이 곱해져서, **뿌리 노드 하나가 이 값을 통째로 쓸 수도 있다.**
	//
	// **넘으면 詰み 문항을 버린다.** 잘린 트리는 완전하지 않고, 완전하지 않은 트리로 채점하면
	// 「정답을 오답이라고 말하는」 경우가 생긴다. 다만 **「최선수는?」 쪽까지 버리지는 않는다** —
	// 그쪽은 따로 잰 값이라 트리가 못 서도 그대로 참이다(server/ws.go generateQuiz).
	MateSearchBudget = 2400

	// BestCandidates 는 gap을 재 볼 국면의 수다. **낙폭이 큰 순으로** 이만큼만 고른다.
	//
	// 사람 수 전부를 재면 50수 × 956ms ≈ 48초다(§10의 depth 12 × k=5). 낙폭은 기록에
	// 이미 있으므로 좁히는 데 엔진이 안 들고, 그러면 12초가 된다.
	BestCandidates = 12

	// BestMultiPV 는 gap을 재는 MultiPV다. 쓰는 것은 1·2위뿐인데 5인 이유가 둘이다 —
	// 실측표에 있는 칸이 k=5라 비용을 아는 값이고(956ms), **그 행이 가정 수순의 k=3 요청을
	// 그대로 받아친다**(archive 는 같은 깊이면 후보가 많은 쪽을 쓴다). 탐색이 버려지지 않는다.
	BestMultiPV = 5

	// BestMinGapCp 는 문항으로 삼을 1위−2위 차의 하한이다.
	//
	// 이 값이 **문항의 정의**다. 차가 작으면 정답이 사실상 여럿이고, 그런 국면을 내면
	// 초심자가 좋은 수를 두고 「不正解」를 받는다. 초반 국면이 저절로 걸러지는 것도 이 값이다 —
	// 둘 만한 수가 여럿인 국면이 곧 그것이다.
	//
	// **초기값이고 실측이 없다** [미확정]. 재려면 사람이 둔 판의 문항 수와 정답률을 본다.
	BestMinGapCp = 200

	// BestMaxItems 는 gap 문항의 개수 상한이다. 모자라면 그만큼만 나가고, 하나도 없으면 0문이다.
	BestMaxItems = 3
)

// MateSearcher 는 詰み 탐색이다. `*usi.Pool` 이 만족한다.
//
// **국면을 SFEN으로 넘긴다** — 트리는 실제로 두어지지 않은 수를 따라가므로 手数 경로로
// 부르면 경로가 길어지고 노드 키와 어긋난다.
type MateSearcher interface {
	SearchMate(ctx context.Context, startSFEN string, moves []string) (usi.MateResult, error)
}

// MultiSearcher 는 후보 평가다. archive 로 감싼 풀이 만족한다 — 여기서 나온 값이
// `positions` 에 쌓인다(§37).
type MultiSearcher interface {
	SearchMultiPV(ctx context.Context, startSFEN string, moves []string, depth, multiPV int) (usi.SearchResult, error)
}

// Quiz 는 한 판에서 뽑은 문항 전부다.
type Quiz struct {
	// Mate 는 詰み 문항. nil이면 그 판에 사람 쪽 詰み이 없었거나 예산에 걸렸다.
	Mate *MateItem `json:"mate,omitempty"`
	// Best 는 「이 국면의 최선수는?」 문항이다. 최대 BestMaxItems 개이고 비어 있을 수 있다.
	Best []BestItem `json:"best,omitempty"`
}

// Empty 는 문항이 하나도 없는가.
//
// **「끝까지 못 봤다」와 짝으로 쓴다**(Build 의 두 번째 값). 못 봤는데 비었으면 그것은
// 결론이 아니므로 부르는 쪽이 아무것도 안 적는다 — 하나라도 나왔으면 나온 것은 사실이라
// 그대로 적는다(server/ws.go generateQuiz).
func (q Quiz) Empty() bool { return q.Mate == nil && len(q.Best) == 0 }

// MateItem 은 詰み 문항 하나다.
type MateItem struct {
	// Ply 는 문제 국면이 만들어진 手数다. 사람은 `Ply+1` 手目를 두는 차례다.
	Ply int `json:"ply"`
	// SFEN 은 문제 국면이다. 화면이 그대로 그린다.
	SFEN string `json:"sfen"`
	// Plies 는 詰みまでの手数다. **늘 홀수다** — 詰ます 쪽이 처음과 끝을 둔다.
	//
	// 사람 눈으로 센 手数와 다를 수 있다. 玉方이 持駒를 들고 있으면 無駄合い이 手数를
	// 늘리고, solver 는 그것도 手数로 센다. 늘린 쪽이 **실제로 강제되는 手数**라 화면에
	// 그대로 적어도 거짓이 아니다(§53).
	Plies int `json:"plies"`
	// Converted 는 사람이 그 詰み을 대국에서 실제로 決めた가.
	//
	// **수를 견주지 않고 手数로 센다.** 「같은 詰み筋인가」로 보면 余詰을 놓친 것으로
	// 세는데(실전에서는 흔하다), 어느 筋으로 詰ましても 판은 그 手数 안에 끝난다.
	Converted bool `json:"converted"`
	// Nodes 는 **사람이 둘 차례인 국면 전부**다. 키는 手数를 뗀 SFEN(shogi.RepetitionKey)이라
	// 전치가 저절로 합쳐진다 — 詰み 트리에서는 흔하다.
	Nodes map[string]MateNode `json:"nodes"`
}

// MateNode 는 사람이 둘 차례인 국면 하나다.
type MateNode struct {
	// Plies 는 이 국면에서 詰みまでの手数다.
	Plies int `json:"plies"`
	// Moves 는 이 국면의 **王手인 수 전부**와 그 판정이다. 王手가 아닌 수는 애초에 문항의
	// 입력이 아니라 여기 없고, 화면도 그 칸을 빛내지 않는다.
	Moves map[string]MateVerdict `json:"moves"`
	// Best 는 오답에 보여줄 정답 수다. 여럿이면(余詰) 그중 하나이고 결정적으로 고른다.
	Best string `json:"best"`
}

// MateVerdict 는 王手 하나의 판정이다.
type MateVerdict struct {
	// Mated 는 이 수가 詰み인가. 참이면 그 자리에서 정답이고 문항이 끝난다.
	Mated bool `json:"mated,omitempty"`
	// Correct 는 詰みまでの手数가 2 이상 줄어드는가. Mated 면 참이다.
	Correct bool `json:"correct,omitempty"`
	// Defense 는 玉方의 **최장 방어**다. `Correct && !Mated` 일 때만 있다.
	//
	// 최장이 동률인 응수가 여럿일 때 결정적으로 고른다 — 매번 다르게 응수하면 같은
	// 문제가 열 때마다 다르게 흘러가고, 사람은 그것을 고장으로 읽는다.
	Defense string `json:"defense,omitempty"`
	// Rest 는 이 수와 Defense 뒤에 **남는** 詰みまでの手数다. 0이면 詰み이 사라진다.
	//
	// **오답 문구가 여기서 갈린다.** 0이면 詰み을 놓치는 수이고, 아니면 詰み이 늘어지는
	// 수다 — 둘을 「この手では詰みません」으로 뭉치면 9手 詰み이 남는 수에 거짓을 말한다.
	Rest int `json:"rest,omitempty"`
}

// BestItem 은 「この局面の最善手は?」 문항 하나다.
type BestItem struct {
	// Ply 는 문제 국면이 만들어진 手数다. 사람은 `Ply+1` 手目를 두는 차례다.
	Ply int `json:"ply"`
	// SFEN 은 문제 국면이다.
	SFEN string `json:"sfen"`
	// Answer 는 정답 수다. **화면에 안 보낸다** — 채점이 서버에 있다(server/quiz.go).
	Answer string `json:"answer"`
	// AnswerCp·SecondCp 는 1위와 2위의 cp다. **사람 관점**이다(그 국면의 수번이 사람이다).
	AnswerCp int `json:"answerCp"`
	SecondCp int `json:"secondCp"`
	// Played 는 사람이 대국에서 실제로 둔 수다. 오답 안내가 이걸 쓴다 —
	// 「あなたはこの対局で△を指しました」가 문항을 그 판의 일로 되돌린다.
	Played string `json:"played"`
}

// Gap 은 1위와 2위의 차다. 문항이 뽑힌 기준이라 화면에도 나간다.
func (b BestItem) Gap() int { return b.AnswerCp - b.SecondCp }

// checkingMoves 는 王手가 되는 합법수만 준다. **엔진을 안 부른다.**
//
// 詰将棋에서 攻方은 매 수 王手를 걸어야 하므로, 이 집합이 곧 문항의 입력 전부다.
// `LegalMoves` 가 二歩·行き所のない駒·打ち歩詰め·자기 玉이 남는 수를 이미 뺀다.
func checkingMoves(pos shogi.Position) []shogi.Move {
	var out []shogi.Move
	them := pos.Turn.Other()
	for _, m := range pos.LegalMoves() {
		np := pos.Apply(m)
		if np.InCheck(them) {
			out = append(out, m)
		}
	}
	return out
}
