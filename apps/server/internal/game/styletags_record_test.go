package game

import (
	"slices"
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// sfenWithSpareRook 은 `sfenWithTurn("b")` 에 **비어 있는 段의 飛** 하나를 더한 것이다.
//
// 저쪽 국면에는 先手의 駒가 囲い를 이루는 넷뿐이라 **어느 수를 둬도 그 囲い가 깨진다.**
// 여기서 재려는 것은 「같은 이름이 여러 手에 걸쳐 있을 때 한 번만 기록되는가」이므로,
// 囲い를 안 건드리고 왕복할 수 있는 駒가 하나 필요하다.
const sfenWithSpareRook = "9/6gk1/5gs2/9/9/9/9/4GGSK1/1R7 b - 1"

// named 는 기록된 이름만 순서대로 뽑는다.
func named(rec *fakeRecorder) []string {
	var out []string
	for _, e := range rec.all() {
		if code, ok := strings.CutPrefix(e, "named "); ok {
			out = append(out, code)
		}
	}
	return out
}

// 사람이 짠 이름이 기록으로 간다. **판에 뜨는 것과 같은 이름이어야 한다** — 갈라 두면
// 대국 중에 본 이름과 마이페이지가 세는 이름이 다른 것이 된다(recordStyleTags).
func TestNamedStyleTagsAreRecordedOncePerGame(t *testing.T) {
	rec := &fakeRecorder{}
	s := newSession(t, Config{
		Opponent:   &scriptedOpponent{moves: []string{"3c3d", "3d3e"}},
		Recorder:   rec,
		HumanColor: shogi.Black,
		StartSFEN:  sfenWithSpareRook,
	})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	// 玉·金·銀을 안 건드리는 수를 둔다 — 本美濃가 그대로 서 있어야 「같은 이름이 여러 번
	// 나온다」는 조건이 성립한다. **상대의 응수까지 기다린다** — 확정된 수마다 다시 세므로
	// (recordLastMove) 두 번째 셈이 그 자리에서 일어난다.
	if _, err := s.Play(t.Context(), "8i9i"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	waitFor(t, ch, func(s Snapshot) bool { return len(s.Moves) >= 2 }, "상대가 두기")

	got := named(rec)
	if !slices.Contains(got, "hon_mino") {
		t.Fatalf("짠 囲い가 기록에 없다: %v", got)
	}
	// **두 번 세지 않는다.** 囲い는 판에서 매번 다시 세어지므로, 거르지 않으면 手数만큼
	// 같은 이름이 쌓이고 마이페이지의 「몇 局」이 「몇 手」가 된다.
	if n := slices.IndexFunc(got[slices.Index(got, "hon_mino")+1:], func(c string) bool {
		return c == "hon_mino"
	}); n >= 0 {
		t.Errorf("같은 이름이 두 번 기록됐다: %v", got)
	}
}

// **상대의 囲い는 기록하지 않는다.** 판에 뜨는 것이 플레이어 쪽뿐이라(styleTags),
// 기록만 양쪽을 담으면 마이페이지가 사람이 본 적 없는 이름을 세운다.
func TestOpponentStyleTagsAreNotRecorded(t *testing.T) {
	rec := &fakeRecorder{}
	s := newSession(t, Config{
		Opponent:   &scriptedOpponent{moves: []string{"3c3d"}},
		Recorder:   rec,
		HumanColor: shogi.Black,
		StartSFEN:  sfenWithSpareRook,
	})

	if _, err := s.Play(t.Context(), "8i9i"); err != nil {
		t.Fatalf("Play: %v", err)
	}

	// 이 국면에서 後手가 짜고 있는 것은 金矢倉이다(sfenWithTurn).
	if got := named(rec); slices.Contains(got, "kin_yagura") {
		t.Errorf("상대의 囲い가 기록됐다: %v", got)
	}
}
