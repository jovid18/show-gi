// Package quiz 는 끝난 판 하나에서 문항을 뽑는다. 근거는 journal §53.
//
// 문항을 고르는 것도 채점하는 것도 엔진과 룰 엔진이다. 「이 국면의 최선수」와
// 「詰み인가」는 검증할 수 없는 문장으로 답하면 안 되는 물음이다(CLAUDE.md).
//
// 만드는 것은 판마다 한 번이고 결과 전체를 저장한다. 판이 끝나면 큐에 들어가고 분석
// 워커가 만든다(server/quiz_jobs.go). 되짚기는 읽기만 한다(§53).
//
// 詰み 문항은 입력을 王手인 수로 닫고 트리를 미리 다 짓는다. 실전 국면은 余詰과
// 無駄合い이 흔해 「엔진이 준 수순과 다르면 오답」으로 채점할 수 없고, 입력이 유한하면
// 채점에 엔진이 들지 않는다(§53).
package quiz

import (
	"context"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// Version 은 문항 생성기의 판번호다. 올리면 옛 행이 무시된다(migrations/007). 문항 기준이
// 바뀌면 옛 문항은 새 기준을 거치지 않고 만든 것이라 채점 규약이 어긋난다.
const Version = 1

const (
	// MateMaxPlies 는 문항으로 삼을 詰み의 상한이다. 고르는 규칙은 mateItem.
	MateMaxPlies = 7

	// MateMinPliesIfConverted 는 사람이 그 詰み을 실제로 決めた 경우의 하한이다. 놓친
	// 詰み은 1手라도 문항이지만, 이미 決めた 1手詰め을 다시 내는 것은 되풀이가 된다.
	MateMinPliesIfConverted = 3

	// MateSearchBudget 는 詰み 탐색 횟수의 상한이다. 바깥의 quizTimeout 이 여전히
	// 마지막에 걸리도록 그 아래로 잡았다. 실전 종반의 트리가 몇 번을 쓰는지는 아직 재지
	// 않았다 [미확정] — 잰 값과 근거는 journal §53.
	//
	// 넘으면 詰み 문항을 버린다. 잘린 트리로 채점하면 정답을 오답이라고 말한다. 「최선수는?」
	// 쪽은 따로 잰 값이라 트리가 서지 못해도 그대로 남는다(server/quiz_jobs.go generateQuiz).
	MateSearchBudget = 2400

	// BestCandidates 는 gap을 재 볼 국면의 수다. 낙폭이 큰 순으로 이만큼만 고른다.
	//
	// 사람 수 전부를 재면 50수 × 956ms ≈ 48초다(§10의 depth 12 × k=5). 낙폭은 기록에
	// 이미 있으므로 좁히는 데 엔진이 들지 않고, 그러면 12초가 된다.
	BestCandidates = 12

	// BestMultiPV 는 gap을 재는 MultiPV다. 쓰는 것은 1·2위뿐인데 5인 이유가 둘이다.
	// 실측표에 있는 칸이 k=5라 비용을 아는 값이고(956ms), 그 행이 가정 수순의 k=3 요청을
	// 그대로 받아친다(archive 는 같은 깊이면 후보가 많은 쪽을 쓴다). 탐색이 버려지지 않는다.
	BestMultiPV = 5

	// BestMinGapCp 는 문항으로 삼을 1위−2위 차의 하한이다. 이 값이 문항의 정의다. 차가
	// 작으면 정답이 사실상 여럿이라 좋은 수를 둔 사람이 「不正解」를 받고, 둘 만한 수가
	// 여럿인 초반 국면도 그래서 저절로 걸린다.
	//
	// 초기값이고 실측이 없다 [미확정] (journal §53).
	BestMinGapCp = 200

	// BestMaxItems 는 gap 문항의 개수 상한이다. 모자라면 그만큼만 나가고, 하나도
	// 없으면 0문이다.
	BestMaxItems = 3

	// BestLinePlies 는 정답 뒤에 적어 둘 수순의 길이다(정답 자신은 세지 않는다).
	// 추가 탐색이 0이다(lineAfter).
	//
	// 짧게 자르는 근거와 [미확정] 로 남은 것은 journal §66.
	BestLinePlies = 4
)

// MateSearcher 는 詰み 탐색이다. *usi.Pool 이 만족한다. 국면을 SFEN으로 넘기는 이유는
// mateSolver.distance.
type MateSearcher interface {
	SearchMate(ctx context.Context, startSFEN string, moves []string) (usi.MateResult, error)
}

// MultiSearcher 는 후보 평가다. archive 로 감싼 풀이 만족한다 — 여기서 나온 값이
// positions 에 쌓인다(§37).
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

// Empty 는 문항이 하나도 없는가. 「끝까지 보지 못했다」와 짝으로 쓴다(Build 의 두 번째 값,
// server/quiz_jobs.go generateQuiz).
func (q Quiz) Empty() bool { return q.Mate == nil && len(q.Best) == 0 }

// MateItem 은 詰み 문항 하나다.
type MateItem struct {
	// Ply 는 문제 국면이 만들어진 手数다. 사람은 Ply+1 手目를 두는 차례다.
	Ply int `json:"ply"`
	// SFEN 은 문제 국면이다. 화면이 그대로 그린다.
	SFEN string `json:"sfen"`
	// Plies 는 詰みまでの手数다. 늘 홀수다(詰ます 쪽이 처음과 끝을 둔다).
	//
	// 사람 눈으로 센 手数와 다를 수 있다. 玉方이 持駒를 들고 있으면 無駄合い이 手数를
	// 늘리고 solver 는 그것도 센다. 늘린 쪽이 실제로 강제되는 手数라 화면에 그대로 적어도
	// 참이다(§53).
	Plies int `json:"plies"`
	// Converted 는 사람이 그 詰み을 대국에서 실제로 決めた가. 세는 법은 Builder.converted.
	Converted bool `json:"converted"`
	// Nodes 는 사람이 둘 차례인 국면 전부다. 키는 手数를 뗀 SFEN(shogi.RepetitionKey)이라
	// 전치가 저절로 합쳐진다 — 詰み 트리에서는 흔하다.
	Nodes map[string]MateNode `json:"nodes"`
}

// MateNode 는 사람이 둘 차례인 국면 하나다.
type MateNode struct {
	// Plies 는 이 국면에서 詰みまでの手数다.
	Plies int `json:"plies"`
	// Moves 는 이 국면의 王手인 수 전부와 그 판정이다. 王手가 아닌 수는 애초에 문항의
	// 입력에서 빠져 여기 없고, 화면도 그 칸을 빛내지 않는다.
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
	// Defense 는 玉方의 최장 방어다. Correct && !Mated 일 때만 있다.
	//
	// 최장이 동률인 응수가 여럿일 때 결정적으로 고른다(journal §53).
	Defense string `json:"defense,omitempty"`
	// Rest 는 이 수와 Defense 뒤에 남는 詰みまでの手数다. 오답 문구가 여기서 갈린다.
	// 0이면 詰み을 놓치는 수이고, 아니면 詰み이 늘어지는 수다. 둘을
	// 「この手では詰みません」으로 뭉치면 9手 詰み이 남는 수에 거짓을 말한다.
	Rest int `json:"rest,omitempty"`
}

// BestItem 은 「この局面の最善手は?」 문항 하나다.
type BestItem struct {
	// Ply 는 문제 국면이 만들어진 手数다. 사람은 Ply+1 手目를 두는 차례다.
	Ply int `json:"ply"`
	// SFEN 은 문제 국면이다.
	SFEN string `json:"sfen"`
	// Answer 는 정답 수다. 화면에 보내지 않는다 — 채점이 서버에 있다(server/quiz.go).
	Answer string `json:"answer"`
	// AnswerCp·SecondCp 는 1위와 2위의 cp다. 사람 관점이다(그 국면의 수번이 사람이다).
	AnswerCp int `json:"answerCp"`
	SecondCp int `json:"secondCp"`
	// Played 는 사람이 대국에서 실제로 둔 수다. 오답 안내가 이걸 쓴다 —
	// 「あなたはこの対局で△を指しました」가 문항을 그 판의 일로 되돌린다.
	Played string `json:"played"`
	// Line 은 정답 뒤에 서로 최선으로 뒀을 때의 수순이다(정답 자신은 없다). 이 칸이
	// 생기기 전에 만들어진 문항은 영영 비어 있다(journal §66).
	//
	// 정답과 같은 취급이다. 맞히기 전에는 응답에 실리지 않는다(server/quiz.go) — 최선
	// 수순의 첫 수가 곧 정답이라 이것만 내보내도 정답을 말한 것이 된다.
	Line []string `json:"line,omitempty"`
}

// Gap 은 1위와 2위의 차다. 문항이 뽑힌 기준이라 화면에도 나간다.
func (b BestItem) Gap() int { return b.AnswerCp - b.SecondCp }

// checkingMoves 는 王手가 되는 합법수만 준다. 엔진을 부르지 않는다. 攻方은 매 수 王手를
// 걸어야 하므로 이 집합이 곧 문항의 입력 전부다(§53).
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
