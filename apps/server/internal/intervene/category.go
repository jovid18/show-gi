package intervene

// Category 는 블런더가 **왜** 나쁜가다. DB의 interventions.category 에 그대로 들어간다.
//
// 엔진은 「−800점」까지만 말한다. 그 숫자를 「이 駒는 그냥 잡힙니다」로 바꾸는 것이
// 여기이고, 그래야 약점 프로파일이 성립한다(01-core.md §3·§5).
//
// **전부 결정적 룰이다.** LLM에게 「이 수 어때?」를 묻지 않는다 — 초심자는 설명이
// 틀려도 검증할 능력이 없어 틀린 것을 그대로 배운다. LLM은 여기서 정해진 카테고리를
// 문장으로 바꾸는 일만 한다(D5).
type Category string

const (
	// CategoryNone 은 개입하지 않았을 때다.
	CategoryNone Category = ""

	// CategoryMissedMate 는 詰み을 놓친 것이다. 종반에는 승률이 포화해 낙폭이
	// 판정력을 잃으므로, 이것만이 유일한 신호다(§2).
	CategoryMissedMate Category = "missed_mate"

	// CategoryLetsMate 는 **그 수로 내 玉이 詰まされる** 것이다. `missed_mate` 의 거울상이다.
	//
	// 종반의 어휘는 駒得이 아니라 **速度**다(「終盤は駒の損得より速度」). 그 速度를 세는
	// 단위가 手スキ이고 0手スキ가 詰み이라, 여기가 그 끝이다 — 아래 분기들이 보는 중반의
	// 모양(タダ捨て·駒得·王手·玉の薄さ)으로는 이 국면을 부를 말이 없다.
	//
	// **이것이 없어서 `other` 의 43%가 이유를 못 댔다**(06-status.md §40). 재 보니 `other`
	// 54건 중 23건이 이 국면이었고, 한 국면(game 47의 133手째)에서는 플레이어가 12수를
	// 시도해 12번 전부 詰まされる 수였는데 화면은 12번 연속 「大きな形勢損」이라고 말했다.
	CategoryLetsMate Category = "lets_mate"

	// CategoryHangsPiece 는 タダ捨て다. 놓은 칸을 상대가 노리는데 내가 안 지킨다.
	CategoryHangsPiece Category = "hangs_piece"

	// CategoryShallowTrap 은 「얕은 이득에 낚임」이다. 얕게 보면 이득인데 깊게 보면 손해.
	//
	// **이 목록에서 가장 교육적인 카테고리다.** 나머지는 「보면 알 수 있었던 것을 못 봤다」
	// 이지만 이것만은 초심자가 얕게 읽기 때문에 필연적으로 빠지는 함정이다.
	// 제안형 힌트의 「깊이 반전형」(§7.1)과 정확한 거울상이다.
	CategoryShallowTrap Category = "shallow_trap"

	// CategoryUnpromoted 는 **成하지 않은 것**이 문제인 수다. 이동은 최선수와 같다.
	//
	// 다른 카테고리보다 앞에 온다. 이동이 최선수와 같다면 나쁜 이유는 成 여부뿐이고,
	// 다른 무엇을 이유로 대면 그 설명이 **틀린다** — 실제로 「잡는 것이 문제」라고
	// 가르쳤다(08-playtest.md §8).
	CategoryUnpromoted Category = "unpromoted"

	// CategoryGreedyCapture 는 駒得에 눈이 멀어 대가를 못 본 것이다.
	CategoryGreedyCapture Category = "greedy_capture"

	// CategoryIdleCheck 는 追う手 — 이어지지 않는 王手다.
	CategoryIdleCheck Category = "idle_check"

	// CategoryKingExposed 는 玉 주변의 수비를 방치한 것이다.
	CategoryKingExposed Category = "king_exposed"

	// CategoryOther 는 미분류다.
	//
	// **반드시 있어야 한다.** 억지로 끼워 맞추면 설명이 틀리고, 그게 이 제품에서 가장
	// 큰 실패다. 판을 못 읽었을 때도 여기로 떨어진다 — 모른다고 말하는 편이 낫다.
	CategoryOther Category = "other"
)

