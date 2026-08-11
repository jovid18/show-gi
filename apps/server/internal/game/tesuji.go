package game

import (
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/tag"
)

// 手筋의 **이름은 룰이 정하고, 이득은 엔진이 정한다** — 여기가 그 AND다.
//
// 나눈 이유는 [09-tags.md §5](../../../../docs/09-tags.md)에 있고, 요점은 두 질문의
// 성질이 다르다는 것이다.
//
//	형태와 이름  「桂가 둘을 노린다」→ ふんどしの桂. 국면만 보면 결정적으로 나온다
//	이득인가     「그래서 得인가」. 미래를 읽는 일이고, 엔진이 이미 매 수 하고 있다
//
// **엔진이 이름을 정하지 않는다.** [01-core.md §7.1](../../../../docs/01-core.md)의 잠금
// ①이 「이름이 있을 것」이고, 엔진은 `+300cp` 까지만 말할 수 있다. 그래서 `tag` 는 엔진을
// 모른 채 이름만 내고(엔진 없이 테스트가 돈다), AND는 둘 다 손에 든 이 패키지에서 걸린다 —
// `intervene` 이 엔진을 모르고 이미 구해진 평가치만 받는 것과 같은 구조다.

// TesujiLossCp 는 手筋이라고 부를 수 있는 **손해의 상한**(cp)이다.
//
// 개입 판정이 이미 구해 온 두 값(착수 전 국면의 최선 · 착수 후 국면)의 차를 그대로 쓴다.
// 그 차가 이 값을 넘으면 엔진은 그 수를 **잃는 수**로 본 것이고, 그런 형태에 手筋이라는
// 이름을 붙이면 화면이 서로 반대를 가르친다 — 같은 국면을 개입 쪽은 블런더라고 한다.
//
// **0으로 두면 안 된다.** 실측에서 두 가지가 나왔다(06-status.md §34).
//
//	엔진 최선수를 그대로 둬도 낙폭이 0이 아니다   실전 11개 국면에서 −30 ~ +99cp
//	같은 국면·같은 깊이가 같은 값을 안 준다        해시가 남아 ±150cp쯤 흔들린다
//
// 100인 이유는 **歩 한 장**이다. 打つ 手筋(叩きの歩·垂れ歩)은 歩 하나를 던지는 것이
// 내용이라 그 아래로는 못 내리고, 그보다 위로 올리면 角을 던져 만든 같은 형태(실측
// +440cp)와의 사이가 좁아진다. 실 기보 102수에서 형태가 생긴 6수 중 2수가 이 선을 넘지
// 않았고, **그 둘이 다 선 근처였다**(+24cp · +91cp).
//
// **경계 근처는 어느 쪽으로도 간다.** 흔들림이 이 값과 같은 크기라 그렇다. 화면에서
// 깜빡이지는 않는다 — 한 수에 한 번만 묻고 그 답을 국면과 함께 들고 있다(styleTags).
//
// **[미확정]** K·레벨 임계치와 같은 실측 큐에 있다(06-status.md §5).
const TesujiLossCp = 100

// namedTesuji 는 pos 에 걸려 있는 手筋 중 **엔진이 이득으로 본 것**의 이름을 낸다.
//
// pos 는 판정이 끝난 그 국면이어야 하고 lastUSI 는 그 국면을 만든 수다. j 의 두 평가치가
// 그 국면의 것이기 때문이고, 그래서 부르는 쪽은 결과를 **국면 세대와 함께** 들고 있어야
// 한다(state.tesujiGen) — 낡은 평가치로 이름을 붙이는 것이 이 게이트를 없애는 것과 같다.
//
// **모르면 이름을 붙이지 않는다.** 평가치가 없으면(엔진이 없거나 판을 못 읽었다) 빈
// 결과가 온다 — 룰만으로 통과시키면 지우려던 그 오판이 그대로 돌아온다.
//
// 打つ 手筋만 방금 둔 수를 따로 받는다. 판에 남지 않는 사실이라 국면만으로는 못 묻는다
// (`tag/drop.go`).
func namedTesuji(pos shogi.Position, c shogi.Color, lastUSI string, j Judgement) []tag.Tag {
	if !enginePaidOff(j, c) {
		return nil
	}
	out := tag.FindTesuji(pos, c)

	// 여기 오는 수는 이미 룰 엔진이 검증한 것이다. 그래도 파싱 실패를 **없는 것으로**
	// 넘기는 이유는, 이름 하나가 안 뜨는 것과 대국이 서는 것의 값이 다르기 때문이다.
	last, err := shogi.ParseUSIMove(lastUSI)
	if err != nil {
		return out
	}
	return append(out, tag.DropTesuji(pos, last, c)...)
}

// enginePaidOff 는 「엔진이 이 국면을 손해로 보지 않는가」다.
//
// **捨て駒를 죽이지 않는 것이 이 형태의 값이다.** 손으로 쓴 판정은 「그 駒가 잡히는가」를
// 물었고, 그러면 腹銀처럼 잡히는 것이 정상인 寄せ 手筋이 전부 탈락한다(tag/placement.go).
// 엔진은 depth 12로 읽으므로 **성립하는 捨て駒는 평가치가 떨어지지 않고**, 성립하지 않는
// 것만 떨어진다 — 조건 하나가 両取り와 寄せ를 함께 가른다.
//
// 견주는 두 값은 `intervene` 이 개입을 판정할 때 쓰는 그 둘이다. 새 축을 만들지 않는다 —
// 그쪽이 「낙폭」이라고 부르는 것을 여기서는 cp로 보고, 임계치만 다르다.
func enginePaidOff(j Judgement, c shogi.Color) bool {
	if !j.HasEvals {
		return false
	}
	before, after := cpFor(j.SenteCpBefore, c), cpFor(j.SenteCpAfter, c)
	return before-after <= TesujiLossCp
}

// cpFor 는 **先手 관점** cp를 그 색 관점으로 되돌린다.
//
// `senteCp` 의 역이고, 부호를 뒤집는 연산은 자기 자신의 역이라 하는 일이 같다. 그래도
// 이름을 따로 두는 이유는 **방향이 안 읽히면 부호 버그가 조용히 남기 때문**이다 —
// 後手로 잡은 판에서만 手筋이 반대로 뜨는 종류이고, 에러가 나지 않는다.
func cpFor(cp int, c shogi.Color) int {
	if c == shogi.Black {
		return cp
	}
	return -cp
}
