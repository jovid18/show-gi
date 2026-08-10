// Package tag 는 국면에 이름을 붙인다 — 囲い와 전법.
//
// **정의는 좌표 집합이다. 해설문에서 가져오지 않는다.**
// 이 제품이 아는 쇼기는 룰과 엔진 평가치뿐이었고, 이름이 붙은 것을 하나도 몰라서
// 설명이 늘 「그 駒가 잡힙니다」 층에 머물렀다(06-status.md §5). 그 통로를 여는데,
// 2차 자료를 쓰면 저작권과 신뢰성이 동시에 걸린다(04-llm.md §4). 좌표 집합은 둘 다
// 걸리지 않는다 — 그 자리에 그 駒가 있는지만 보면 되고, 그건 룰 엔진이 답한다.
//
// 이 패키지는 엔진도 DB도 모른다. 입력은 국면 하나와 색 하나뿐이다.
//
// **좌표는 지어내지 않았다.** 각 囲い의 필수 칸은 일본어 위키백과 본문의 배치 서술에서
// 옮겼고, 출처 URL을 shapes 표에 칸마다 달아 뒀다. 판이 규칙을 틀리게 가르치는 것이
// 이 레포에서 가장 값비싼 버그이고(06-status.md §22), 囲い 이름은 화면에 그대로 나가는
// 단언이라 근거 없이 적으면 안 된다.
package tag

import "github.com/jovid18/show-gi/apps/server/internal/shogi"

// Kind 는 태그의 축이다. 囲い와 전법은 **동시에 성립한다** — 「四間飛車 + 本美濃囲い」가
// 한 국면의 정상적인 상태다. 그래서 하나를 고르는 것이 아니라 축마다 하나씩 고른다.
type Kind string

const (
	KindCastle    Kind = "castle"    // 囲い — 玉 주변의 배치
	KindFormation Kind = "formation" // 戦法 — 飛의 筋
)

// Tag 는 붙은 이름 하나다.
//
// Code 와 NameJa 를 가르는 이유는 **가는 곳이 다르기 때문**이다. Code 는 검색 키라서
// (`kb_chunks.tags` · `edges.tags`) 영어이고 안 바뀌어야 한다. NameJa 는 화면에 그대로
// 나가는 문자열이라 일본어여야 한다(CLAUDE.md 언어 규칙).
type Tag struct {
	Code   string `json:"code"`
	NameJa string `json:"nameJa"`
	Kind   Kind   `json:"kind"`
}

// square 는 필수 칸 하나다. 좌표는 **先手 기준**으로만 적는다 — 後手는 파생시킨다.
type square struct {
	file, rank int
	pt         shogi.PieceType
}

// shape 는 이름 하나의 정의다.
//
// squares 가 **전부** 맞아야 성립한다. 부분 일치를 허용하지 않는 이유는 이름이 화면에
// 나가는 단언이기 때문이다 — 절반만 맞은 형태를 「本美濃囲い」라고 부르면 초심자는
// 그것을 本美濃라고 배우고, 검증할 수단이 없다. 대신 **더 느슨한 형태를 따로 정의한다**
// (片美濃가 本美濃의 느슨한 판이 아니라 그 자체로 이름이 있는 형태인 것처럼).
type shape struct {
	tag     Tag
	squares []square
	// source 는 이 좌표를 옮겨온 곳이다. kb_chunks.source_url 에 그대로 들어간다.
	source string
}

const (
	wikiMino   = "https://ja.wikipedia.org/wiki/美濃囲い"
	wikiYagura = "https://ja.wikipedia.org/wiki/矢倉囲い"
	wikiFune   = "https://ja.wikipedia.org/wiki/舟囲い"
	wikiGinkan = "https://ja.wikipedia.org/wiki/銀冠"
	wikiMusou  = "https://ja.wikipedia.org/wiki/金無双"
)

