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
	KindOpening   Kind = "opening"   // 戦型 — 판 전체의 상태 (角換わり 등)
)

// 세 축은 **정의 방식이 다르고, 그 차이가 입력을 정한다.**
//
//	囲い    상태 — 지금 판의 배치. 깨지면 진짜로 그 囲い가 아니다
//	戦法    수순 — 「飛를 그 筋으로 振った」. 한 번 일어나면 그 판 내내 참이다
//	戦型    상태 — 角の有無처럼 판 전체를 보는 조건. 상대 쪽까지 본다
//
// 셋을 한 함수에 뭉치면 어느 것이 국면에서 오고 어느 것이 수순에서 오는지가 흐려진다.
// 실제로 처음에는 전법을 국면에서 읽었고, 그래서 飛를 올린 순간 이름이 꺼졌다.

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

// formationByFile 는 飛를 振った 筋으로 전법을 정한다. 키는 **先手 기준 筋**이다.
//
// **좌표만으로 결정적이지 않은 전법은 여기 넣지 않는다** — 棒銀·藤井システム은 배치가
// 아니라 수순 사전으로 정의되므로, 筋으로 흉내내면 틀린 이름을 가르친다([09-tags.md](../../../../docs/09-tags.md)).
//
// **段을 안 본다.** 처음에는 `{6, 8, Rook}` 처럼 칸으로 적었는데, 출처가 전부 筋으로
// 말하고 있었다 — 袖飛車는 「先手ならば飛車を3筋に」이고 ▲3八飛에서 ▲3五飛까지 段이
// 움직인다. 段을 고정하면 四間飛車를 짜고 飛를 6五로 올린 순간 이름이 꺼진다.
// 전법은 그대로인데 라벨만 사라지는 것이라, 그건 잡고 있던 것이 전법이 아니라
// **전법을 짜는 순간**이었다는 뜻이다.
//
// 그리고 筋만 보는 것으로 바꾸면 국면이 아니라 **수순**을 봐야 한다. 국면에서 「飛가
// 6筋에 있다」를 물으면 종반에 우연히 6筋을 지나가는 飛까지 四間飛車가 된다.
// 振り飛車의 정의는 「飛를 그 筋으로 振った」이고, 그건 한 번 일어나면 그 판 내내
// 참인 **수순의 사실**이다. 将棋ウォーズ가 그 수를 둔 순간에 이름을 주는 이유이기도 하다.
var formationByFile = map[int]Tag{
	3: {Code: "sode_bisha", NameJa: "袖飛車", Kind: KindFormation},
	4: {Code: "migi_shiken_bisha", NameJa: "右四間飛車", Kind: KindFormation},
	5: {Code: "naka_bisha", NameJa: "中飛車", Kind: KindFormation},
	6: {Code: "shiken_bisha", NameJa: "四間飛車", Kind: KindFormation},
	7: {Code: "sanken_bisha", NameJa: "三間飛車", Kind: KindFormation},
	8: {Code: "mukai_bisha", NameJa: "向かい飛車", Kind: KindFormation},
}

// ibisha 는 「飛를 끝까지 振らなかった」쪽이다.
//
// **없음과 구별해야 하므로 조건이 하나 더 붙는다.** 振っていない은 初期配置에서도
// 참이라, 그대로 태그하면 첫 수 전에 「居飛車」가 뜬다 — 플레이어가 아직 하지 않은
// 선택에 이름을 붙이는 것이다. 그래서 **囲いが組めている** 것을 함께 요구한다:
// 「玉を囲ったのに飛車は振っていない」은 그 자체로 드러난 선택이다.
var ibisha = Tag{Code: "ibisha", NameJa: "居飛車", Kind: KindFormation}

// rookStartFile 는 平手에서 그 색의 飛가 서 있는 筋이다 (先手 2八 · 後手 8二).
func rookStartFile(c shogi.Color) int {
	if c == shogi.Black {
		return 2
	}
	return 8
}

// senteFile 는 그 색의 筋을 先手 기준으로 옮긴다. formationByFile 의 키가 先手 기준이다.
func senteFile(file int, c shogi.Color) int {
	if c == shogi.Black {
		return file
	}
	return 10 - file
}

