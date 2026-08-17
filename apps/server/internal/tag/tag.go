// Package tag 는 국면에 이름을 붙인다 — 囲い · 戦法 · 戦型 · 手筋.
//
// 이 제품이 아는 쇼기는 룰과 엔진 평가치뿐이었고, 이름이 붙은 것을 하나도 몰라서
// 설명이 늘 「그 駒가 잡힙니다」 층에 머물렀다(06-status.md §5). 그 통로를 여는데,
// 2차 자료를 쓰면 저작권과 신뢰성이 동시에 걸린다(09-tags.md §0).
//
// **그래서 정의를 우리가 계산할 수 있는 것으로만 적는다.** 축마다 방식이 다르고,
// 그 차이가 곧 입력이 무엇인지를 정한다 — 자세한 것은 아래 Kind 옆에 적었다.
//
// 이 패키지는 엔진도 DB도 모른다. 국면과 수순만 받는다.
//
// **좌표는 지어내지 않았다.** 囲い의 필수 칸은 일본어 위키백과 본문의 배치 서술에서
// 옮겼고 출처 URL을 정의마다 달아 뒀다. 판이 규칙을 틀리게 가르치는 것이 이 레포에서
// 가장 값비싼 버그이고(journal §22), 이름은 화면에 그대로 나가는 단언이라
// 근거 없이 적으면 안 된다. 전체 목록과 남은 것은 docs/09-tags.md 에 있다.
package tag

import "github.com/jovid18/show-gi/apps/server/internal/shogi"

// Kind 는 태그의 축이다. 축은 **동시에 성립한다** — 「四間飛車 + 本美濃囲い + 角交換振り飛車」
// 가 한 국면의 정상적인 상태다. 그래서 하나를 고르는 것이 아니라 축마다 하나씩 고른다.
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
//	手筋    관계 — 이 駒가 무엇을 동시에 노리는가. 룰 엔진에 합법수로 묻는다 (tesuji.go)
//
// 셋을 한 함수에 뭉치면 어느 것이 국면에서 오고 어느 것이 수순에서 오는지가 흐려진다.
// 실제로 처음에는 전법을 국면에서 읽었고, 그래서 飛를 올린 순간 이름이 꺼졌다.

// Tag 는 붙은 이름 하나다.
//
// Code 와 NameJa 를 가르는 이유는 **가는 곳이 다르기 때문**이다. Code 는 검색 키라서
// (`games.style_tags` · `edges.tags`) 영어이고 안 바뀌어야 한다. NameJa 는 화면에 그대로
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
	// source 는 이 좌표를 옮겨온 곳이다. 신뢰 계층은 09-tags.md §0.
	source string
}

