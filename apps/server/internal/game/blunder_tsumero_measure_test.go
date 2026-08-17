package game

import (
	"strconv"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// **`other` 중 詰み이 아닌 것들이 무엇인지 세는 자리다**(journal §40 ⑦). 詰み은 앞선
// 측정이 23건을 찾았고, 여기는 나머지가 「한 수 뒤에 죽는 자리」인지를 본다.
//
// **카테고리를 만들기 위한 준비가 아니다.** 재 보니 3건이고, 그 3건에 이름을 붙이지 않기로
// 했다 — 앱이 어차피 그 수를 되돌리므로 「詰まされる」와 「다음 수에 詰まされる」에서
// 플레이어가 할 행동이 같다. 이 측정의 값은 **남는 31건이 무엇인지 정직하게 적을 수 있게
// 하는 것**이고, 그것으로 끝이다.
//
// 종반의 어휘가 駒得이 아니라 **速度**라는 것은 그대로다 — 「終盤は駒の損得より速度」. 그
// 速度를 세는 단위가 手スキ이고 0手スキ가 詰み, 1手スキ가 詰めろ다.
//
// **판정은 패스로 한다.** 「내가 아무것도 안 하면 詰むか」가 곧 정의라, 수번만 뒤집어 詰み
// 탐색에 물으면 답이 나온다. 판을 뒤집는 것은 SFEN 한 줄이고 엔진은 그것을 그냥 국면으로 받는다.
//
// **어디서 묻는지가 요점이다.** 물러진 수 직후는 상대 차례라 거기서 詰み을 묻는 것이
// 이미 앞 측정이다. 詰めろ는 그 **다음**, 상대가 벌하는 수를 두고 내 차례가 된 자리에서
// 묻는다 — 그리고 그 「벌하는 수」를 `PV[0]` 로 잡는다.
//
// **그 PV 첫 수가 이 측정의 한계다.** §39 ⑦이 같은 국면·같은 깊이가 다른 값을 준다고 쟀고,
// PV도 거기 포함된다. 즉 아래 3건은 **다시 돌리면 달라질 수 있는 숫자**다. 그것이 이 신호를
// 프로덕션에 안 붙인 이유 중 하나이므로, 여기서도 그 숫자를 단정으로 읽지 않는다.
//
// **엔진과 DB가 동시에 필요하다.** 엔진은 arm64 Debian 바이너리라 macOS에서 직접 못 돌고,
// db는 컨테이너라 컨테이너 안에서 localhost 로는 안 보인다 — 돌리는 방법은
// [README](../../README.md) ④에 있다.
//
//	go test ./internal/game/ -run MeasureBlunderTsumero -v -timeout 60m

// passSFEN 은 수번만 뒤집은 국면이다. 「내가 손을 뺐다면」이 곧 詰めろ의 정의다.
//
// **王手를 받고 있을 때는 물으면 안 된다.** 王手는 응수가 강제라 손을 뺄 수 없고,
// 그 국면에서 나온 「詰めろ」는 판 위에서 성립하지 않는 말이다.
func passSFEN(pos shogi.Position) (string, bool) {
	if pos.InCheck(pos.Turn) {
		return "", false
	}
	p := pos
	p.Turn = p.Turn.Other()
	return p.SFEN(), true
}

func TestMeasureBlunderTsumero(t *testing.T) {
	conn := measureDB(t)
	pool := measurePool(t)
	mate := measureMatePool(t)

	all, moves := loadBlunders(t, conn)
	ctx := t.Context()

	var mated, tsumero, inCheck, quiet, unknown, skipped int
	t.Logf("== 詰み이 아닌 `other` 에 詰めろ를 물었다 ==")
	t.Logf("  %-6s %-5s %-9s %-14s %-10s", "game", "ply", "Δwin", "벌하는 수", "판정")

	for _, b := range all {
		if b.category != "other" {
			continue
		}
		gm := moves[b.gameID]
		if _, _, err := replayBlunder(b, gm); err != nil {
			skipped++
			continue
		}
		played := append(append([]string(nil), gm[:b.ply-1]...), b.retracted)

		// 물러진 수 직후에 이미 詰んでいる 것은 앞 측정이 센 23건이다. 여기서는 뺀다.
		if r, err := mate.SearchMate(ctx, b.startSFEN, played); err == nil && r.Found() {
			mated++
			continue
		}

		// 상대가 벌하는 수. **이미 손에 있는 탐색의 PV 첫 수다** — 추가 비용이 없다.
		res, err := pool.SearchDepth(ctx, b.startSFEN, played, JudgeDepth)
		if err != nil || len(res.PV) == 0 {
			skipped++
			continue
		}
		punish := res.PV[0]

		pos, err := positionAfter(b.startSFEN, append(append([]string(nil), played...), punish))
		if err != nil {
			skipped++
			continue
		}

		sfen, ok := passSFEN(pos)
		if !ok {
			// 王手를 받고 있다. 손을 뺄 수 없으니 詰めろ를 물을 자리가 아니다 —
			// 이쪽은 「王手가 이어진다」로 따로 말해야 한다.
			inCheck++
			t.Logf("  %-6d %-5d %-9.3f %-14s %-10s", b.gameID, b.ply, b.deltaWin, punish, "王手中")
			continue
		}

		verdict := "-"
		// **여기서도 「증명된 なし」와 「모른다」를 가른다.** `Proven` 이 false면 solver가
		// 한계(`DepthLimit=11`) 안에서 결론을 못 낸 것이고, 그건 「詰めろ가 아니다」가
		// 아니다. 섞어 세면 조용한 수의 몫이 실제보다 커 보인다.
		switch r, err := mate.SearchMate(ctx, sfen, nil); {
		case err != nil:
			skipped++
			verdict = "탐색 실패"
		case r.Found():
			tsumero++
			verdict = "詰めろ " + strconv.Itoa(len(r.Moves)) + "手"
		case r.Proven:
			quiet++
		default:
			unknown++
			verdict = "불명"
		}
		t.Logf("  %-6d %-5d %-9.3f %-14s %-10s", b.gameID, b.ply, b.deltaWin, punish, verdict)
	}

	t.Logf("\n== 갈래 (詰み 23건을 뺀 나머지) ==")
	t.Logf("  이미 詰み (앞 측정)                     %3d", mated)
	t.Logf("  詰めろ — 벌하는 수 뒤에 손을 빼면 詰む   %3d", tsumero)
	t.Logf("  王手中 — 손을 뺄 수 없다                %3d", inCheck)
	t.Logf("  그래도 조용하다 (증명됨)                %3d", quiet)
	t.Logf("  모른다 (solver가 결론을 못 냈다)        %3d", unknown)
	t.Logf("  못 물었다                               %3d", skipped)
}
