package game

import (
	"context"

	"github.com/jovid18/show-gi/apps/server/internal/book"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/skill"
)

// bookOpponent 는 초반에 고른 진형을 따라 두고, 그것이 성립하지 않게 되면 안쪽 상대에게
// 넘긴다.
//
// 후보 생성도 두 안전 필터도 안 건드린다. adaptive.go 를 감싸기만 하므로 밴드 제어와
// 실력 추정은 그대로 돌고, 이 층이 하는 일은 「초반 몇 수를 정석으로 대신 두는 것」뿐이다 —
// 넘어야 할 선은 05-roadmap.md 의 6번 항목 주석에 있다.
//
// 상태를 안 들고 있다. 지금 몇 수까지 뒀는지도, 넘길 때가 됐는지도 매번 (startSFEN, moves)
// 에서 다시 구한다. 개입이 수를 되무르는 이상 카운터를 들고 있으면 롤백된 수를 센 채로
// 남고, 그 어긋남은 「상대가 갑자기 진형을 건너뛴다」로만 드러나서 잡기 어렵다.
type bookOpponent struct {
	inner Opponent
	// color 는 상대가 잡은 쪽. moves 가 이미 그 색으로 돌아가 있다.
	color shogi.Color
	moves []string
}

// NewBookOpponent 는 진형을 따르는 상대를 만든다. inner 는 북이 끝난 뒤로 계속 쓴다.
//
// 감싸는 자리는 배선이다(server/ws.go) — Config 에 진형을 받지 않는 이유는 세션이
// 상대의 수순을 아는 자리를 만들지 않기 위해서다(Config.OpponentOpening).
//
// color 는 상대가 잡은 쪽이다. 사람의 색을 넘기면 진형이 통째로 반대편에 선다.
func NewBookOpponent(inner Opponent, o book.Opening, color shogi.Color) Opponent {
	return &bookOpponent{inner: inner, color: color, moves: o.Moves(color)}
}

// AdaptsToSkill 은 안쪽에 물어본다.
//
// 여기서 false 를 답하면 진형을 고른 판에서 강함 눈금이 조용히 사라진다 — 화면은
// 추정기 유무가 아니라 이 성질로 갈린다(journal §47).
func (o *bookOpponent) AdaptsToSkill() bool { return adaptsToSkill(o.inner) }

func (o *bookOpponent) Choose(ctx context.Context, startSFEN string, moves []string, sk skill.Estimate) (string, error) {
	if usi, ok := o.next(startSFEN, moves); ok {
		return usi, nil
	}
	return o.inner.Choose(ctx, startSFEN, moves, sk)
}

// ChooseBest 는 북을 건너뛴다(BestPlayer). 불리는 자리가 사람이 詰み을 걸고 있는
// 종반이라 진형을 조립할 국면이 아니고, 북이 그 자리에서 수순을 이어 두면 「최선으로
// 버틴다」가 그 판에서만 조용히 안 지켜진다.
func (o *bookOpponent) ChooseBest(ctx context.Context, startSFEN string, moves []string) (string, error) {
	if b, ok := o.inner.(BestPlayer); ok {
		return b.ChooseBest(ctx, startSFEN, moves)
	}
	return o.inner.Choose(ctx, startSFEN, moves, skill.Unknown)
}

// next 는 이번에 둘 정석 수다. 없으면 두 번째 값이 false 이고, 그때는 안쪽 상대가 고른다.
//
// 판단은 전부 룰 엔진과 좌표뿐이다 — 엔진을 부르지 않는다. adaptive.go 의 자살수 필터가
// 그런 것과 같은 이유이고, 그래서 진형을 늘려 보는 데 엔진이 필요 없다.
func (o *bookOpponent) next(startSFEN string, moves []string) (string, bool) {
	pos, err := shogi.ParseSFEN(startSFENOr(startSFEN))
	if err != nil {
		return "", false
	}

	// 몇 수까지 뒀는지와 「싸움이 시작됐는지」를 한 번의 되두기로 같이 구한다.
	played, fought := 0, false
	for _, u := range moves {
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			return "", false
		}
		// 駒를 잡은 적이 있으면 진형 만들기는 끝이다. 그 뒤로는 사람이 무엇을 하든
		// 상관없이 자기 형태를 쌓는 것이 곧 국면을 안 보는 것이 된다.
		//
		// 「한 번이라도」로 세는 것이 상태를 안 들고도 되돌리기와 맞는 자리다 — 물러진 수는
		// moves 에서 사라지므로 그 잡기도 같이 없던 일이 된다.
		if !m.IsDrop() && !pos.Board[m.To].Empty() {
			fought = true
		}
		if pos.Turn == o.color {
			played++
		}
		pos = pos.Apply(m)
	}

	if fought || played >= len(o.moves) {
		return "", false
	}
	if pos.Turn != o.color || pos.InCheck(o.color) {
		// 王手를 받고 있으면 진형이 아니라 玉이 급하다.
		return "", false
	}

	usi := o.moves[played]
	m, err := shogi.ParseUSIMove(usi)
	if err != nil {
		return "", false
	}
	if err := pos.ValidateMove(m); err != nil {
		// 사람이 그 칸을 먼저 쓴 경우다. 수순을 건너뛰지 않고 통째로 넘긴다 —
		// 한 수만 빼고 이어 두면 그때부터는 어느 진형도 아닌 모양이 된다.
		return "", false
	}
	// adaptive.go 와 같은 필터다. 정석 수는 원래 駒를 버리지 않지만, 사람이 예상 밖으로
	// 둔 국면에서는 같은 좌표가 그냥 주는 수가 될 수 있다.
	if MoveFeatures(pos, m).HangsPiece() {
		return "", false
	}
	return usi, true
}

// startSFENOr 는 빈 값을 평수 초기 국면으로 바꾼다. Config.StartSFEN 의 규약과 같다.
func startSFENOr(s string) string {
	if s == "" {
		return shogi.StartSFEN
	}
	return s
}