const (
	wikiMino   = "https://ja.wikipedia.org/wiki/美濃囲い"
	wikiYagura = "https://ja.wikipedia.org/wiki/矢倉囲い"
	wikiFune   = "https://ja.wikipedia.org/wiki/舟囲い"
	wikiGinkan = "https://ja.wikipedia.org/wiki/銀冠"
	wikiMusou  = "https://ja.wikipedia.org/wiki/金無双"
	wikiKani   = "https://ja.wikipedia.org/wiki/カニ囲い"
	wikiGangi  = "https://ja.wikipedia.org/wiki/雁木囲い"
	wikiAna    = "https://ja.wikipedia.org/wiki/穴熊囲い"
	wikiHidari = "https://ja.wikipedia.org/wiki/左美濃"
	wikiMille  = "https://ja.wikipedia.org/wiki/ミレニアム囲い"
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
		// 「金銀4枚で囲っているため堅い」 — 金矢倉 + 銀5七
		tag: Tag{Code: "sou_yagura", NameJa: "総矢倉", Kind: KindCastle},
		squares: []square{
			{8, 8, shogi.King}, {7, 8, shogi.Gold}, {6, 7, shogi.Gold},
			{7, 7, shogi.Silver}, {5, 7, shogi.Silver},
		},
		source: wikiYagura,
	},
	{
		// 「左銀が7六に移れば銀立ち矢倉となる」
		tag: Tag{Code: "gindachi_yagura", NameJa: "銀立ち矢倉", Kind: KindCastle},
		squares: []square{
			{8, 8, shogi.King}, {7, 8, shogi.Gold}, {6, 7, shogi.Gold},
			{7, 7, shogi.Silver}, {7, 6, shogi.Silver},
		},
		source: wikiYagura,
	},
	{
		// 「右銀が6六の位置までくると菱矢倉となる」
		tag: Tag{Code: "hishi_yagura", NameJa: "菱矢倉", Kind: KindCastle},
		squares: []square{
			{8, 8, shogi.King}, {7, 8, shogi.Gold}, {6, 7, shogi.Gold},
			{7, 7, shogi.Silver}, {6, 6, shogi.Silver},
		},
		source: wikiYagura,
	},
	{
		// 「玉が8九に、左銀が8八にいる」
		tag: Tag{Code: "kikusui_yagura", NameJa: "菊水矢倉", Kind: KindCastle},
		squares: []square{
			{8, 9, shogi.King}, {7, 8, shogi.Gold}, {6, 7, shogi.Gold},
			{8, 8, shogi.Silver}, {7, 7, shogi.Silver},
		},
		source: wikiYagura,
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
		// 穴熊 — **여기만 출처의 성격이 다르다.** 본문에 배치 서술이 없어 좌표의 근거가
		// URL이 아니라 도달 확인 테스트다(`TestAnagumaIsReachableFromTheStart`;
		// 09-tags.md §1 · journal §30).
		tag: Tag{Code: "ibisha_anaguma", NameJa: "居飛車穴熊", Kind: KindCastle},
		squares: []square{
			{9, 9, shogi.King}, {8, 8, shogi.Silver}, {7, 9, shogi.Gold},
		},
		source: wikiAna,
	},
	{
		// 左美濃(8八玉型) — 玉8八·金7八·金6八·銀7七.
		//
		// 金矢倉와 필수 칸 수가 같아서(넷) `pick` 의 「구체적인 쪽」으로는 안 갈리는데,
		// **동시에 성립할 수 없어서 문제가 되지 않는다** — 둘 다 金7八을 요구하고 나머지
		// 金의 자리가 6八 대 6七로 갈리므로, 걸리려면 金이 세 장이어야 한다.
		//
		// 穴熊와 같은 이유로 도달 확인을 붙였다(`TestHidariMinoIsReachableFromTheStart`).
		tag: Tag{Code: "hidari_mino", NameJa: "左美濃", Kind: KindCastle},
		squares: []square{
			{8, 8, shogi.King}, {7, 8, shogi.Gold}, {6, 8, shogi.Gold}, {7, 7, shogi.Silver},
		},
		source: wikiHidari,
	},
	{
		// ミレニアム囲い(トーチカ) — 「▲8九玉。玉を深く囲う」「▲7九金。玉の脇を固める」
		//
		// **桂7七이 이 囲い를 이 囲い로 만든다.** 先手 桂는 8九에서 출발하므로, 그 桂가
		// 7七로 뛰어야 玉이 8九에 들어갈 자리가 생긴다 — 조건이 서로를 요구한다.
		// 菊水矢倉도 玉8九인데 그쪽은 7七에 銀을 요구하므로 동시에 성립하지 않는다.
		tag: Tag{Code: "millennium", NameJa: "ミレニアム囲い", Kind: KindCastle},
		squares: []square{
			{8, 9, shogi.King}, {7, 9, shogi.Gold}, {7, 7, shogi.Knight},
		},
		source: wikiMille,
	},
	{
		// 天守閣美濃 — 「8六に歩を突き8七の位置に玉を構える」. 金銀은 左美濃와 같다.
		//
		// 玉이 한 段 올라간 것이 이름을 갈라서, 左美濃와 동시에 성립하지 않는다.
		tag: Tag{Code: "tenshukaku_mino", NameJa: "天守閣美濃", Kind: KindCastle},
		squares: []square{
			{8, 7, shogi.King}, {7, 8, shogi.Gold}, {6, 8, shogi.Gold}, {7, 7, shogi.Silver},
		},
		source: wikiHidari,
	},
	{
		// 振り飛車穴熊 — 居飛車穴熊를 **좌우로 뒤집은** 자리다. 振り飛車는 飛를 왼쪽으로
		// 振るので玉が右へ行き、隅も1九になる。
		//
		// `squareFor` 의 거울(後手용 180° 회전)과 **다른 뒤집기**라 따로 적어야 한다.
		// 좌우 대칭은 先手 안에서의 이야기이고, 그걸 회전으로 얻을 수는 없다.
		tag: Tag{Code: "furibisha_anaguma", NameJa: "振り飛車穴熊", Kind: KindCastle},
		squares: []square{
			{1, 9, shogi.King}, {2, 8, shogi.Silver}, {3, 9, shogi.Gold},
		},
		source: wikiAna,
	},
	{
		// 4枚穴熊 — 3枚에 金7八을 더한 형태
		tag: Tag{Code: "yonmai_anaguma", NameJa: "四枚穴熊", Kind: KindCastle},
		squares: []square{
			{9, 9, shogi.King}, {8, 8, shogi.Silver}, {7, 9, shogi.Gold}, {7, 8, shogi.Gold},
		},
		source: wikiAna,
	},
	{
		// 「先手番であれば6七銀、5七銀、7八金、5八金の金銀4枚の形であり、その場合玉は
		// 基本的には6九に置いていた」 — 旧型(相居飛車二枚銀雁木)의 배치다.
		//
		// カニ囲い와 玉6九·金7八·金5八을 공유하는데 銀의 자리가 갈린다(6八 vs 6七·5七).
		// 銀은 두 장뿐이라 두 囲い가 동시에 성립할 수 없다.
		tag: Tag{Code: "gangi", NameJa: "雁木囲い", Kind: KindCastle},
		squares: []square{
			{6, 9, shogi.King}, {7, 8, shogi.Gold}, {5, 8, shogi.Gold},
			{6, 7, shogi.Silver}, {5, 7, shogi.Silver},
		},
		source: wikiGangi,
	},
	{
		// 「通常は『▲7八金・▲6八銀・▲5八金・▲6九王』の形」
		tag: Tag{Code: "kani", NameJa: "カニ囲い", Kind: KindCastle},
		squares: []square{
			{6, 9, shogi.King}, {7, 8, shogi.Gold}, {5, 8, shogi.Gold}, {6, 8, shogi.Silver},
		},
		source: wikiKani,
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

// formationByFile 는 飛를 振った 筋(**先手 기준**)으로 전법을 정한다. **段을 안 보고,
// 국면이 아니라 수순에서 읽는다** — 段을 고정하면 飛를 올린 순간 이름이 꺼지고, 국면으로
// 물으면 종반에 그 筋을 지나가는 飛까지 걸린다(journal §30, 09-tags.md §2).
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

// senteRank 는 그 색의 段을 先手 기준으로 옮긴다. senteFile 의 段 판. 先手는 그대로,
// 後手는 뒤집으므로 **자기 2段이 양쪽 다 8**이 된다(先手 八 · 後手 二).
func senteRank(rank int, c shogi.Color) int {
	if c == shogi.Black {
		return rank
	}
	return 10 - rank
}

// OpeningPlies 는 **戦法·戦型이 선언될 수 있는 구간**이다. 手数(양쪽 합)로 센다. 없으면
// 종반에 떠돌던 飛가 「中飛車」가 된다 — 값의 근거와 341판 실측, 남은 **[미확정]** 은
// journal §44.
//
// 아래 비율(이름이 처음 붙은 手数가 20수보다 뒤인 판)만 §44 에 없어서 여기가 유일본이다:
//
//	shiken_bisha 43%  sanken_bisha 40%  kaku_gawari 65%
//	naka_bisha 62%  migi_shiken_bisha 96%  ai_furibisha 95%
const OpeningPlies = 24

// DetectFormation 은 **플레이어 자신의 수만** 순서대로 받아 전법을 읽는다.
//
// 飛를 좇다가 **처음으로 筋을 바꾼 수**를 찾는다. 그 도착 筋이 곧 전법이고, 먼저
// 나온 것이 이긴다 — 振り直し(예: 四間에서 三間으로)는 그 판의 전법을 바꾸지 않는다.
//
// 飛가 잡혔다가 다시 打たれる 경우는 걸리지 않는다. 打은 `From < 0` 이라 좇던 칸과
// 절대 안 맞고, 그 뒤로는 아무것도 반환하지 않는다 — **거짓으로 붙이는 것보다 안 붙는
// 쪽이 낫다.**
//
// **振った 手数가 序盤 안이어야 한다**(OpeningPlies). 경계를 「지금 몇 수째인가」가 아니라
// **「振った 것이 몇 수째였나」**에 거는 것이 요점이다 — 그래야 12수에 얻은 四間飛車가
// 100수째에도 그대로 남는다. 지금 手数로 자르면 이름이 대국 중에 사라진다.
func DetectFormation(playerMoves []string, c shogi.Color) (Tag, bool) {
	rook := shogi.SquareOf(rookStartFile(c), rookStartRank(c))

	for i, usi := range playerMoves {
		m, err := shogi.ParseUSIMove(usi)
		if err != nil || m.IsDrop() || int(m.From) != rook {
			continue
		}
		rook = int(m.To)

		if from, to := shogi.FileOf(int(m.From)), shogi.FileOf(int(m.To)); from != to {
			// i 번째 자기 수는 전체로 세면 先手가 2i+1, 後手가 2i+2 手째다.
			ply := 2*i + 1
			if c == shogi.White {
				ply++
			}
			if ply > OpeningPlies {
				return Tag{}, false // 종반의 転換은 그 판의 전법이 아니다
			}
			// 振り飛車는 飛를 **자기 2段**에 振る게 선언이다 — 先手 ▲3八飛·▲6八飛(八),
			// 後手 △3二飛·△5二飛(二). 그 段이 아니면 전법 선언이 아니다: ▲3四飛(横歩取り)은
			// 筋만 보면 袖飛車와 같지만 敵陣 3四로 뛰어드는 것이라 여기서 걸러진다.
			// 段을 formationByFile 에 안 넣고 여기서만 보는 이유: 振った 뒤 ▲3五飛로
			// 올라가는 것은 이 시점에 이미 이름이 정해져 다시 안 물어서, 段을 안 보는
			// 규칙(그 段이 움직여도 이름이 안 꺼짐)은 그대로 유지된다.
			if senteRank(shogi.RankOf(int(m.To)), c) != 8 {
				return Tag{}, false
			}
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

// squareFor 는 先手 좌표를 그 색의 좌표로 옮긴다 — 後手 진영은 180° 회전이라 筋·段을
// 함께 뒤집는다. **後手 정의를 손으로 두 벌 적지 않는 이유는 journal §30.**
func squareFor(s square, c shogi.Color) int {
	if c == shogi.Black {
		return shogi.SquareOf(s.file, s.rank)
	}
	return shogi.SquareOf(10-s.file, 10-s.rank)
}

func (sh shape) matches(pos shogi.Position, c shogi.Color, dropped map[int]bool) bool {
	for _, s := range sh.squares {
		sq := squareFor(s, c)
		if pos.Board[sq] != shogi.MakePiece(s.pt, c) {
			return false
		}
		if dropped[sq] {
			return false // 打って 채운 칸은 「짰다」가 아니다 — droppedSquares
		}
	}
	return true
}

// droppedSquares 는 **打으로 놓인 뒤 한 번도 안 움직인 자기 駒의 칸**이다.
//
// 囲い는 「玉을 감싸도록 駒를 옮겨 짓는 것」이라, 종반에 수비용으로 打은 金 하나가 우연히
// 필수 칸을 채우는 것은 그 囲い를 지은 것이 아니다. 실제로 사람이 둔 판에서 **59手에
// 金矢倉이 깨진 뒤 63手의 `G*6h` 하나로 左美濃가 한 手 동안 떴다**(회차 1 #5).
//
// 戦法의 OpeningPlies 와 **같은 문제의 다른 축**이다 — 그쪽은 종반에 떠돌던 飛가 中飛車가
// 되는 것이었고(§44), 여기는 종반에 打은 金이 囲い를 만드는 것이다. 다만 **경계를 手数로
// 걸지 않는다**: §44가 「美濃는 70수째에 서도 美濃다」로 정한 것은 그대로 맞고, 갈라야 할
// 것은 늦은 것이 아니라 **옮겨 짓지 않은 것**이다.
//
// 상대 수는 안 봐도 된다. 打은 駒를 상대가 따 가면 그 칸이 적 駒가 되어 matches 가 먼저
// 걸러 내고, 그 뒤 자기 駒가 그 칸으로 **옮겨** 오면 아래에서 false 로 지워진다.
func droppedSquares(playerMoves []string) map[int]bool {
	var out map[int]bool
	for _, usi := range playerMoves {
		m, err := shogi.ParseUSIMove(usi)
		if err != nil {
			continue
		}
		if m.IsDrop() {
			if out == nil {
				out = map[int]bool{}
			}
			out[int(m.To)] = true
			continue
		}
		if out != nil {
			delete(out, int(m.To)) // 옮겨 온 駒가 덮었다
			delete(out, int(m.From))
		}
	}
	return out
}

// pick 은 맞는 것 중 **필수 칸이 가장 많은 것**을 고른다.
//
// 판정 순서가 규칙의 일부인 것은 블런더 카테고리와 같다(01-core.md §3). 여기서는
// 순서가 아니라 구체성으로 정하는데, 그래야 정의를 추가할 때 표의 순서를 신경 쓰지
// 않아도 된다 — 本美濃는 片美濃의 칸을 모두 포함하므로 **항상** 本美濃가 이긴다.
func pick(shapes []shape, pos shogi.Position, c shogi.Color, dropped map[int]bool) (Tag, bool) {
	best := -1
	var found Tag
	for _, sh := range shapes {
		if len(sh.squares) > best && sh.matches(pos, c, dropped) {
			best, found = len(sh.squares), sh.tag
		}
	}
	return found, best >= 0
}

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

// Detect 는 이 색이 지금 짜고 있는 이름들을 돌려준다. 없으면 빈 슬라이스다.
//
// **이름과 달리 手筋은 안 낸다.** 축이 囲い·戦法·戦型 셋뿐이라, 手筋이 필요한 쪽은
// `FindTesuji`·`DropTesuji` 를 따로 불러야 한다(`game/tesuji.go` 가 그렇게 한다).
//
// 축마다 최대 하나이고 囲い가 먼저 온다 — 화면이 순서를 다시 정하지 않아도 되게.
//
// **호출하는 쪽이 플레이어 색만 넘긴다.** 컴퓨터 쪽 태그는 화면에 그리지 않는다
// (01-core.md §7 — 상대의 계획을 알려주지 않는다). 그 규칙을 이 함수가 강제하지 않는
// 이유는, 리뷰 화면이 끝난 판을 양쪽 다 보여주는 자리에서는 반대가 맞기 때문이다.
func Detect(in Input) []Tag {
	var out []Tag

	castle, castled := pick(castles, in.Pos, in.Color, droppedSquares(in.PlayerMoves))
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

	// **角換わり만 「지금 手数」로 자른다** — 상태 술어라 교환 시점을 복원할 수 없어서다.
	// 대가로 이 이름은 序盤 동안만 뜬다(journal §44).
	traded := len(in.PlayerMoves)+len(in.OpponentMoves) <= OpeningPlies && bishopsTraded(in.Pos)

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

// bishopsTraded 는 角交換이 **끝난 상태**인지 본다 — 양쪽 持ち駒에 角이 하나씩 **그리고**
// 판에 角·馬가 없다. 한쪽만 보면 각각 샌다(09-tags.md §2).
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

// All 은 정의된 모든 태그다. 기보 스캔과 테스트가 **축을 가로질러** 훑는 데 쓴다 —
// 축마다 컨테이너가 다르므로(`castles`·`formationByFile`) 이 하나가 없으면 부르는 쪽마다
// 목록이 두 벌이 되고, 태그를 하나 더할 때 그중 하나가 조용히 빠진다.
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
	out = append(out, ibisha, kakuGawari, aiFuribisha, kakukanFuribisha)

	// 手筋은 駒 종류로 이름이 갈린다. 순서를 못 박아 테스트 출력이 흔들리지 않게 한다.
	for _, pt := range []shogi.PieceType{shogi.Knight, shogi.Silver, shogi.Rook, shogi.Bishop} {
		out = append(out, forkNames[pt])
	}
	return append(out, dengaku, haraGin, keitouGin, soko_no_fu)
}

// ByCode 는 코드로 태그를 되찾는다. 두 번째 값이 false 면 모르는 코드다.
//
// **기록에서 이름을 되찾는 자리다.** DB에 남는 것은 코드뿐이고(games.style_tags), 화면에
// 나갈 이름과 축은 여기서만 온다 — 부르는 쪽이 자기 표를 만들면 판에 뜨는 이름과
// 마이페이지의 이름이 갈린다.
//
// **모르는 코드는 이름을 안 지어낸다.** 정의를 지우거나 이름을 바꾼 뒤에도 옛 기록에는
// 그 코드가 남아 있고, 그때 코드를 그대로 화면에 내보내면 일본어 화면에 영어가 뜬다.
func ByCode(code string) (Tag, bool) {
	for _, t := range All() {
		if t.Code == code {
			return t, true
		}
	}
	return Tag{}, false
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
