package intervene

import "github.com/jovid18/show-gi/apps/server/internal/eval"

// Category 는 블런더가 왜 나쁜가다. DB의 interventions.category 에 그대로 들어간다.
//
// 목록과 판정 룰은 01-core.md §3.
type Category string

const (
	// CategoryNone 은 개입하지 않았을 때다.
	CategoryNone Category = ""

	// CategoryMissedMate 는 詰み을 놓친 것이다. 종반에는 이것만이 하나뿐인 신호다(§2).
	CategoryMissedMate Category = "missed_mate"

	// CategorySlowerMate 는 詰み이 남았는데 멀어진 것이다.
	//
	// missed_mate 와 따로 두는 것은 사람에게 할 말이 정반대라서다(journal §76).
	CategorySlowerMate Category = "slower_mate"

	// CategoryLetsMate 는 그 수로 내 玉이 詰まされる 것이다. missed_mate 의 거울상이다.
	CategoryLetsMate Category = "lets_mate"

	// CategoryHangsPiece 는 タダ捨て다. 놓은 칸을 상대가 노리는데 내가 지키지 않는다.
	CategoryHangsPiece Category = "hangs_piece"

	// CategoryShallowTrap 은 「얕은 이득에 낚임」이다. 얕게 보면 이득인데 깊게 보면 손해.
	//
	// 제안형 힌트의 「깊이 반전형」과 방향이 반대다. 거울상으로 읽으면 한쪽 실측을
	// 다른 쪽 근거로 쓰게 된다(journal §39 ④).
	CategoryShallowTrap Category = "shallow_trap"

	// CategoryUnpromoted 는 成하지 않은 것이 문제인 수다. 이동은 최선수와 같다.
	CategoryUnpromoted Category = "unpromoted"

	// CategoryGreedyCapture 는 駒得에 눈이 멀어 대가를 보지 못한 것이다.
	CategoryGreedyCapture Category = "greedy_capture"

	// CategoryIdleCheck 는 追う手 — 이어지지 않는 王手다.
	CategoryIdleCheck Category = "idle_check"

	// CategoryKingExposed 는 玉 주변의 수비를 방치한 것이다.
	CategoryKingExposed Category = "king_exposed"

	// CategoryOther 는 미분류다. 판을 읽지 못했을 때도 여기로 떨어진다.
	CategoryOther Category = "other"
)

// Features 는 카테고리를 정하는 데 쓰는 국면의 사실들이다. 뽑아 오는 쪽은 game.MoveFeatures.
type Features struct {
	// Known 이 false면 사실을 구하지 못한 것이다. CategoryOther 로 떨어진다.
	Known bool

	// LandsAttacked 는 상대가 그 칸의 駒를 실제로 딸 수 있는가(합법수로).
	//
	// 「노리고 있는가」와 다르다. 핀에 묶인 駒는 노리기만 한다(01-core.md §3).
	LandsAttacked bool
	// LandsDefended 는 따인 뒤에 되딸 수 있는가. 되딸 수 없는 따는 수가 하나라도
	// 있으면 false 다.
	LandsDefended bool

	// MovedValue 는 그 칸에 놓인 내 駒의 가치다(성했으면 성한 값).
	MovedValue int
	// CapturedValue 는 이 수로 딴 駒의 가치. 따지 않았으면 0.
	CapturedValue int

	// GivesCheck 는 이 수가 王手인가.
	GivesCheck bool

	// UnpromotedOnly 는 최선수와 같은 이동인데 成하지 않은 수인가. 뽑아 오는 쪽은
	// game.UnpromotedOnly 이고, 여기로는 참거짓만 온다.
	UnpromotedOnly bool

	// ShieldLoss 는 내 玉 주변 8칸의 내 방어 利き 감소량. 늘었으면 음수다.
	ShieldLoss int
	// ThreatGain 은 같은 칸들의 상대 공격 利き 증가량. 줄었으면 음수다.
	ThreatGain int

	// Shallow 는 착수 후 국면의 얕은 평가(둔 쪽 관점). HasShallow 가 false면 없는 값이다.
	Shallow    eval.Score
	HasShallow bool

	// OpponentMatePlies 는 이 수 뒤에 상대가 내 玉을 詰ます 手数다. 없으면 0.
	//
	// 채우는 쪽(game.engineAnalyst)이 지켜야 할 두 조건은 01-core.md §3. ②(최선수 뒤에는
	// 그 詰み이 없을 것)는 아직 실전에서 걸러지지 않았다(journal §40 ③).
	OpponentMatePlies int
}

