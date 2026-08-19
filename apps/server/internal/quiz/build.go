package quiz

import (
	"context"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// Input 은 한 판의 기록이다.
//
// store 를 안 본다. 이 패키지가 아는 것은 국면과 평가치뿐이라, 기록의 모양이 바뀌어도
// 문항 기준은 그대로다 — 옮겨 담는 자리는 부르는 쪽이다(server/quiz.go).
type Input struct {
	// StartSFEN 은 0手目의 국면이다. 비어 있으면 평수 초기 국면이다.
	StartSFEN string
	// Moves 는 확정된 수 전부다. 手数 순이고 구멍이 없어야 한다 — 부르는 쪽이 채워 준다.
	Moves []string
	Human shogi.Color
	// EvalCp[i] 는 i+1 手目를 둔 뒤의 先手 관점 cp다. nil이면 그 手数에 값이 없다
	// (평가치는 수보다 늦게 오므로 마지막 몇 수가 비어 있을 수 있다 — store.RecordedMove).
	EvalCp []*int
	// OpeningPlies 는 컴퓨터가 고른 진형의 수순 길이다. 그 안의 국면은 문항 후보가 아니다.
	//
	// 10수 만에 投了한 판에서 「최선수는?」 셋이 전부 오프닝이 되는 것을 막는다. 정석
	// 수순에는 애초에 둘 만한 수가 여럿이라 gap 하한도 대개 걸러 내지만, 그쪽은 엔진을
	// 쓴 뒤에 거르는 것이고 이쪽은 쓰기 전에 거른다.
	OpeningPlies int
	// Won 은 사람이 이긴 판인가. MateItem.Converted 가 이걸 본다.
	Won bool
}

// PlayerEval 은 i+1 手目를 둔 뒤의 사람 관점 cp다. 없으면 ok=false.
//
// DB의 先手 관점을 여기서 뒤집는다 — 안 뒤집으면 後手로 둔 판의 낙폭 부호가 통째로
// 반대가 되고, 그러면 문항이 잘 둔 자리에서 뽑힌다.
//
// 공개해 둔 것은 옮겨 담는 쪽이(server/ws.go quizInput) 부호 규약을 시험으로 못박을 수
// 있어야 하기 때문이다.
func (in Input) PlayerEval(i int) (int, bool) {
	if i < 0 || i >= len(in.EvalCp) || in.EvalCp[i] == nil {
		return 0, false
	}
	cp := *in.EvalCp[i]
	if in.Human == shogi.White {
		cp = -cp
	}
	return cp, true
}

// Builder 는 문항을 만든다. 엔진 둘을 쓴다 — 詰み solver 와 탐색부는 다른 바이너리다
// (02-architecture.md §3).
//
// 어느 쪽이 nil이면 그쪽 문항만 안 나온다. 대국이 엔진 없이도 도는 것과 같은 규약이다.
type Builder struct {
	mate   MateSearcher
	search MultiSearcher
	depth  int
}

// NewBuilder 는 생성기를 만든다. depth 는 대국이 쓰는 것과 같아야 한다 — 다르면
// positions 가 서로 못 쓰는 두 무리로 갈린다(가정 수순의 whatifDepth 와 같은 이유).
func NewBuilder(mate MateSearcher, search MultiSearcher, depth int) *Builder {
	return &Builder{mate: mate, search: search, depth: depth}
}

// Build 는 한 판에서 문항을 뽑는다.
//
// 에러를 안 돌려준다. 문항이 없는 것은 고장이 아니라 흔한 결과이고(10수 만에 投了한
// 판이 실제로 그렇다), 하나를 못 만든 것이 나머지를 버릴 이유도 아니다.
//
// 대신 엔진이 한 번이라도 답했는가를 함께 준다. 거짓이면 빈 결과가 「이 판에 문항이
// 없다」가 아니라 「아무것도 못 봤다」이고, 그 둘은 화면에서 전혀 다른 말이 되어야 한다 —
// 부르는 쪽은 거짓이면서 비었을 때만 아무것도 안 적는다(server/ws.go generateQuiz).
//
// 한 자리를 못 본 것과 통째로 못 본 것을 가른다. 중반의 무관한 국면 하나에서 solver 가
// 결론을 못 낸 것은 흔한 일이고(df-pn이 timeout 하는 자리다) 그때 나머지는 다 봤으므로
// 「문항이 없다」는 결론이 성립한다. 아무것도 못 본 쪽은 배포가 생성 도중에 껴서 엔진 풀이
// 먼저 닫힌 경우다 — 그때만 결론을 못 낸다.
//
// 엔진이 아예 없는 것도 「못 본」 것이 아니다. 그 배포에는 문항이라는 것이 없고, 그건
// 사실이라 그대로 적어도 된다.
func (b *Builder) Build(ctx context.Context, in Input) (q Quiz, measured bool) {
	posAt := replay(in)
	if len(posAt) == 0 {
		return Quiz{}, true
	}

	answers := 0
	if b.mate != nil {
		item, n := b.mateItem(ctx, in, posAt)
		q.Mate, answers = item, answers+n
	}
	if b.search != nil {
		items, n := b.bestItems(ctx, in, posAt, q.Mate)
		q.Best, answers = items, answers+n
	}
	return q, answers > 0 || (b.mate == nil && b.search == nil)
}

// replay 는 手数마다의 국면을 만든다. posAt[i] 는 i手目까지 둔 뒤의 국면이다.
//
// 구멍에서 멈춘다. 읽을 수 없는 수가 있으면 그 뒤를 안 만든다 — 없던 국면을 그럴듯하게
// 만들면 한 번도 벌어지지 않은 국면을 문항으로 내게 된다(review.go detailOf 와 같은 판단).
func replay(in Input) []shogi.Position {
	start := in.StartSFEN
	if start == "" {
		start = shogi.StartSFEN
	}
	pos, err := shogi.ParseSFEN(start)
	if err != nil {
		return nil
	}

	out := make([]shogi.Position, 0, len(in.Moves)+1)
	out = append(out, pos)
	for _, u := range in.Moves {
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			break
		}
		if err := pos.ValidateMove(m); err != nil {
			break
		}
		pos = pos.Apply(m)
		out = append(out, pos)
	}
	return out
}
