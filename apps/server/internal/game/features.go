package game

import (
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

// MoveFeatures 는 착수 전 국면과 그 한 수에서 카테고리 판정에 쓸 사실을 뽑는다.
//
// **여기가 판을 읽는 유일한 자리다.** intervene 은 여기서 나온 숫자만 받는다 —
// 그 패키지가 엔진도 판도 모른다는 성질이 카테고리에도 남아야 하기 때문이다.
//
// 엔진에서 오는 두 값(얕은 평가)은 부르는 쪽이 채운다. 이 함수는 룰 엔진만 쓴다.
func MoveFeatures(before shogi.Position, m shogi.Move) intervene.Features {
	me := before.Turn
	to := int(m.To)

	f := intervene.Features{Known: true}

	if cap := before.Board[to]; !cap.Empty() && cap.Color() != me {
		f.CapturedValue = pieceValue[cap.Type()]
	}

	after := before.Apply(m)

	f.MovedValue = pieceValue[after.Board[to].Type()]
	f.LandsAttacked = after.IsAttacked(to, me.Other())
	f.LandsDefended = after.IsAttacked(to, me)
	f.GivesCheck = after.InCheck(me.Other())

	// 玉 주변은 착수 전후로 **각자의 玉 위치**를 기준으로 센다. 玉이 움직이는 수에서
	// 착수 전 자리를 계속 보면 「빈 칸 주변이 허술해졌다」는 엉뚱한 사실이 나온다.
	beforeDefend, beforeThreat := kingPressure(&before, me)
	afterDefend, afterThreat := kingPressure(&after, me)
	f.ShieldLoss = beforeDefend - afterDefend
	f.ThreatGain = afterThreat - beforeThreat

	return f
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
	pos, err := shogi.ParseSFEN(startSFEN)
	if err != nil {
		return pos, shogi.Move{}, err
	}
	for _, u := range moves[:len(moves)-1] {
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			return pos, shogi.Move{}, err
		}
		pos = pos.Apply(m)
	}
	last, err := shogi.ParseUSIMove(moves[len(moves)-1])
	if err != nil {
		return pos, shogi.Move{}, err
	}
	return pos, last, nil
}
