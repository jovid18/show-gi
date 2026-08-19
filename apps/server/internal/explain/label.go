package explain

import "github.com/jovid18/show-gi/apps/server/internal/intervene"

// 카테고리를 짧은 이름으로 부르는 자리. baseMessages(render.go)가 문장이라면 이쪽은 이름이다.
//
// 화면이 코드(hangs_piece)를 일본어로 바꾸기 시작하면 어휘가 두 벌이 되고 어긋났을 때
// 어느 쪽이 맞는지 알 수 없다. 읽는 사람은 그 수가 왜 나빴는지를 아직 모르는 사람이라,
// 전문 용어가 아니라 읽으면 뜻이 통하는 말로 고른다.
var categoryNames = map[intervene.Category]string{
	intervene.CategoryMissedMate: "詰み逃し",
	// 「逃し」가 아니다. 詰み은 남아 있고 멀어졌을 뿐이라, 되짚기 목록과 총평이
	// 같은 이름으로 부르면 이긴 판에 「놓쳤다」가 남는다(journal §76).
	intervene.CategorySlowerMate:    "詰みの遠回り",
	intervene.CategoryLetsMate:      "詰まされる",
	intervene.CategoryHangsPiece:    "タダ捨て",
	intervene.CategoryShallowTrap:   "浅い得",
	intervene.CategoryUnpromoted:    "不成",
	intervene.CategoryGreedyCapture: "割に合わない取り",
	intervene.CategoryIdleCheck:     "追う手",
	intervene.CategoryKingExposed:   "玉が薄い",
	intervene.CategoryOther:         "大きな形勢損",
}

// CategoryJa 는 카테고리의 짧은 일본어 이름이다. 모르는 값이면 미분류와 같이 부른다.
//
// 빈 문자열은 빈 문자열로 돌려준다. 그건 「개입하지 않았다」이지 미분류가 아니다
// (CategoryNone). 이름을 붙이면 없는 개입에 이름이 생긴다.
func CategoryJa(c intervene.Category) string {
	if c == intervene.CategoryNone {
		return ""
	}
	if name, ok := categoryNames[c]; ok {
		return name
	}
	return categoryNames[intervene.CategoryOther]
}

// BaseMessage 는 카테고리만으로 나오는 결정적 문구다. 기록에는 카테고리만 남으므로
// 리뷰가 문장을 다시 만들어야 하고, 국면 사실은 남지 않아 여기 없다(지어내지 않는다).
//
// 새 문장을 짓지 않고 Render 가 사실 없이 내는 것과 같은 문장을 준다 — 두 벌이 되면
// 같은 수가 대국 중과 리뷰에서 다른 이유로 나쁜 것이 된다(journal §33).
func BaseMessage(c intervene.Category) string {
	if c == intervene.CategoryNone {
		return ""
	}
	if m, ok := baseMessages[c]; ok {
		return m
	}
	return unknownMessage
}