// castles 는 囲い 정의다. 순서는 상관없다 — 고르는 것은 **필수 칸이 가장 많은 쪽**이고
// (matchCastle), 그래야 本美濃가 성립하는 국면에서 片美濃라고 말하지 않는다.
var castles = []shape{
	{
		// 「玉を2八、右金を4八、銀を3八」 — 左金がない形
		tag:     Tag{Code: "kata_mino", NameJa: "片美濃囲い", Kind: KindCastle},
		squares: []square{{2, 8, shogi.King}, {3, 8, shogi.Silver}, {4, 8, shogi.Gold}},
		source:  wikiMino,
	},
	{
		// 片美濃 + 左金5八
		tag:     Tag{Code: "hon_mino", NameJa: "本美濃囲い", Kind: KindCastle},
		squares: []square{{2, 8, shogi.King}, {3, 8, shogi.Silver}, {4, 8, shogi.Gold}, {5, 8, shogi.Gold}},
		source:  wikiMino,
	},
	{
		// 本美濃の左金を4七へ進めた形
		tag:     Tag{Code: "taka_mino", NameJa: "高美濃囲い", Kind: KindCastle},
		squares: []square{{2, 8, shogi.King}, {3, 8, shogi.Silver}, {4, 8, shogi.Gold}, {4, 7, shogi.Gold}},
		source:  wikiMino,
	},
	{
		// 「銀を2七の位置へ、右金を3八の位置へ進めると銀冠」
		tag:     Tag{Code: "ginkanmuri", NameJa: "銀冠", Kind: KindCastle},
		squares: []square{{2, 8, shogi.King}, {2, 7, shogi.Silver}, {3, 8, shogi.Gold}},
		source:  wikiGinkan,
	},
	{
		// 「玉を8八に、左金を7八、右金を6七に、左銀を7七に移動させたもの」
		tag:     Tag{Code: "kin_yagura", NameJa: "金矢倉", Kind: KindCastle},
		squares: []square{{8, 8, shogi.King}, {7, 8, shogi.Gold}, {6, 7, shogi.Gold}, {7, 7, shogi.Silver}},
		source:  wikiYagura,
	},
	{
		// 金矢倉の右金が銀に置き換わった形
		tag:     Tag{Code: "gin_yagura", NameJa: "銀矢倉", Kind: KindCastle},
		squares: []square{{8, 8, shogi.King}, {7, 8, shogi.Gold}, {6, 7, shogi.Silver}, {7, 7, shogi.Silver}},
		source:  wikiYagura,
	},
	{
		// 片矢倉(天野矢倉)
		tag:     Tag{Code: "kata_yagura", NameJa: "片矢倉", Kind: KindCastle},
		squares: []square{{7, 8, shogi.King}, {6, 8, shogi.Gold}, {7, 7, shogi.Silver}, {6, 7, shogi.Gold}},
		source:  wikiYagura,
	},
	{
		// 「玉を3八に、左金を5八に、右金を4八に動かして作られる」
		//
		// 銀은 넣지 않았다. 원문이 右銀2八을 「ただし…壁銀となり玉の逃げ道がなくなって
		// しまう」로 적어 **필수가 아니라 선택**임을 밝히고 있다. 필수 칸에 넣으면
		// 銀을 안 올린 정상적인 金無双에서 이름이 안 뜬다.
		tag:     Tag{Code: "kin_musou", NameJa: "金無双", Kind: KindCastle},
		squares: []square{{3, 8, shogi.King}, {5, 8, shogi.Gold}, {4, 8, shogi.Gold}},
		source:  wikiMusou,
	},
	{
		// 「8八角、7八玉、7九銀、6九金、5八金、4八銀型である」
		//
		// 角8八을 필수 칸에 넣었다. 원문이 배치의 일부로 적고 있고, 舟囲い는 角道를
		// 막은 채로 짜는 형태라 角이 8八에 있는 것이 이 囲い의 조건이다.
		tag: Tag{Code: "fune", NameJa: "舟囲い", Kind: KindCastle},
		squares: []square{
			{8, 8, shogi.Bishop}, {7, 8, shogi.King}, {7, 9, shogi.Silver},
			{6, 9, shogi.Gold}, {5, 8, shogi.Gold}, {4, 8, shogi.Silver},
		},
		source: wikiFune,
	},
}

// formations 는 전법 정의다 — **飛의 筋 하나로 정해진다.**
//
// 囲い처럼 여러 칸을 요구하지 않는 이유는 전법의 정의가 실제로 그렇기 때문이다.
// 四間飛車는 「飛が6八にある」이 곧 정의이고, 거기까지 가는 수순은 여러 가지다.
// 반대로 **좌표만으로 결정적이지 않은 전법은 여기 넣지 않는다** — 棒銀·藤井システム은
// 배치가 아니라 수순으로 정의되므로, 좌표로 흉내내면 틀린 이름을 가르친다.
// **居飛車는 여기 없다.** 정의가 「飛が2八のまま」인데 그 칸은 初期配置라서, 넣으면
// 첫 수를 두기도 전에 「居飛車」가 뜬다 — 플레이어가 아직 하지 않은 선택에 이름을
// 붙이는 것이다. 振り飛車 넷은 전부 飛를 실제로 옮겨야 성립하므로 그 문제가 없다.
// 居飛車를 태그하려면 「飛를 안 옮긴 채 玉을 囲った」처럼 **선택이 드러난 뒤**를
// 조건으로 해야 하고, 그건 좌표 하나로 안 된다.
var formations = []shape{
	{tag: Tag{Code: "naka_bisha", NameJa: "中飛車", Kind: KindFormation}, squares: []square{{5, 8, shogi.Rook}}},
	{tag: Tag{Code: "shiken_bisha", NameJa: "四間飛車", Kind: KindFormation}, squares: []square{{6, 8, shogi.Rook}}},
	{tag: Tag{Code: "sanken_bisha", NameJa: "三間飛車", Kind: KindFormation}, squares: []square{{7, 8, shogi.Rook}}},
	{tag: Tag{Code: "mukai_bisha", NameJa: "向かい飛車", Kind: KindFormation}, squares: []square{{8, 8, shogi.Rook}}},
}