// Features 는 카테고리를 정하는 데 쓰는 **국면의 사실들**이다.
//
// 전부 이미 구해진 숫자와 참거짓이다. 판도 엔진도 여기 들어오지 않는다 — 그래야
// 임계치를 흔들어 보는 데 국면을 만들 필요가 없고, 이 패키지가 엔진을 모른다는
// 성질이 카테고리에도 그대로 남는다(CLAUDE.md).
//
// 뽑아 오는 쪽은 game.MoveFeatures 다.
type Features struct {
	// Known 이 false면 사실을 못 구한 것이다 — CategoryOther 로 떨어진다.
	// 지어내지 않는다.
	Known bool

	// LandsAttacked 는 상대가 그 칸의 駒를 **실제로 딸 수 있는가**(합법수로).
	//
	// 「노리고 있는가」가 아니다. 핀에 묶여 못 움직이는 駒는 노리기만 할 뿐이고,
	// 그걸 잡힌다고 말하면 화면의 단언이 거짓이 된다.
	LandsAttacked bool
	// LandsDefended 는 따인 뒤에 **되딸 수 있는가**.
	//
	// 상대는 되따이지 않는 쪽으로 딸 것이므로, 되딸 수 없는 따는 수가 하나라도 있으면
	// false 다.
	LandsDefended bool

	// MovedValue 는 그 칸에 놓인 내 駒의 가치다(성했으면 성한 값).
	MovedValue int
	// CapturedValue 는 이 수로 딴 駒의 가치. 안 땄으면 0.
	CapturedValue int

	// GivesCheck 는 이 수가 王手인가.
	GivesCheck bool

	// UnpromotedOnly 는 **최선수와 같은 이동인데 成하지 않은** 수인가.
	//
	// 이 한 칸이 없어서 `▲同銀不成` 이 greedy_capture 로 분류되어 「잡는 것이
	// 문제」라고 가르쳤다 — 잡는 것이 정답이었다(08-playtest.md §8). 뽑아 오는 쪽은
	// game.UnpromotedOnly 이고, 여기로는 참거짓만 온다.
	UnpromotedOnly bool

	// ShieldLoss 는 내 玉 주변 8칸의 **내 방어 利き** 감소량. 늘었으면 음수다.
	ShieldLoss int
	// ThreatGain 은 같은 칸들의 **상대 공격 利き** 증가량. 줄었으면 음수다.
	ThreatGain int

	// ShallowCp 는 착수 후 국면의 얕은 평가(둔 쪽 관점). HasShallow 가 false면 없는 값이다.
	ShallowCp  int
	HasShallow bool

	// OpponentMatePlies 는 이 수 뒤에 **상대가 내 玉을 詰ます 手数**다. 없으면 0.
	//
	// **채우는 쪽이 두 조건을 다 확인한 뒤에 넣는다**(game.engineAnalyst):
	//
	//	① 이 수 뒤에 상대의 詰み이 **증명**됐다 (`go mate` 의 checkmate. 탐색의 mate 점수가
	//	   아니다 — 실측에서 둘이 어긋났고 탐색 쪽은 증명이 아니다)
	//	② **최선수 뒤에는 그 詰み이 없다**
	//
	// **②가 없으면 이 값은 거짓말이 된다.** 이미 詰んでいた 국면에 「この手で詰まされます」를
	// 내보내면 그 수의 죄가 아닌 것을 그 수의 죄라고 가르치는 것이고, 초심자는 검증할 수단이
	// 없어 그대로 배운다. 실측에서는 23건 전부가 ②를 통과했지만(06-status.md §40) **공짜로
	// 통과한 것이 아니라 그 조건이 통과시킨 것이다.**
	//
	// 여기로는 手数만 온다. 판도 엔진도 이 패키지에 들어오지 않는다.
	OpponentMatePlies int
}

// HangsPiece 는 놓인 駒를 그냥 내주는가다.
//
// **두 곳이 같은 정의를 쓴다.** 개입의 タダ捨て 판정이자 적응형 상대의 「던지지 않는다」
// 필터다(01-core.md §6). 정의가 갈리면 화면이 「その駒は取り返せない場所に置かれています」
// 라고 가르쳐놓고 컴퓨터가 바로 그 수를 두는 일이 생긴다 — 그 순간 배운 것이 무너진다.
func (f Features) HangsPiece() bool {
	return f.Known && f.LandsAttacked && !f.LandsDefended && f.MovedValue > f.CapturedValue
}

// ShallowTrapCp 는 「얕게 보면 이득」과 「깊게 보면 손해」 사이의 최소 반전 폭이다.
//
// 이만큼 안 벌어지면 그냥 평가가 흔들린 것이지 함정이 아니다. 제안형의 reversal
// 임계치와 같은 축이라 같은 값에서 출발한다.
//
// **300에 둔다. 살아 있다는 것까지만 확인됐다**(06-status.md §39) — 재채점한 개입
// 35건 중 8건(22.9%)이 이 카테고리라 값이 너무 높아 죽어 있지는 않다. 다만 **얕은
// 평가(`ShallowCp`)가 기보에 안 남아** 반전 폭 자체를 다시 못 구한다. 300을 400으로
// 올리면 몇 건이 빠지는지는 기록으로 답이 안 나오고, 엔진을 다시 돌려야 나온다.
// **[미확정: 폭 자체는 아직 못 쟀다]**
const ShallowTrapCp = 300