// DetectFormation 은 **플레이어 자신의 수만** 순서대로 받아 전법을 읽는다.
//
// 飛를 좇다가 **처음으로 筋을 바꾼 수**를 찾는다. 그 도착 筋이 곧 전법이고, 먼저
// 나온 것이 이긴다 — 振り直し(예: 四間에서 三間으로)는 그 판의 전법을 바꾸지 않는다.
//
// 飛가 잡혔다가 다시 打たれる 경우는 걸리지 않는다. 打은 `From < 0` 이라 좇던 칸과
// 절대 안 맞고, 그 뒤로는 아무것도 반환하지 않는다 — **거짓으로 붙이는 것보다 안 붙는
// 쪽이 낫다.**
func DetectFormation(playerMoves []string, c shogi.Color) (Tag, bool) {
	rook := shogi.SquareOf(rookStartFile(c), rookStartRank(c))

	for _, usi := range playerMoves {
		m, err := shogi.ParseUSIMove(usi)
		if err != nil || m.IsDrop() || int(m.From) != rook {
			continue
		}
		rook = int(m.To)

		if from, to := shogi.FileOf(int(m.From)), shogi.FileOf(int(m.To)); from != to {
			if t, ok := formationByFile[senteFile(to, c)]; ok {
				return t, true
			}
			return Tag{}, false // 1筋 등 이름이 없는 筋. 억지로 붙이지 않는다
		}
	}
	return Tag{}, false
}

