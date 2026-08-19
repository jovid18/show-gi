package explain

import (
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

// Facts 는 문장으로 바꿀 이미 정해진 사실들이다. 전부 결정적으로 구해진 값이고,
// 판·SFEN·cp·평가치는 여기 없다 — 말할 수 있는 것으로 좁혀진 입력이다.
//
// 칸과 Δ승률이 없는 것이 설계다 — 둘 다 판과 카드가 이미 말하고 있어서, 문장이 옮겨
// 적으면 두 벌이 되고 어긋났을 때 어느 쪽이 맞는지 알 수 없다.
type Facts struct {
	// Kind 는 제지형(blunder)인가 제안형(tesuji)인가다. 지금은 제지형뿐이다 —
	// 「なぜ悪いか」와 「ここに何かある」는 문장이 아예 다르므로 붙는 날 갈라진다.
	Kind intervene.Kind
	// Category 는 왜 나쁜가다. 이 패키지는 그 값을 읽기만 한다.
	Category intervene.Category
	// Level 은 읽는 사람의 실력 구간이다. 문장의 어휘가 여기서 갈린다.
	Level intervene.Level
	// LostMate 는 종반 판정(詰み을 놓쳤다)으로 걸렸는가.
	LostMate bool

	// Known 이 false면 판을 못 읽어 아래 사실들이 없는 것이다. 지어내지 않는다 —
	// 카테고리도 그때 other 로 떨어진다.
	Known bool

	// MovedPiece 는 움직인 駒의 한자다(성했으면 「成銀」처럼 성한 이름).
	MovedPiece string
	// Captured 는 이 수로 딴 駒의 한자. 안 땄으면 빈 문자열.
	Captured string
	// Attackers 는 놓인 칸의 駒를 실제로 딸 수 있는 상대 駒의 매수다.
	//
	// 「노리는 매수」가 아니다 — 핀에 묶여 못 움직이는 駒는 세지 않는다. 그리고 수가
	// 아니라 매수다. 같은 駒의 成·不成은 두 수지만 한 장이라, 수로 세면 화면이
	// 「2枚あり」라고 거짓을 말한다.
	Attackers int
	// Defended 는 따인 뒤에 되딸 수 있는가.
	Defended bool
	// MatePlies 는 이 수 뒤에 상대가 내 玉을 詰ます 手数다. 없으면 0.
	// 詰ます 쪽이 처음과 끝을 두므로 늘 홀수이고 상한은 solver 의 DepthLimit(11)이다.
	//
	// 증명된 것만 온다 — 탐색의 mate 점수는 증명이 아니라 틀린 手数를 말하고,
	// 틀린 手数를 적으면 초심자는 검증할 수단이 없다(journal §40).
	MatePlies int

	// MateBefore 는 이 수를 두기 전에 내가 가지고 있던 詰み까지의 手数다. 없으면 0.
	//
	// MatePlies 와 방향이 반대다 — 저쪽은 내 玉이 죽는 手数이고 이쪽은 내가 詰ます 手数다.
	// slower_mate 에서만 쓴다.
	//
	// 여기도 증명된 것만 온다(MatePlies 와 같은 규칙). 착수 후의 手数는 solver 가
	// 아니라 탐색이 준 값이라 증명이 아니고, 실제로 같은 국면에 14·16·「없음」이 나왔다 —
	// 그래서 문장이 그쪽 숫자를 안 쓴다(journal §76).
	MateBefore int

	// Threatened 는 반박 수순의 첫 수로 상대가 딸 수 있는 내 駒의 한자다. 없으면 빈 값.
	//
	// 카테고리가 이유를 못 대는 3분의 2에 「무엇을 잃는가」를 주는 자리다(journal §25).
	// 「잃습니다」가 아니라 「取れます」로 말한다 — 실제로 그렇게 될지는 상대가 정하지만,
	// 그 수가 합법이라는 것은 룰 엔진이 확인한 사실이다.
	Threatened string

	// OpponentBest 는 물러진 수 뒤의 상대 최선수의 棋譜 표기(▲3三角成)다. 없으면 빈 값.
	//
	// other 에서만 채워진다. 나머지 카테고리는 이유를 이름으로 대므로 수를 적을 필요가
	// 없고, 적기 시작하면 「최선수를 보여주지 않는다」(01-core.md §1)가 카테고리마다 갈린다.
	OpponentBest string

	// Branches 는 그 상대 수 뒤에 내가 둘 수 있는 갈래 셋이다. 없으면 빈 슬라이스.
	//
	// 이것이 붙는 자리는 되물러서 이미 사라진 국면이다 — 「지금 어떻게 두라」가 아니라
	// 「그 수를 뒀다면 어떻게 됐나」라, 답을 알려주는 것과 다른 일이다. 채우는 쪽은
	// game.engineAnalyst.otherBranches.
	Branches []Branch

	// Tags 는 이 국면에서 감지된 囲い·전법·戦型의 태그 코드다(tag.Detect가 준다).
	//
	// 문장은 아직 이 값을 안 쓴다 — 기록과 되짚기가 쓰고, 여기 실려 오는 것은 사실 하나에
	// 태그가 붙어 다니게 두는 편이 나중에 문구를 태그로 가를 때 배선이 안 늘어나기 때문이다.
	Tags []string
}

// Branch 는 「이렇게 두면 이렇게 된다」 한 줄이다.
//
// 셋이 한 벌로만 뜻이 있다 — 내 수·상대의 응수·그 결말. 하나라도 비면 채우는 쪽이
// 그 줄을 통째로 버린다(otherBranches).
type Branch struct {
	// PlayerJa 는 내가 두는 수의 棋譜 표기.
	PlayerJa string
	// ReplyJa 는 그에 대한 상대의 최선 응수.
	ReplyJa string
	// Cp 는 거기까지 갔을 때의 플레이어 관점 평가치다. MateIn 이 0이 아니면 안 쓴다.
	Cp int
	// MateIn 은 詰み까지의 手数. 양수면 내가 詰ます 쪽이다. 없으면 0 —
	// cp로 적으면 30000이 그대로 문장에 나간다(whatifCandidate 와 같은 판단이다).
	MateIn int
}

// namesMoves 는 이 문장이 棋譜 표기를 적는가다.
//
// 갈래가 없어도 상대의 최선수 하나만으로 참이 된다 — renderBranches 가 그 하나만으로도
// 쓸 문장을 갖고 있고, 여기서 갈래만 보면 그 사실이 문장에서 조용히 사라진다.
func (f Facts) namesMoves() bool {
	return f.OpponentBest != "" || len(f.Branches) > 0
}

// used 는 이 카테고리의 문장이 쓸 수 있는 사실만 남기는 허용 목록이다.
//
// Render 가 이 함수를 지난다. 카테고리마다 말할 수 있는 사실이 다르다 —
// 채우는 쪽(game.engineAnalyst)이 사실을 넉넉히 실어 보내도 문장에 나갈 수 있는 것은
// 여기서 남은 것뿐이고, 그래서 사실을 하나 더 쓰려면 이 목록을 먼저 고쳐야 한다.
func (f Facts) used() Facts {
	// 판단에 쓰인 값들은 카테고리와 무관하게 남는다. Tags도 국면에 매인 값이라 함께 간다.
	u := Facts{Kind: f.Kind, Category: f.Category, Level: f.Level, LostMate: f.LostMate, Tags: f.Tags}
	if !f.Known {
		return u
	}

	switch f.Category {
	case intervene.CategoryHangsPiece:
		// 그냥 잡히는 駒가 무엇이고 몇 장이 노리는가. 플레이 기록이 정확히 이것을
		// 요구했다 — 「어느 駒인지, 몇 개가 지키는지를 숫자로」(08-playtest.md §7).
		u.Known = true
		u.MovedPiece, u.Attackers, u.Defended = f.MovedPiece, f.Attackers, f.Defended

	case intervene.CategoryLetsMate:
		// 手数만 말한다. 「무엇이 몇 장에게 노려지는가」는 여기서 의미가 없다 — 玉이
		// 죽는 국면에서 駒의 매수를 말하면 읽는 사람이 駒를 지키러 간다. 그리고 이 카테고리는
		// 「몇 手 뒤에 죽는가」가 그대로 급함의 크기라, 그 하나가 문장을 완성한다.
		u.Known = true
		u.MatePlies = f.MatePlies

	case intervene.CategorySlowerMate:
		// 착수 전의 手数만 말한다. 그 값은 solver 가 증명한 것이고, 착수 후의 手数는
		// 탐색이 준 미증명 값이라 문장에 못 쓴다(MateBefore). 그래서 이 카테고리의 문장은
		// 「5手で詰ませられました」까지가 숫자이고 뒤는 「遠回りになります」다.
		u.Known = true
		u.MateBefore = f.MateBefore

	case intervene.CategoryGreedyCapture:
		// 「駒は取れますが」의 그 駒를 이름으로 부른다.
		u.Known = true
		u.Captured = f.Captured

	case intervene.CategoryOther:
		// 이유를 모르는 자리다. 지어내지 않고, 대신 그래서 어떻게 되는가를 말한다 —
		// 잡히는 駒 하나와, 상대의 최선수 뒤에 갈라지는 세 갈래다(journal §54).
		//
		// 여기만 수를 적을 수 있다. 다른 카테고리는 이유를 이름으로 대므로 수가 필요
		// 없고, 이 갈래들이 서는 국면은 되물러서 이미 사라졌다 — 「지금 어떻게 두라」가
		// 되지 않는다.
		u.Known = true
		u.Threatened = f.Threatened
		u.OpponentBest, u.Branches = f.OpponentBest, f.Branches

	default:
		// missed_mate · shallow_trap · unpromoted · idle_check · king_exposed.
		// 카테고리 자체가 이미 구체적이라 붙일 사실이 없다. Known 을 세우지 않는 것이
		// 곧 「이 문장은 국면을 안 짚는다」이고, 그러면 카테고리 문구가 그대로 나간다.
	}
	return u
}
