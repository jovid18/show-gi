package game

import (
	"errors"

	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// pieceValue 는 駒交換의 손익을 견주기 위한 값이다. 歩를 1로 잡은 흔한 눈금이다.
//
// **평가치가 아니다.** 형세를 재는 것은 엔진이고 여기서 하는 일은 「이 수로 딴 것보다
// 놓은 것이 비싼가」를 묻는 것뿐이다. cp로 바꾸려 들면 엔진의 평가와 두 벌이 되고,
// 어긋났을 때 어느 쪽이 맞는지 알 수 없다.
//
// 玉은 잡히지 않으므로 값이 의미를 갖는 자리가 없다. 인위 국면 방어로만 크게 둔다.
var pieceValue = map[shogi.PieceType]int{
	shogi.Pawn: 1, shogi.Lance: 4, shogi.Knight: 4, shogi.Silver: 6,
	shogi.Gold: 6, shogi.Bishop: 8, shogi.Rook: 10,
	shogi.PromPawn: 6, shogi.PromLance: 6, shogi.PromKnight: 6, shogi.PromSilver: 6,
	shogi.PromBishop: 11, shogi.PromRook: 13,
	shogi.King: 100,
}

// UnpromotedOnly 는 둔 수가 **최선수와 같은 이동인데 成하지 않은 것**인지 본다.
//
// 이 하나가 없어서 제품이 **틀린 것을 가르쳤다**(08-playtest.md §8). `▲同銀不成` 에
// 「駒は取れますが、払う代償のほうが大きくなります」가 나갔는데, 잡는 것이 정답이었고
// 문제는 成하지 않은 것이었다. 플레이어는 그 문장을 「잡으면 안 되는구나」로 읽고 세 수를
// 더 헤맸다 — 초심자는 설명을 검증할 수단이 없으니 틀린 것을 그대로 배운다.
//
// **판정은 여기서 하고 `intervene` 에는 참거짓만 간다.** 저쪽에 USI 문자열을 넘기면
// 「입력은 이미 구해진 숫자뿐」이 깨진다 — 카테고리를 스칼라로 받게 만든 것과 같은
// 이유다(06-status.md §15).
//
// **거울상(成하지 말아야 했던 수)은 안 본다.** 「成らずの妙手」는 초심자의 실수 모양이
// 아니고, 문구도 「成れます」로 끝낼 수 없다. 그쪽까지 한 카테고리에 넣으면 다시 뭉뚱그린
// 설명이 된다. **[미확정]** 실제로 나오는지는 표본이 더 필요하다.
func UnpromotedOnly(played shogi.Move, bestUSI string) bool {
	if played.IsDrop() || played.Promote || bestUSI == "" {
		return false
	}
	best, err := shogi.ParseUSIMove(bestUSI)
	if err != nil || best.IsDrop() || !best.Promote {
		return false
	}
	return played.From == best.From && played.To == best.To
}

// MoveFeatures 는 착수 전 국면과 그 한 수에서 카테고리 판정에 쓸 사실을 뽑는다.
//
// **여기가 판을 읽는 유일한 자리다.** intervene 은 여기서 나온 숫자만 받는다 —
// 그 패키지가 엔진도 판도 모른다는 성질이 카테고리에도 남아야 하기 때문이다.
//
// 엔진에서 오는 두 값(얕은 평가)은 부르는 쪽이 채운다. 이 함수는 룰 엔진만 쓴다.
func MoveFeatures(before shogi.Position, m shogi.Move) intervene.Features {
	f, _ := moveFacts(before, m)
	return f
}

// moveFacts 는 판정에 쓸 사실과 **설명에 쓸 사실을 한 번에** 뽑는다.
//
// 갈라서 두 번 세면 조용히 어긋난다 — 카테고리는 タダ捨て가 아니라고 판정했는데 문장은
// 「取れる相手の駒が2枚あります」라고 말하는 식이다. 같은 것을 두 곳에서 세는 것이 그
// 어긋남의 원인이므로 **세는 자리를 하나로 둔다.**
//
// 둘의 성격은 다르다. 판정용은 임계치와 견줄 **값**이고(歩 1, 飛 10), 설명용은 화면에 그대로
// 나갈 **이름과 매수**다. 그래서 타입이 갈려 있고, 여기서만 만난다.
func moveFacts(before shogi.Position, m shogi.Move) (intervene.Features, explain.Facts) {
	me := before.Turn
	to := int(m.To)

	f := intervene.Features{Known: true}
	d := explain.Facts{Known: true}

	if cap := before.Board[to]; !cap.Empty() && cap.Color() != me {
		f.CapturedValue = pieceValue[cap.Type()]
		d.Captured = shogi.PieceJa(cap.Type())
	}

	after := before.Apply(m)

	f.MovedValue = pieceValue[after.Board[to].Type()]
	f.GivesCheck = after.InCheck(me.Other())
	// 성했으면 성한 이름이다. 판이 그렇게 그리고 棋譜도 그렇게 적는다.
	d.MovedPiece = shogi.PieceJa(after.Board[to].Type())

	// **利き이 아니라 합법수로 묻는다.** IsAttacked 는 핀을 안 본다 — 玉 앞에 묶여
	// 움직일 수 없는 駒도 「노리고 있다」로 센다. 玉 주변의 압력을 재는 데는 그걸로
	// 충분하지만(AttackCount), 여기서 나온 값은 「その駒は取り返せない場所に
	// 置かれています」라는 **화면에 그대로 나가는 단언**이 된다. 못 잡는 駒를 두고
	// 잡힌다고 말하면 초심자는 그것을 검증할 수단이 없다.
	capturers := legalCapturesOn(after, to)
	f.LandsAttacked = len(capturers) > 0

	// **수가 아니라 매수를 센다.** 같은 駒의 成·不成은 수로 둘이지만 판 위에서는 한 장이라,
	// 수로 세면 화면이 「2枚あります」라고 거짓을 말한다. 그리고 이 값은 「노리는 매수」가
	// 아니라 **실제로 딸 수 있는 매수**다 — 위에서 합법수로 물었기 때문이고, 핀에 묶인 駒를
	// 두고 잡힌다고 말하면 초심자는 그것을 검증할 수단이 없다.
	d.Attackers = distinctSources(capturers)

	// 되딸 수 있는가는 **따인 뒤의 국면**에서 묻는다. 상대는 되따이지 않는 쪽으로
	// 딸 것이므로, 되딸 수 없는 따는 수가 하나라도 있으면 그 駒는 그냥 잡히는 것이다.
	f.LandsDefended = len(capturers) > 0
	for _, c := range capturers {
		if len(legalCapturesOn(after.Apply(c), to)) == 0 {
			f.LandsDefended = false
			break
		}
	}
	d.Defended = f.LandsDefended

	// 玉 주변은 착수 전후로 **각자의 玉 위치**를 기준으로 센다. 玉이 움직이는 수에서
	// 착수 전 자리를 계속 보면 「빈 칸 주변이 허술해졌다」는 엉뚱한 사실이 나온다.
	beforeDefend, beforeThreat := kingPressure(&before, me)
	afterDefend, afterThreat := kingPressure(&after, me)
	f.ShieldLoss = beforeDefend - afterDefend
	f.ThreatGain = afterThreat - beforeThreat

	return f, d
}

// distinctSources 는 수 목록에 등장하는 **駒의 매수**를 센다.
//
// 같은 출발 칸에서 나온 수는 한 장이다 — 成·不成이 두 수로 오는 것이 흔하다. 打는 그
// 자리에 駒가 있으면 애초에 둘 수 없으므로 따는 수에는 들어오지 않지만, 들어와도 한 장으로
// 세도록 -1을 하나의 출처로 취급한다.
func distinctSources(moves []shogi.Move) int {
	seen := make(map[int]struct{}, len(moves))
	for _, m := range moves {
		from := -1
		if !m.IsDrop() {
			from = int(m.From)
		}
		seen[from] = struct{}{}
	}
	return len(seen)
}

// legalCapturesOn 은 sq 위의 駒를 **실제로 딸 수 있는** 합법수를 모은다.
//
// 매 수 한 번 도는 비용이고, 그 옆에서 엔진 탐색이 수백 ms를 쓴다. 정확도를 살 값으로 싸다.
func legalCapturesOn(pos shogi.Position, sq int) []shogi.Move {
	var out []shogi.Move
	for _, m := range pos.LegalMoves() {
		if int(m.To) == sq {
			out = append(out, m)
		}
	}
	return out
}

// kingPressure 는 c의 玉 주변 8칸에 걸린 利き을 (내 방어, 상대 공격)으로 센다.
//
// 玉 자신의 칸은 빼고 주변만 본다. 玉이 잡히는지(=王手)는 다른 신호이고, 여기서
// 재려는 것은 **둘러싼 곳이 허술해졌는가**다.
func kingPressure(pos *shogi.Position, c shogi.Color) (defend, threat int) {
	k := pos.KingSquare(c)
	if k < 0 {
		return 0, 0
	}
	for _, sq := range shogi.Neighbors8(k) {
		defend += pos.AttackCount(sq, c)
		threat += pos.AttackCount(sq, c.Other())
	}
	return defend, threat
}

// replay 는 startSFEN 에 수순을 놓아 착수 **전** 국면과 마지막 한 수를 돌려준다.
//
// 판정은 세션 goroutine 밖에서 도는데, 세션의 국면을 빌려다 읽으면 그 순간
// 「상태를 소유하는 goroutine 하나」가 깨진다. 다시 놓는 편이 싸다 — 수십 번의
// Apply 이고, 그 옆에서 엔진 탐색이 수백 ms를 쓴다.
func replay(startSFEN string, moves []string) (shogi.Position, shogi.Move, error) {
	// 부르는 쪽이 이미 막고 있지만 여기서도 막는다. 판정은 세션 goroutine 밖의
	// 맨 `go func()` 에서 도는데 recover 가 없어서, **여기서 panic 하면 대국이 아니라
	// 서버 프로세스가 죽는다.** 전제를 30줄 떨어진 다른 파일에 맡기지 않는다.
	if len(moves) == 0 {
		return shogi.Position{}, shogi.Move{}, errors.New("replay: no moves")
	}
	pos, err := positionAfter(startSFEN, moves[:len(moves)-1])
	if err != nil {
		return pos, shogi.Move{}, err
	}
	last, err := shogi.ParseUSIMove(moves[len(moves)-1])
	if err != nil {
		return pos, shogi.Move{}, err
	}
	return pos, last, nil
}