func rookStartRank(c shogi.Color) int {
	if c == shogi.Black {
		return 8
	}
	return 2
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
// Input 은 태그 판정에 필요한 것 전부다.
//
// 인자를 늘리는 대신 구조체로 둔 이유는 **축마다 보는 것이 다르기 때문**이다. 戦型은
// 상대의 수순까지 봐야 하고(相振り飛車), 앞으로 붙을 手筋은 또 다른 것을 본다.
// 인자로 늘리면 호출부가 `Detect(pos, mine, theirs, c)` 처럼 순서로만 구별되는 슬라이스
// 둘을 넘기게 되고, **바꿔 넘겨도 컴파일된다.**
type Input struct {
	Pos shogi.Position
	// Color 는 태그를 붙일 쪽. 보통 플레이어다.
	Color shogi.Color
	// PlayerMoves · OpponentMoves 는 각 쪽이 둔 수만 순서대로. 물러진 수는 없다.
	PlayerMoves   []string
	OpponentMoves []string
}

func Detect(in Input) []Tag {
	var out []Tag

	castle, castled := pick(castles, in.Pos, in.Color)
	if castled {
		out = append(out, castle)
	}

	mine, swung := DetectFormation(in.PlayerMoves, in.Color)
	switch {
	case swung:
		out = append(out, mine)
	case castled && rookOnStartFile(in.Pos, in.Color):
		// 振っていない + 囲った = 居飛車. 囲い이 없으면 아직 아무 선택도 안 드러났다.
		out = append(out, ibisha)
	}

	if t, ok := detectOpening(in, mine, swung); ok {
		out = append(out, t)
	}
	return out
}

var (
	kakuGawari       = Tag{Code: "kaku_gawari", NameJa: "角換わり", Kind: KindOpening}
	aiFuribisha      = Tag{Code: "ai_furibisha", NameJa: "相振り飛車", Kind: KindOpening}
	kakukanFuribisha = Tag{Code: "kakukan_furibisha", NameJa: "角交換振り飛車", Kind: KindOpening}
)

// detectOpening 은 판 **전체**의 상태로 戦型을 정한다.
//
// 순서가 규칙의 일부다 — 좁은 것이 먼저다. 角交換振り飛車는 角換わり이면서 振り飛車라,
// 뒤에 두면 언제나 角換わり로 먼저 걸려서 영원히 안 나온다. 블런더 카테고리에서
// 판정 순서가 규칙인 것과 같은 자리다([01-core.md §3](01-core.md)).
func detectOpening(in Input, mine Tag, swung bool) (Tag, bool) {
	theirs, theySwung := DetectFormation(in.OpponentMoves, in.Color.Other())

	mineFuri := swung && isFuribisha(mine)
	theirsFuri := theySwung && isFuribisha(theirs)
	traded := bishopsTraded(in.Pos)

	switch {
	case traded && mineFuri:
		return kakukanFuribisha, true
	case mineFuri && theirsFuri:
		return aiFuribisha, true
	case traded:
		return kakuGawari, true
	}
	return Tag{}, false
}

// isFuribisha 는 그 전법이 **飛를 왼쪽으로 振った** 쪽인지. 袖飛車(3筋)·右四間飛車(4筋)는
// 飛를 옮기지만 居飛車系라 여기 안 든다 — 相振り飛車를 셀 때 그 둘을 세면 틀린다.
func isFuribisha(t Tag) bool {
	switch t.Code {
	case "naka_bisha", "shiken_bisha", "sanken_bisha", "mukai_bisha":
		return true
	}
	return false
}

// bishopsTraded 는 角交換이 끝난 상태인지 본다 — **양쪽이 角을 持ち駒로 하나씩 들고
// 있고, 판 위에는 角도 馬도 없다.**
//
// 처음에는 「판에 角이 없다」만 봤는데, **駒를 몇 개만 놓은 판에서 角換わり가 떴다.**
// 없는 것과 交換된 것을 구별하지 못한 것이고, `StartSFEN` 으로 만든 국면이 바로 그렇다.
// 交換이란 **서로 상대의 角을 딴 것**이라, 그 증거는 판의 빈자리가 아니라 持ち駒에 있다.
//
// 두 조건을 함께 보는 이유는 한쪽만으로는 각각 새기 때문이다 — 持ち駒만 보면 아직 판에
// 角이 남은 국면(한쪽이 二枚目를 든 경우)이 걸리고, 판만 보면 위의 빈 판이 걸린다.
//
// **상태로 묻는 것의 대가**는 나중에 어느 쪽이 角을 打つと 이름이 사라지는 것이다.
// 囲い이 깨지면 그 囲い가 아니게 되는 것과 같은 성질이고, 戦型을 상태 축에 둔 결과다.
func bishopsTraded(pos shogi.Position) bool {
	if pos.Hands[shogi.Black][shogi.Bishop] < 1 || pos.Hands[shogi.White][shogi.Bishop] < 1 {
		return false
	}
	for sq := range pos.Board {
		if t := pos.Board[sq].Type(); t == shogi.Bishop || t == shogi.PromBishop {
			return false
		}
	}
	return true
}

// rookOnStartFile 은 그 색의 飛(또는 龍)가 아직 처음 筋에 있는지 본다.
//
// **居飛車에만 필요한 확인이다.** 「振っていない」를 수순으로만 물으면 수순이 없을 때
// 참이 되어 버린다 — `StartSFEN` 으로 중간 국면부터 시작한 세션이 그렇고, 거기서는
// 飛가 6筋에 있는데도 居飛車라고 말한다. 振り飛車 쪽은 이 문제가 없다: 振った 수가
// 수순에 실제로 있어야 하므로, 수순이 없으면 아무 이름도 안 붙는다.
//
// 飛가 잡혀서 판에 없으면 false다. **모르는 것을 居飛車로 세지 않는다.**
func rookOnStartFile(pos shogi.Position, c shogi.Color) bool {
	file := rookStartFile(c)
	for rank := 1; rank <= 9; rank++ {
		p := pos.Board[shogi.SquareOf(file, rank)]
		if p.Color() == c && (p.Type() == shogi.Rook || p.Type() == shogi.PromRook) {
			return true
		}
	}
	return false
}

// All 은 정의된 모든 태그다. 코퍼스(`kb_chunks`)가 태그마다 항목을 갖는지 기계로
// 확인하는 데 쓴다 — 태그는 있는데 설명이 없으면 화면에 이름만 뜨고 배울 것이 없다.
func All() []Tag {
	out := make([]Tag, 0, len(castles)+len(formationByFile)+1)
	for _, sh := range castles {
		out = append(out, sh.tag)
	}
	// 筋 순서로 낸다. map 순회는 순서가 없어서 그대로 쓰면 테스트 출력이 매번 달라진다.
	for file := 1; file <= 9; file++ {
		if t, ok := formationByFile[file]; ok {
			out = append(out, t)
		}
	}
	out = append(out, ibisha)
	return append(out, kakuGawari, aiFuribisha, kakukanFuribisha)
}

// SourceOf 는 그 囲い의 좌표 출처다. 없으면 빈 문자열.
//
// **전법에는 출처가 없다.** 필수 칸을 옮겨온 것이 아니라 「飛를 그 筋으로 振った」가
// 정의 전부라서, 가리킬 서술이 없고 우리 코드가 곧 정의다.
func SourceOf(code string) string {
	for _, sh := range castles {
		if sh.tag.Code == code {
			return sh.source
		}
	}
	return ""
}
