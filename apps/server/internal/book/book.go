// Package book 은 상대가 초반에 따라 두는 진형 수순이다.
//
// **판도 엔진도 모른다.** 여기 있는 것은 USI 문자열 목록뿐이고, 그 수를 지금 국면에서
// 둘 수 있는지·둬도 되는지는 부르는 쪽이 정한다(game.bookOpponent). intervene 이 엔진을
// 모르는 것과 같은 성질이고, 그래서 수순을 고치는 데 엔진도 DB도 필요 없다.
//
// **상대 자신의 수만 담는다.** 사람의 수와 짝지어 두지 않는 이유는 진형 만들기가 사람이
// 무엇을 하든 대체로 같은 순서로 돌기 때문이다 — 짝으로 묶으면 초심자가 정석대로 받아주지
// 않는 순간(거의 매번이다) 북이 첫 수에서 끝난다.
package book

import (
	"fmt"
	"strings"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// Opening 은 고를 수 있는 진형 하나다.
//
// Name·Note 는 화면에 그대로 나가므로 일본어다.
type Opening struct {
	ID   string
	Name string
	Note string
	// Source 는 이 수순의 출처다. 위키백과 인용 범위와 URL 규약은 06-status.md §30.
	Source string
	// black 은 상대가 先手일 때의 수순이다. 後手 몫은 Moves 가 180° 돌려 만든다.
	black []string
}

// openings 의 순서가 곧 화면에 뜨는 순서다. 振り飛車 둘 · 居飛車 둘로 짝을 맞춰 둔 것은
// 「相手の戦型を選ぶ」가 뜻을 가지려면 서로 다른 쪽이 보여야 하기 때문이다.
var openings = []Opening{
	{
		ID:     "shikenbisha",
		Name:   "四間飛車",
		Note:   "飛車を6筋に振り、美濃囲いに組みます。振り飛車の基本形です。",
		Source: "https://ja.wikipedia.org/wiki/四間飛車",
		black: []string{
			"7g7f", // ▲7六歩
			"2h6h", // ▲6八飛 — 玉·金이 들어오기 전에 振る. 뒤로 밀면 슬라이드가 막힌다
			"5i4h", // ▲4八玉
			"4h3h", // ▲3八玉
			"4i4h", // ▲4八金
			"6i5h", // ▲5八金
			// **▲6六歩이 ▲7八銀보다 먼저다.** 銀이 7九를 비우는 순간 8八의 角을 받치는
			// 것이 없어지고, 대각선이 열려 있으면 그 자리에서 角을 공짜로 준다.
			"6g6f", // ▲6六歩
			"7i7h", // ▲7八銀
			"1g1f", // ▲1六歩
			"9g9f", // ▲9六歩
		},
	},
	{
		ID:     "nakabisha",
		Name:   "中飛車",
		Note:   "飛車を5筋に振って中央から圧力をかけます。狙いが分かりやすい戦型です。",
		Source: "https://ja.wikipedia.org/wiki/中飛車",
		black: []string{
			"5g5f", // ▲5六歩
			"2h5h", // ▲5八飛 — 위와 같은 이유로 먼저 振る
			"5i4h", // ▲4八玉
			"4h3h", // ▲3八玉
			"4i4h", // ▲4八金
			"6i6h", // ▲6八金 — 5八은 飛가 쓰므로 四間飛車와 갈린다
			"7i7h", // ▲7八銀
			"1g1f", // ▲1六歩
		},
	},
	{
		ID:     "yagura",
		Name:   "矢倉",
		Note:   "居飛車で玉を8八まで運び、金銀三枚で固めます。持久戦になります。",
		Source: "https://ja.wikipedia.org/wiki/矢倉囲い",
		black: []string{
			"7g7f", // ▲7六歩
			// **두 번째 수가 ▲6六歩이어야 한다.** ▲7六歩이 7七을 비웠으므로 상대가 3四歩을
			// 밀면 2二의 角이 8八까지 닿는다. 그 상태로 ▲6八銀을 두면 8八을 받치던 銀이
			// 사라져 **角을 공짜로 준다** — 실제로 브라우저에서 개입이 그것을 잡았다(§48).
			"6g6f", // ▲6六歩
			"7i6h", // ▲6八銀
			"6i7h", // ▲7八金
			"8h7i", // ▲7九角 — 銀이 비운 7九로. 玉이 지나갈 길을 여는 첫 걸음이다
			"6h7g", // ▲7七銀
			"7i6h", // ▲6八角 — 다시 비켜 준다. 玉의 통로가 7九 하나뿐이라서다
			"5i6i", // ▲6九玉
			"6i7i", // ▲7九玉
			"7i8h", // ▲8八玉
			"2g2f", // ▲2六歩
		},
	},
	{
		ID:     "bogin",
		Name:   "棒銀",
		Note:   "銀をまっすぐ繰り出して端から攻めます。攻め方が一本道で覚えやすい戦型です。",
		Source: "https://ja.wikipedia.org/wiki/棒銀",
		black: []string{
			"2g2f", // ▲2六歩 — 2七을 비운다. 銀의 통로다
			"2f2e", // ▲2五歩
			"3i3h", // ▲3八銀
			"3h2g", // ▲2七銀
			"2g2f", // ▲2六銀 — 歩가 2五로 올라가 비운 칸. 위 첫 수와 USI가 같지만 다른 駒다
			"6i7h", // ▲7八金
			"5i6h", // ▲6八玉
			"7g7f", // ▲7六歩
			"3g3f", // ▲3六歩
			"1g1f", // ▲1六歩
		},
	},
}

// All 은 고를 수 있는 진형 전부다. 화면이 목록을 그리는 데 쓴다.
func All() []Opening { return openings }

// Find 는 id 로 진형을 찾는다. 빈 id 는 「おまかせ」라 없는 것으로 답한다 —
// 부르는 쪽이 그때 북 없이 상대를 만든다.
func Find(id string) (Opening, bool) {
	if id == "" {
		return Opening{}, false
	}
	for _, o := range openings {
		if o.ID == id {
			return o, true
		}
	}
	return Opening{}, false
}

// Moves 는 상대가 c 를 잡을 때 둘 수순이다.
//
// **後手 몫은 돌려서 만든다.** 두 벌 적어 두면 한쪽을 고칠 때 다른 쪽이 조용히 낡고,
// 어긋난 것을 알아차릴 방법이 없다 — 진형은 눈으로 봐야 틀린 것이 보이는 종류다.
// 180° 회전이 정확히 先手/後手 대응이라 좌표 변환으로 닫힌다.
func (o Opening) Moves(c shogi.Color) []string {
	if c == shogi.Black {
		return append([]string(nil), o.black...)
	}
	out := make([]string, 0, len(o.black))
	for _, m := range o.black {
		out = append(out, mirror(m))
	}
	return out
}

// mirror 는 수를 판 가운데를 중심으로 180° 돌린 것이다.
//
// 못 읽는 문자열은 그대로 돌려준다 — 여기서 에러를 만들어도 부를 곳이 없고(수순은 상수다),
// 어차피 착수 직전에 룰 엔진이 한 번 더 본다.
func mirror(usi string) string {
	// 打(`P*5e`). 駒 글자는 그대로 두고 칸만 돌린다.
	if i := strings.IndexByte(usi, '*'); i >= 0 {
		sq, ok := mirrorSquare(usi[i+1:])
		if !ok {
			return usi
		}
		return usi[:i+1] + sq
	}
	if len(usi) < 4 {
		return usi
	}
	from, ok1 := mirrorSquare(usi[0:2])
	to, ok2 := mirrorSquare(usi[2:4])
	if !ok1 || !ok2 {
		return usi
	}
	return from + to + usi[4:] // 成 표시(`+`)는 그대로 붙는다
}

func mirrorSquare(s string) (string, bool) {
	if len(s) != 2 || s[0] < '1' || s[0] > '9' || s[1] < 'a' || s[1] > 'i' {
		return "", false
	}
	file := 10 - int(s[0]-'0')
	rank := 10 - int(s[1]-'a'+1)
	return fmt.Sprintf("%d%c", file, 'a'+byte(rank-1)), true
}
