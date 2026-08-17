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
// 뺀 것은 「이 수를 두면 이름이 생긴다」 쪽이고, 근거는 실측이다(journal §44):
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

// **전법도 착수 전에 권하지 않는다.**
//
// 囲い와 이유가 다르다. 저쪽은 「짓다 만 형태에 이름이 없다」였고(§44), 이쪽은 **飛를 어느
// 筋으로 振るか가 그 사람이 고르는 것**이라서다. 첫 수 앞에서 「中飛車になります」가 뜨면
// 그건 힌트가 아니라 지시이고, 사람이 실제로 그렇게 읽었다(회차 1 #0 · §71).
func TestPreMoveHintsLeaveFormationsOut(t *testing.T) {
	s := newSession(t, Config{
		Opponent:   legalOpponent{},
		HumanColor: shogi.Black,
	})

	snap, err := s.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	for _, tg := range snap.TagHints {
		if tg.Kind == tag.KindFormation {
			t.Errorf("전법을 착수 전에 권했다: %s", tg.Code)
		}
	}
}

// **그런데 振った 뒤에는 이름이 붙어야 한다.** 위 테스트만 있으면 「전법 감지를 통째로
// 껐다」와 구별되지 않는다 — 囲い 쪽과 같은 짝이다.
func TestSwungRooksStillGetTheirName(t *testing.T) {
	s := newSession(t, Config{
		Opponent:   legalOpponent{},
		HumanColor: shogi.Black,
	})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	// ▲7六歩 → 상대의 응수를 기다렸다가 ▲6八飛. 6筋이고 자기 2段이라 四間飛車다.
	// **상대 수는 비동기로 온다** — 기다리지 않고 두면 「내 차례가 아니다」로 막힌다.
	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("7g7f: %v", err)
	}
	waitFor(t, ch, func(snap Snapshot) bool { return len(snap.Moves) >= 2 }, "상대의 응수")
	if _, err := s.Play(t.Context(), "2h6h"); err != nil {
		t.Fatalf("2h6h: %v", err)
	}

	waitFor(t, ch, func(snap Snapshot) bool { return hasTag(snap.StyleTags, "shiken_bisha") }, "四間飛車 이름")
}

// **제안 채널에 남는 것은 戦型 하나다.** 셋 중 둘을 뺐으므로, 무엇이 남았는지를 못 박아
// 두지 않으면 다음에 축을 하나 더 빼면서 채널이 조용히 죽는다.
func TestPreMoveHintsAreOpeningsOnly(t *testing.T) {
	for _, k := range []tag.Kind{tag.KindCastle, tag.KindFormation, tag.KindTesuji} {
		if hintable(tag.Tag{Kind: k}) {
			t.Errorf("%s 를 착수 전에 권한다", k)
		}
	}
	if !hintable(tag.Tag{Kind: tag.KindOpening}) {
		t.Error("戦型까지 빠지면 이 채널에 남는 것이 없다")
	}
}
