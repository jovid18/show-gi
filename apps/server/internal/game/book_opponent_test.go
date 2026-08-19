package game

import (
	"context"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/book"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/skill"
)

// stubOpponent 는 불렸는지만 기억한다. 북이 손을 놓았는가가 이 파일의 관심사라,
// 안쪽이 무엇을 고르는지는 여기서 볼 것이 아니다.
type stubOpponent struct {
	called int
	adapts bool
	move   string
}

func (o *stubOpponent) Choose(context.Context, string, []string, skill.Estimate) (string, error) {
	o.called++
	return o.move, nil
}

func (o *stubOpponent) AdaptsToSkill() bool { return o.adapts }

func mustFind(t *testing.T, id string) book.Opening {
	t.Helper()
	o, ok := book.Find(id)
	if !ok {
		t.Fatalf("진형 %q 가 없다", id)
	}
	return o
}

// TestBookPlaysInOrder 는 사람이 정석대로 받아주지 않아도 진형이 순서대로 나오는지다.
// 짝으로 묶지 않은 것이 이 성질이고(book 패키지 주석), 그것이 깨지면 북이 첫 수에서 끝난다.
func TestBookPlaysInOrder(t *testing.T) {
	inner := &stubOpponent{move: "1g1f"}
	// 상대가 後手다. 수순은 돌아서 나온다.
	opp := NewBookOpponent(inner, mustFind(t, "shikenbisha"), shogi.White)

	// 사람(先手)이 7六歩. 상대의 첫 정석수는 ▲7六歩을 돌린 △3四歩이다.
	got, err := opp.Choose(context.Background(), "", []string{"7g7f"}, skill.Estimate{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "3c3d" {
		t.Errorf("첫 수 = %q, want 3c3d", got)
	}

	// 사람이 전법과 무관한 수를 둬도 두 번째 정석수가 그대로 나온다.
	got, err = opp.Choose(context.Background(), "", []string{"7g7f", "3c3d", "1g1f"}, skill.Estimate{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "8b4b" { // ▲6八飛 을 돌린 △4二飛
		t.Errorf("두 번째 수 = %q, want 8b4b", got)
	}
	if inner.called != 0 {
		t.Errorf("북이 도는 동안 안쪽이 %d번 불렸다", inner.called)
	}
}

// TestBookYieldsAfterCapture 는 駒를 잡은 뒤로는 진형을 안 쌓는지다. 초심자가 초반에
// 교환을 시작했을 때 상대가 그것을 안 보고 囲い를 계속 쌓는 것이 이 층의 가장 나쁜 실패다.
func TestBookYieldsAfterCapture(t *testing.T) {
	inner := &stubOpponent{move: "1c1d"}
	opp := NewBookOpponent(inner, mustFind(t, "shikenbisha"), shogi.White)

	// ▲7六歩 △3四歩 ▲2二角成 — 角을 잡았다. 다음은 後手(상대) 차례다.
	moves := []string{"7g7f", "3c3d", "8h2b+"}
	got, err := opp.Choose(context.Background(), "", moves, skill.Estimate{})
	if err != nil {
		t.Fatal(err)
	}
	if inner.called != 1 {
		t.Fatalf("안쪽이 %d번 불렸다 — 잡기 뒤에는 넘겨야 한다", inner.called)
	}
	if got != inner.move {
		t.Errorf("안쪽의 수가 안 나왔다: %q", got)
	}
}

// TestBookIsDerivedFromMoves 는 되무르기와 맞는지다.
//
// 개입이 수를 물리면 moves 가 줄어든다. 카운터를 들고 있었다면 그 수를 센 채로 남아 진형이
// 한 칸 건너뛴다 — 상태를 안 들고 있다는 것이 그 버그가 아예 없다는 뜻이다.
func TestBookIsDerivedFromMoves(t *testing.T) {
	inner := &stubOpponent{}
	opp := NewBookOpponent(inner, mustFind(t, "shikenbisha"), shogi.White)

	long := []string{"7g7f", "3c3d", "1g1f"}
	first, err := opp.Choose(context.Background(), "", long, skill.Estimate{})
	if err != nil {
		t.Fatal(err)
	}

	// 사람의 수가 물러져 수순이 줄었다. 같은 자리로 돌아오면 같은 답이어야 한다.
	back, err := opp.Choose(context.Background(), "", []string{"7g7f"}, skill.Estimate{})
	if err != nil {
		t.Fatal(err)
	}
	if back != "3c3d" {
		t.Errorf("되물러진 뒤 = %q, want 3c3d (첫 정석수)", back)
	}
	if first == back {
		t.Errorf("줄어든 수순에서 같은 수가 나왔다: %q", first)
	}
}

// TestBookYieldsWhenPositionRejectsIt 는 정석수를 둘 수 없는 국면이면 넘기는지다.
// 수순을 한 수만 빼고 이어 두지 않는 이유는 book_opponent.go 의 그 자리 주석.
func TestBookYieldsWhenPositionRejectsIt(t *testing.T) {
	inner := &stubOpponent{move: "5a4a"}
	opp := NewBookOpponent(inner, mustFind(t, "shikenbisha"), shogi.White)

	// 歩도 飛도 없는 국면. 첫 정석수 △3四歩이 애초에 반칙이다.
	got, err := opp.Choose(context.Background(), "4k4/9/9/9/9/9/9/9/4R2K1 w - 1", nil, skill.Estimate{})
	if err != nil {
		t.Fatal(err)
	}
	if inner.called != 1 || got != inner.move {
		t.Errorf("반칙인 정석수에서 안 넘겼다: called=%d got=%q", inner.called, got)
	}
}

// TestBookHandsOverWhenExhausted 는 수순을 다 두면 안쪽 상대가 이어받는지다.
// 진형을 다 짜고 나서가 이 기능의 목적지다 — 그 뒤로는 밴드 제어가 대국을 끌고 간다.
func TestBookHandsOverWhenExhausted(t *testing.T) {
	opening := mustFind(t, "nakabisha")
	inner := &stubOpponent{move: "1c1d"}
	opp := NewBookOpponent(inner, opening, shogi.White)

	pos, err := shogi.ParseSFEN(shogi.StartSFEN)
	if err != nil {
		t.Fatal(err)
	}
	var moves []string
	want := opening.Moves(shogi.White)

	// 상대가 後手이므로 사람이 먼저 둔다. 사람 쪽은 판을 흔들지 않는 수만 고른다 —
	// 잡으면 북이 그 자리에서 손을 놓아(TestBookYieldsAfterCapture) 이 테스트가 뜻을 잃는다.
	for i := 0; i < len(want); i++ {
		m, ok := quietPawnMove(pos)
		if !ok {
			t.Fatalf("%d번째: 사람 쪽에 밀 歩가 없다", i)
		}
		pos = pos.Apply(m)
		moves = append(moves, m.USI())

		got, err := opp.Choose(context.Background(), "", moves, skill.Estimate{})
		if err != nil {
			t.Fatal(err)
		}
		if got != want[i] {
			t.Fatalf("%d번째 정석수 = %q, want %q", i+1, got, want[i])
		}
		bm, err := shogi.ParseUSIMove(got)
		if err != nil {
			t.Fatal(err)
		}
		pos = pos.Apply(bm)
		moves = append(moves, got)
	}

	if inner.called != 0 {
		t.Fatalf("수순이 남았는데 안쪽이 %d번 불렸다", inner.called)
	}

	// 여기서부터는 북이 없다.
	m, ok := quietPawnMove(pos)
	if !ok {
		t.Fatal("사람 쪽에 밀 歩가 없다")
	}
	moves = append(moves, m.USI())
	got, err := opp.Choose(context.Background(), "", moves, skill.Estimate{})
	if err != nil {
		t.Fatal(err)
	}
	if inner.called != 1 || got != inner.move {
		t.Errorf("수순을 다 뒀는데 안 넘겼다: called=%d got=%q", inner.called, got)
	}
}

// TestBookForwardsAdaptsToSkill 은 강함 눈금이 살아 있는지다. 진형을 고른 판에서 눈금이
// 사라지는 것이 여기서 갈린다 — 화면은 추정기 유무가 아니라 이 성질을 본다(§47).
func TestBookForwardsAdaptsToSkill(t *testing.T) {
	o := mustFind(t, "shikenbisha")
	for _, adapts := range []bool{true, false} {
		opp := NewBookOpponent(&stubOpponent{adapts: adapts}, o, shogi.White)
		if got := adaptsToSkill(opp); got != adapts {
			t.Errorf("안쪽이 %v인데 %v를 답했다", adapts, got)
		}
	}
}

// quietPawnMove 는 잡지 않는 歩 밀기 하나다. 결정적으로 고른다.
func quietPawnMove(pos shogi.Position) (shogi.Move, bool) {
	for _, m := range pos.LegalMoves() {
		if m.IsDrop() || m.Promote {
			continue
		}
		if pos.Board[m.From].Type() != shogi.Pawn || !pos.Board[m.To].Empty() {
			continue
		}
		return m, true
	}
	return shogi.Move{}, false
}
