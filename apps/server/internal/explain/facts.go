package explain

import (
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

// Facts 는 문장으로 바꿀 이미 정해진 사실들이다. 전부 결정적으로 구해진 값이다.
//
// 판·SFEN·cp·평가치·칸·Δ승률이 없다. 판과 카드가 이미 말하고 있어서, 문장이 옮겨
// 적으면 두 벌이 되고 어긋났을 때 어느 쪽이 맞는지 알 수 없다.
type Facts struct {
	// Kind 는 제지형(blunder)인가 제안형(tesuji)인가다. 지금은 제지형뿐이다.
	Kind intervene.Kind
	// Category 는 왜 나쁜가다. 이 패키지는 그 값을 읽기만 한다.
	Category intervene.Category
	// Level 은 읽는 사람의 실력 구간이다. 문장의 어휘가 여기서 갈린다.
	Level intervene.Level
	// LostMate 는 종반 판정(詰み을 놓쳤다)으로 걸렸는가.
	LostMate bool

	// Known 이 false면 판을 읽지 못해 아래 사실들이 없는 것이다. 그때 카테고리도 other 다.
	Known bool

	// MovedPiece 는 움직인 駒의 한자다(성했으면 「成銀」처럼 성한 이름).
	MovedPiece string
	// Captured 는 이 수로 딴 駒의 한자. 따지 않았으면 빈 문자열.
	Captured string
	// Attackers 는 놓인 칸의 駒를 실제로 딸 수 있는 상대 駒의 매수다.
	//
	// 「노리는 매수」와 다르다. 핀에 묶여 움직일 수 없는 駒는 세지 않는다. 수 대신 매수로
	// 센다. 같은 駒의 成·不成은 두 수지만 한 장이라, 수로 세면 「2枚あり」가 거짓이 된다.
	Attackers int
	// Defended 는 따인 뒤에 되딸 수 있는가.
	Defended bool
	// MatePlies 는 이 수 뒤에 상대가 내 玉을 詰ます 手数다. 없으면 0.
	// 詰ます 쪽이 처음과 끝을 두므로 늘 홀수이고 상한은 solver 의 DepthLimit(11)이다.
	//
	// 증명된 것만 온다. 탐색의 mate 점수는 증명을 거치지 않아 틀린 手数를 말한다(journal §40).
	MatePlies int

	// MateBefore 는 이 수를 두기 전에 내가 가지고 있던 詰み까지의 手数다. 없으면 0.
	//
	// MatePlies 와 방향이 반대다. 저쪽은 내 玉이 죽는 手数이고 이쪽은 내가 詰ます 手数다.
	// slower_mate 에서만 쓴다.
	//
	// 여기도 증명된 것만 온다. 착수 후의 手数는 탐색이 준 미증명 값이고, 실제로 같은
	// 국면에 14·16·「없음」이 나왔다(journal §76).
	MateBefore int

	// Threatened 는 반박 수순의 첫 수로 상대가 딸 수 있는 내 駒의 한자다. 없으면 빈 값.
	//
	// 카테고리가 이유를 대지 못하는 3분의 2에 「무엇을 잃는가」를 준다(journal §25).
	// 「잃습니다」 대신 「取れます」로 말한다. 실제로 그렇게 될지는 상대가 정한다.
	Threatened string

	// OpponentBest 는 물러진 수 뒤의 상대 최선수의 棋譜 표기(▲3三角成)다. 없으면 빈 값.
	//
	// other 에서만 채워진다. 적기 시작하면 「최선수를 보여주지 않는다」(01-core.md §1)가
	// 카테고리마다 갈린다.
	OpponentBest string

	// Branches 는 그 상대 수 뒤에 내가 둘 수 있는 갈래 셋이다. 없으면 빈 슬라이스.
	//
	// 이것이 붙는 자리는 되물러서 이미 사라진 국면이라 「지금 어떻게 두라」가 되지 않는다.
	// 채우는 쪽은 game.engineAnalyst.otherBranches.
	Branches []Branch

	// Tags 는 이 국면에서 감지된 囲い·전법·戦型의 태그 코드다(tag.Detect가 준다).
	//
	// 문장은 아직 이 값을 쓰지 않는다. 기록과 되짚기가 쓴다.
	Tags []string
}

// Branch 는 「이렇게 두면 이렇게 된다」 한 줄이다.
//
// 셋이 함께여야 뜻이 있다. 하나라도 비면 otherBranches 가 그 줄 전체를 버린다.
type Branch struct {
	// PlayerJa 는 내가 두는 수의 棋譜 표기.
	PlayerJa string
	// ReplyJa 는 그에 대한 상대의 최선 응수.
	ReplyJa string
	// Cp 는 거기까지 갔을 때의 플레이어 관점 평가치다. MateIn 이 0이 아니면 쓰지 않는다.
	Cp int
	// MateIn 은 詰み까지의 手数. 양수면 내가 詰ます 쪽이다. 없으면 0.
	// 0이 아니면 Cp 칸을 보지 않는다. 詰み에는 cp 가 없다.
	MateIn int
}

// namesMoves 는 이 문장이 棋譜 표기를 적는가다.
//
// 갈래가 없어도 상대의 최선수 하나만으로 참이다. renderBranches 가 그 하나로도 쓴다.
func (f Facts) namesMoves() bool {
	return f.OpponentBest != "" || len(f.Branches) > 0
}

// used 는 이 카테고리의 문장이 쓸 수 있는 사실만 남기는 허용 목록이다.
//
// Render 가 이 함수를 지난다. 채우는 쪽(game.engineAnalyst)이 사실을 넉넉히 실어 보내도
// 문장에 나갈 수 있는 것은 여기서 남은 것뿐이다. 사실을 하나 더 쓰려면 여기를 먼저 고친다.
func (f Facts) used() Facts {
	// 판단에 쓰인 값들은 카테고리와 무관하게 남는다. Tags도 국면에 매인 값이라 함께 간다.
	u := Facts{Kind: f.Kind, Category: f.Category, Level: f.Level, LostMate: f.LostMate, Tags: f.Tags}
	if !f.Known {
		return u
	}

	switch f.Category {
	case intervene.CategoryHangsPiece:
		// 그냥 잡히는 駒가 무엇이고 몇 장이 노리는가. 플레이 기록이 이것을 요구했다
		// (08-playtest.md §7).
		u.Known = true
		u.MovedPiece, u.Attackers, u.Defended = f.MovedPiece, f.Attackers, f.Defended

	case intervene.CategoryLetsMate:
		// 手数만 말한다. 玉이 죽는 국면에서 駒의 매수를 말하면 읽는 사람이 駒를 지키러
		// 간다. 「몇 手 뒤에 죽는가」가 그대로 급함의 크기다.
		u.Known = true
		u.MatePlies = f.MatePlies

	case intervene.CategorySlowerMate:
		// 착수 전의 手数만 말한다. 착수 후의 手数는 미증명 값이라 쓸 수 없다(MateBefore).
		u.Known = true
		u.MateBefore = f.MateBefore

	case intervene.CategoryGreedyCapture:
		// 「駒は取れますが」의 그 駒를 이름으로 부른다.
		u.Known = true
		u.Captured = f.Captured

	case intervene.CategoryOther:
		// 이유를 모르는 자리다. 지어내지 않고 그래서 어떻게 되는가를 말한다. 잡히는 駒
		// 하나와, 상대의 최선수 뒤에 갈라지는 세 갈래다(journal §54).
		//
		// 여기만 수를 적을 수 있다. 그 갈래들이 성립하는 국면은 되물러서 이미 사라졌다.
		u.Known = true
		u.Threatened = f.Threatened
		u.OpponentBest, u.Branches = f.OpponentBest, f.Branches

	default:
		// missed_mate · shallow_trap · unpromoted · idle_check · king_exposed.
		// 카테고리 자체가 이미 구체적이라 붙일 사실이 없다. Known 을 켜지 않으면
		// 카테고리 문구가 그대로 나간다.
	}
	return u
}