// HangsPiece 는 놓인 駒를 그냥 내주는가다.
//
// 개입의 タダ捨て 판정이자 적응형 상대의 「던지지 않는다」 필터다. 갈렸을 때 무엇이
// 깨지는지는 01-core.md §6.
func (f Features) HangsPiece() bool {
	return f.Known && f.LandsAttacked && !f.LandsDefended && f.MovedValue > f.CapturedValue
}

// ShallowTrapCp 는 「얕게 보면 이득」과 「깊게 보면 손해」 사이의 최소 반전 폭이다. 제안형의
// reversal 임계치와 같은 기준이다.
//
// [미확정] 300은 죽어 있지 않다는 것까지만 확인됐다(journal §39 ⑤).
const ShallowTrapCp = 300

// shallowTrap 은 「얕게 보면 이득, 깊게 보면 손해」인가다. 기준점에서 읽는다.
func shallowTrap(shallow, after eval.Score, baselineCp int) bool {
	shallowCp, shallowIsCp := shallow.Centipawns()
	afterCp, afterIsCp := after.Centipawns()

	if !above(shallow, shallowCp, shallowIsCp, baselineCp) {
		return false
	}
	if above(after, afterCp, afterIsCp, baselineCp) {
		return false
	}
	// 한쪽이 詰み이면 폭이 없다. 부호가 갈린 것 자체가 잴 수 있는 가장 큰 반전이다.
	if !shallowIsCp || !afterIsCp {
		return true
	}
	return shallowCp-afterCp >= ShallowTrapCp
}

// above 는 그 점수가 이 판의 「형세 0」보다 위인가다. 詰み은 이기는 쪽만 위다.
func above(s eval.Score, cp int, isCp bool, baselineCp int) bool {
	if !isCp {
		n, _ := s.MateIn()
		return n > 0
	}
	return cp > baselineCp
}

// classify 는 개입하기로 정해진 수의 이유를 고른다.
//
// 아래 순서가 규칙의 일부다. 한 수가 여러 조건에 걸리는 것은 흔하고, 그때 무엇을
// 말해줄지가 곧 제품의 판단이다(01-core.md §3).
func classify(in Input, lostMate bool) Category {
	if lostMate {
		// 詰み이 남았으면 사람은 아직 이기는 중이다(journal §76).
		if in.MateAfter > 0 {
			return CategorySlowerMate
		}
		return CategoryMissedMate
	}
	f := in.Features
	if !f.Known {
		return CategoryOther
	}

	switch {
	// 成 여부만 다른 수가 맨 앞이다. 이동이 최선수와 같으면 나쁜 이유는 그것뿐이라,
	// 아래 어느 분기로 보내도 설명이 틀린다(08-playtest.md §8).
	case f.UnpromotedOnly:
		return CategoryUnpromoted

	// 詰まされる 것이 나머지 전부보다 앞이다. unpromoted 뒤인 근거는 01-core.md §3.
	case f.OpponentMatePlies > 0:
		return CategoryLetsMate

	// タダ捨て가 그다음이다. 그냥 잡히는 駒는 판에서 그대로 보이고, 딴 것보다 잃는 것이
	// 클 때만 걸리므로 정당한 駒交換은 여기 들어오지 않는다.
	case f.HangsPiece():
		return CategoryHangsPiece

	// 얕은 이득에 낚임. 위에서 걸리지 않았다는 것은 놓인 駒가 그냥 잡히지는 않는다는
	// 뜻이라, 여기 남는 것은 「한 수 앞은 좋아 보이는」 부류다.
	//
	// 두 부호를 기준점에서 읽는다(Input.BaselineCp). 절대 부호로 쓰면 二枚落ち에서 이
	// 카테고리가 판 내내 나오지 않는다(journal §84). 반전 폭은 차이라서 기준점과 무관하다.
	case f.HasShallow && shallowTrap(f.Shallow, in.After, in.BaselineCp):
		return CategoryShallowTrap

	// 駒는 땄는데 형세가 나빠졌다. 딴 것만으로는 부족하고, 대가가 실제로 보이는 둘로
	// 좁힌다: 되따일 수 있거나 玉이 더 밀리거나(01-core.md §3).
	case f.CapturedValue > 0 && (f.LandsAttacked || f.ThreatGain > 0):
		return CategoryGreedyCapture

	// 追う手. 딴 것도 없이 王手만 걸었다 — 위에서 CapturedValue > 0 이 이미 빠졌다.
	// [미확정: 詰めろ 판정이 붙으면 좁힌다]
	case f.GivesCheck:
		return CategoryIdleCheck

	// 玉이 열렸다. 지키던 利き이 줄고 상대 利き이 늘었을 때만이다 — 한쪽만 보면
	// 玉을 자연스럽게 옮기는 수까지 걸린다.
	case f.ShieldLoss > 0 && f.ThreatGain > 0:
		return CategoryKingExposed

	default:
		return CategoryOther
	}
}
