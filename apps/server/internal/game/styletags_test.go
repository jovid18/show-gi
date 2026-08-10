package game

import (
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// sfenWithTurn 은 양쪽이 **서로 다른** 囲い를 짜고 있는 국면을 준다.
//
//	先手  本美濃囲い  玉2八 · 銀3八 · 金4八 · 金5八
//	後手  金矢倉      玉2二 · 金3二 · 金4三 · 銀3三   (先手의 8八/7八/6七/7七 을 180° 돌린 자리)
//
// **양쪽을 다르게 두는 것이 이 테스트의 조건이다.** 처음에는 양쪽 다 本美濃로 적었는데,
// 그러면 어느 색을 재도 `hon_mino` 가 나와서 **`HumanColor` 를 `shogi.Black` 으로 박아도
// 테스트가 통과했다.** 「플레이어 쪽만 그린다」를 지키는지 재려면 두 쪽의 답이 달라야 한다.
func sfenWithTurn(turn string) string {
	return "9/6gk1/5gs2/9/9/9/9/4GGSK1/9 " + turn + " - 1"
}

func styleCodes(snap Snapshot) []string {
	out := make([]string, 0, len(snap.StyleTags))
	for _, tg := range snap.StyleTags {
		out = append(out, tg.Code)
	}
	return out
}

// 스냅샷이 **플레이어 쪽** 태그를 나른다. 색을 바꿔도 플레이어 쪽이 나와야 한다.
//
// 두 번 재는 것이 요점이다. 한 색으로만 재면 `tag.Detect` 에 늘 先手를 넘기는 버그가
// 통과한다 — 그러면 後手를 잡은 플레이어에게 상대의 囲い 이름이 뜬다.
func TestSnapshotCarriesPlayersOwnStyleTags(t *testing.T) {
	for _, tc := range []struct {
		name     string
		color    shogi.Color
		turn     string
		wantCode string
		wantJa   string
	}{
		{"사람이 先手", shogi.Black, "b", "hon_mino", "本美濃囲い"},
		{"사람이 後手", shogi.White, "w", "kin_yagura", "金矢倉"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// 사람 차례로 시작하게 둔다 — 엔진이 두면 판이 바뀌어 무엇을 재는지 흐려진다.
			opp := &scriptedOpponent{}
			s := newSession(t, Config{
				Opponent:   opp,
				HumanColor: tc.color,
				StartSFEN:  sfenWithTurn(tc.turn),
			})

			snap, err := s.Snapshot(t.Context())
			if err != nil {
				t.Fatalf("Snapshot: %v", err)
			}
			if !snap.YourTurn {
				t.Fatalf("전제가 깨졌다: 사람 차례로 시작하지 않았다 (%+v)", snap.Status)
			}

			got := styleCodes(snap)
			if len(got) != 1 || got[0] != tc.wantCode {
				t.Fatalf("[%s] 를 기대했는데 %v", tc.wantCode, got)
			}
			if snap.StyleTags[0].NameJa != tc.wantJa {
				t.Errorf("화면에 나갈 이름 = %q (%q 기대)", snap.StyleTags[0].NameJa, tc.wantJa)
			}
		})
	}
}

// 初期配置에서는 아무 이름도 안 뜬다. 화면이 첫 수 전에 라벨을 그리지 않게.
func TestSnapshotHasNoStyleTagsAtTheStart(t *testing.T) {
	opp := &scriptedOpponent{}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	snap, err := s.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got := styleCodes(snap); len(got) != 0 {
		t.Errorf("初期配置인데 %v 가 떴다", got)
	}
}