// squareFor 는 先手 좌표를 그 색의 좌표로 옮긴다.
//
// **後手 정의를 손으로 두 벌 적지 않는다.** 두 벌이면 한쪽만 고치는 버그가 나고,
// 그 버그는 에러를 내지 않는다 — 後手 국면에서 태그가 조용히 안 뜨는 것으로만 보인다.
// 後手 진영은 180° 회전이므로 筋도 段도 함께 뒤집는다.
func squareFor(s square, c shogi.Color) int {
	if c == shogi.Black {
		return shogi.SquareOf(s.file, s.rank)
	}
	return shogi.SquareOf(10-s.file, 10-s.rank)
}

func (sh shape) matches(pos shogi.Position, c shogi.Color) bool {
	for _, s := range sh.squares {
		if pos.Board[squareFor(s, c)] != shogi.MakePiece(s.pt, c) {
			return false
		}
	}
	return true
}

// pick 은 맞는 것 중 **필수 칸이 가장 많은 것**을 고른다.
//
// 판정 순서가 규칙의 일부인 것은 블런더 카테고리와 같다(01-core.md §3). 여기서는
// 순서가 아니라 구체성으로 정하는데, 그래야 정의를 추가할 때 표의 순서를 신경 쓰지
// 않아도 된다 — 本美濃는 片美濃의 칸을 모두 포함하므로 **항상** 本美濃가 이긴다.
func pick(shapes []shape, pos shogi.Position, c shogi.Color) (Tag, bool) {
	best := -1
	var found Tag
	for _, sh := range shapes {
		if len(sh.squares) > best && sh.matches(pos, c) {
			best, found = len(sh.squares), sh.tag
		}
	}
	return found, best >= 0
}

// Detect 는 이 색이 지금 짜고 있는 이름들을 돌려준다. 없으면 빈 슬라이스다.
//
// 축마다 최대 하나이고 囲い가 먼저 온다 — 화면이 순서를 다시 정하지 않아도 되게.
//
// **호출하는 쪽이 플레이어 색만 넘긴다.** 컴퓨터 쪽 태그는 화면에 그리지 않는다
// (01-core.md §7 — 상대의 계획을 알려주지 않는다). 그 규칙을 이 함수가 강제하지 않는
// 이유는, 리뷰 화면이 끝난 판을 양쪽 다 보여주는 자리에서는 반대가 맞기 때문이다.
func Detect(pos shogi.Position, c shogi.Color) []Tag {
	var out []Tag
	if t, ok := pick(castles, pos, c); ok {
		out = append(out, t)
	}
	if t, ok := pick(formations, pos, c); ok {
		out = append(out, t)
	}
	return out
}

// All 은 정의된 모든 태그다. 코퍼스(`kb_chunks`)가 태그마다 항목을 갖는지 기계로
// 확인하는 데 쓴다 — 태그는 있는데 설명이 없으면 화면에 이름만 뜨고 배울 것이 없다.
func All() []Tag {
	out := make([]Tag, 0, len(castles)+len(formations))
	for _, sh := range castles {
		out = append(out, sh.tag)
	}
	for _, sh := range formations {
		out = append(out, sh.tag)
	}
	return out
}

// SourceOf 는 그 태그의 좌표 출처다. 없는 태그면 빈 문자열.
//
// 전법은 출처가 없다 — 飛의 筋 하나라서 옮겨올 서술이 없고, 정의가 곧 좌표다.
func SourceOf(code string) string {
	for _, sh := range append(append([]shape{}, castles...), formations...) {
		if sh.tag.Code == code {
			return sh.source
		}
	}
	return ""
}
