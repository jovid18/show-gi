package game

import (
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/tag"
)

// 手筋의 **이름은 룰(tag)이 정하고 이득은 엔진이 정한다** — 이 파일이 그 AND다.
//
// `tag` 가 엔진을 모른 채 이름만 내는 것은(엔진 없이 테스트가 돈다) `intervene` 이 엔진을
// 모르는 것과 같은 구조이고, 이름 없는 수를 안 알리는 잠금이
// [01-core.md §7.1](../../../../docs/01-core.md), 나눈 이유가
// [09-tags.md §5](../../../../docs/09-tags.md)다.

// TesujiLossCp 는 手筋이라고 부를 수 있는 **손해의 상한**(cp)이다. 넘으면 엔진이 잃는 수로 본
// 것이고, 거기 이름을 붙이면 개입 쪽과 화면이 서로 반대를 가르친다.
// 0이 안 되는 이유·100인 근거·**[미확정]** 은 06-status.md §34 ③.
const TesujiLossCp = 100

// NamedTesuji 는 그 수가 **새로 만들고 엔진이 값을 인정한** 手筋의 이름이다.
// 두 cp는 **先手 관점**이다(`edges.eval_by_depth` 와 같은 규약).
// 세션 밖(archive·whatif)도 **같은 함수를 지나야** 대국 중과 기록의 이름이 갈리지 않는다.
func NamedTesuji(before, after shogi.Position, c shogi.Color, lastUSI string, senteCpBefore, senteCpAfter int) []tag.Tag {
	return namedTesuji(before, after, c, lastUSI, Judgement{
		SenteCpBefore: senteCpBefore,
		SenteCpAfter:  senteCpAfter,
		HasEvals:      true,
	})
}

// namedTesuji 는 게이트(엔진)와 이름(룰)의 AND다. **판에 서 있는 것 전부가 아니라 그 수가 새로
// 만든 것만** 통과시킨다 — 안 그러면 게이트가 없는 것과 같다(06-status.md §34 ⑦).
// **모르면 이름을 붙이지 않는다** — 평가치가 없으면 빈 결과다.
//
// j 의 두 평가치가 after 의 것이라, 부르는 쪽은 결과를 **국면 세대와 함께** 들고 있어야
// 한다(state.tesujiGen) — 낡은 평가치로 이름을 붙이는 것이 이 게이트를 없애는 것과 같다.
func namedTesuji(before, after shogi.Position, c shogi.Color, lastUSI string, j Judgement) []tag.Tag {
	if !enginePaidOff(j, c) {
		return nil
	}
	return freshTesuji(before, after, c, lastUSI)
}

// freshTesuji 는 그 수가 **새로 만든** 手筋의 이름이다 — 엔진은 묻지 않는다.
//
// 이름으로 견준다. 같은 이름이 다른 자리에 하나 더 생긴 것은 새 소식이 아니고, 화면이
// 어차피 이름 단위로 한 번만 띄운다.
//
// 打つ 手筋은 견줄 것이 없다. 방금 놓인 駒에만 붙는 이름이라 언제나 새것이고, 그래서
// `FindTesuji` 가 내지 않는다(`tag/drop.go`).
func freshTesuji(before, after shogi.Position, c shogi.Color, lastUSI string) []tag.Tag {
	had := map[string]bool{}
	for _, t := range tag.FindTesuji(before, c) {
		had[t.Code] = true
	}

	var out []tag.Tag
	for _, t := range tag.FindTesuji(after, c) {
		if !had[t.Code] {
			out = append(out, t)
		}
	}

	// 여기 오는 수는 이미 룰 엔진이 검증한 것이다. 그래도 파싱 실패를 **없는 것으로**
	// 넘기는 이유는, 이름 하나가 안 뜨는 것과 대국이 서는 것의 값이 다르기 때문이다.
	last, err := shogi.ParseUSIMove(lastUSI)
	if err != nil {
		return out
	}
	return append(out, tag.DropTesuji(after, last, c)...)
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

// cpFor 는 **先手 관점** cp를 그 색 관점으로 되돌린다(`senteCp` 의 역).
// 이름을 따로 두는 이유는 **방향이 안 읽히면 부호 버그가 조용히 남기 때문**이다 — 後手로 잡은
// 판에서만 手筋이 반대로 뜨고, 에러가 나지 않는다.
func cpFor(cp int, c shogi.Color) int {
	if c == shogi.Black {
		return cp
	}
	return -cp
}
