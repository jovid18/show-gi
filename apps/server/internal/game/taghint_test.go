package game

import (
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/tag"
)

// kataminoOneMoveAway 는 **한 수 뒤에 片美濃가 되는** 국면이다.
//
//	玉2八 · 銀3八   이미 서 있다
//	金4九           4八로 올리면 片美濃(玉2八·銀3八·金4八)가 완성된다
const kataminoOneMoveAway = "8k/9/9/9/9/9/9/6SK1/5G3 b - 1"

// **囲い는 착수 전에 권하지 않는다.**
//
// 이름을 붙이는 쪽(styleTags)에는 그대로 남는다 — 완성된 형태에 이름을 다는 것은 사실이다.
// 뺀 것은 「이 수를 두면 이름이 생긴다」 쪽이고, 근거는 실측이다(06-status.md §44):
// 玉이 囲い 자리에 있는데 이름이 없는 71쪽의 배치가 50가지로 흩어져 있었다 — 빠진 이름이
// 아니라 짓다 만 것이라, 권할 「그 한 수」가 우리가 어느 21종을 구현했느냐로 정해진다.
func TestPreMoveHintsLeaveCastlesOut(t *testing.T) {
	s := newSession(t, Config{
		Opponent:   legalOpponent{},
		HumanColor: shogi.Black,
		StartSFEN:  kataminoOneMoveAway,
	})

	snap, err := s.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	for _, tg := range snap.TagHints {
		if tg.Kind == tag.KindCastle {
			t.Errorf("囲い를 착수 전에 권했다: %s", tg.Code)
		}
	}
}

// **그런데 완성된 囲い에는 이름이 붙어야 한다.** 위 테스트만 있으면 「囲い 감지를 통째로
// 껐다」와 구별되지 않는다 — 경계를 재는 테스트는 양쪽을 함께 짚어야 한다.
func TestFinishedCastlesStillGetTheirName(t *testing.T) {
	// 위 국면에서 金을 4八로 올린 뒤 — 片美濃가 서 있다.
	const done = "8k/9/9/9/9/9/9/5GSK1/9 b - 1"

	s := newSession(t, Config{
		Opponent:   legalOpponent{},
		HumanColor: shogi.Black,
		StartSFEN:  done,
	})

	snap, err := s.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	for _, tg := range snap.StyleTags {
		if tg.Code == "kata_mino" {
			return
		}
	}
	t.Errorf("완성된 片美濃에 이름이 안 붙었다: %+v", snap.StyleTags)
}