// classify 는 개입하기로 정해진 수의 이유를 고른다.
//
// **순서가 규칙의 일부다.** 한 수가 여러 조건에 걸리는 것은 흔하고(딴 駒가 있는데
// 玉도 열렸다 같은), 그때 무엇을 말해줄지가 곧 제품의 판단이다. 위에서부터
// **더 구체적이고 눈으로 확인할 수 있는 것**을 앞에 둔다 — 초심자가 판을 보고
// 「아 정말 그렇네」까지 갈 수 있어야 배운 것이 된다.
func classify(in Input, lostMate bool) Category {
	if lostMate {
		return CategoryMissedMate
	}
	f := in.Features
	if !f.Known {
		return CategoryOther
	}

	switch {
	// **成 여부만 다른 수가 맨 앞이다.** 이동이 최선수와 같으면 나쁜 이유는 그것뿐이라,
	// 아래 어느 분기로 보내도 설명이 틀린다. タダ捨て보다도 앞인 것은, 不成으로 놓인 駒가
	// 마침 잡히는 자리여도 배울 것은 「成れた」쪽이기 때문이다.
	case f.UnpromotedOnly:
		return CategoryUnpromoted

	// **詰まされる 것이 그다음이고, 나머지 전부보다 앞이다.** 「終盤は駒の損得より速度」가
	// 그대로 순서다 — 玉이 죽는 국면에서 「駒を取られます」를 말하면 초심자는 駒를 지키고
	// 그 다음 수에 詰まされる. 실제로 그 모양이 기록에 있다(06-status.md §40).
	//
	// `unpromoted` 뒤인 것은 그쪽이 「이동은 맞았다」는 더 좁은 사실이기 때문이다. 이동이
	// 최선수와 같은데 成만 안 한 수라면 고칠 것은 成이고, 그것이 詰み까지 막는다.
	case f.OpponentMatePlies > 0:
		return CategoryLetsMate

	// タダ捨て가 그다음이다. 그냥 잡히는 駒는 판에서 그대로 보이고,
	// 딴 것보다 잃는 것이 클 때만 걸리므로 정당한 駒交換은 여기 안 들어온다.
	case f.HangsPiece():
		return CategoryHangsPiece

	// 얕은 이득에 낚임. 위에서 안 걸렸다는 것은 놓인 駒가 그냥 잡히지는 않는다는
	// 뜻이라, 여기 남는 것은 「한 수 앞은 좋아 보이는」 부류다.
	case f.HasShallow && f.ShallowCp > 0 && in.AfterCp < 0 &&
		f.ShallowCp-in.AfterCp >= ShallowTrapCp:
		return CategoryShallowTrap

	// 駒는 땄는데 형세가 나빠졌다. 「손해가 아닌데 왜?」가 그대로 설명이 된다.
	//
	// **딴 것만으로는 부족하다.** 다른 쪽에서 벌어진 일 때문에 나쁜 수인데 마침 歩를
	// 하나 공짜로 땄다면, 딴 것은 이유가 아니다 — 그걸 이유라고 말하면 틀린 설명이 된다.
	// 그래서 딴 대가가 실제로 보이는 두 경우로 좁힌다: **되따일 수 있거나**(위에서
	// 안 걸렸으니 교환 자체는 유리해 보이는 쪽이다) **玉이 더 밀리거나**.
	// 카테고리 이름의 「옥 안전 무시」가 뒤쪽이다.
	case f.CapturedValue > 0 && (f.LandsAttacked || f.ThreatGain > 0):
		return CategoryGreedyCapture

	// 追う手. 딴 것도 없이 王手만 걸었다 — 위에서 CapturedValue > 0 이 이미 빠졌다.
	//
	// 원안은 「후속 詰めろ가 없을 것」을 조건에 뒀는데, 詰めろ 판정은 지금 없다.
	// 대신 **이득이 없는 王手**로 좁혔다 — 개입이 걸릴 만큼 형세를 해친 王手는
	// 이어지지 않았다는 뜻이므로 억지가 아니다. **[미확정: 詰めろ 판정이 붙으면 좁힌다]**
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
