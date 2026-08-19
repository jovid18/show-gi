// Package handicap 은 駒落ち — 手合割 하나가 **시작 국면과 「형세 0」을 같이 정한다.**
//
// **엔진도 판정도 모른다.** 여기 있는 것은 SFEN 문자열과 cp 상수뿐이고, 그것으로 무엇을
// 하는지는 부르는 쪽이 정한다(intervene.Input.BaselineCp · game.adaptiveOpponent) —
// `book` 이 수순만 들고 있는 것과 같은 성질이라 표를 고치는 데 엔진도 DB도 필요 없다.
//
// **두 값을 한 표에 두는 것이 요점이다.** 국면만 주면 판정식이 포화 구간에 갇힌다 —
// 승률 낙폭은 우세 구간에서 압축되므로(01-core.md §2) 二枚落ち에서는 낙폭 0.25를 넘기는 데
// **1058cp**가 필요하다(平手는 635cp). 銀을 공짜로 주는 것이 약 1000cp이므로(01-core.md §2)
// 그것도 안 걸린다는 뜻이다. 기준점이 곧 그 국면의 「아직 아무것도 안 흘렸다」이고,
// 빼고 나면 駒落ち도 660cp 언저리에서 걸린다 — 실측과 그 결과는 journal §84.
//
// **平手는 이 표에 없으므로 판정이 한 비트도 안 바뀐다.** 기준점이 0이라 빼는 것이 없다.
//
// **平手는 이 표에 없다.** 빈 `startSFEN` 이 平手라는 규약이 이미 있고(game.Config.StartSFEN),
// 화면의 「平手」도 서버 목록이 아니라 클라이언트의 기본값이다 — `book` 의 「おまかせ」와
// 같은 자리다. 그래서 `Find("")` 도 `Of("")` 도 없는 것으로 답하고, 기준점은 0이 된다.
//
// > 平手의 실측 초기 평가치는 **+91**이다. 그 값을 기준점으로 쓰지 않는 것은 지금까지의
// > 상수가 전부 **0을 기준으로** 잡혔기 때문이다 — 265시도 재채점(journal §39)이 그 좌표에서
// > 나왔고, 91을 넣는 순간 그 측정이 통째로 다른 기준의 것이 된다.
package handicap

import (
	"strings"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// Handicap 은 고를 수 있는 手合割 하나다.
//
// Name·Note 는 화면에 그대로 나가므로 일본어다.
type Handicap struct {
	ID   string
	Name string
	Note string
	// SFEN 은 그 手合의 0手目다. **上手(後手)의 駒를 빼고 下手(先手)가 먼저 둔다** —
	// 駒落ち에 先手/後手가 없고 언제나 下手부터라서다.
	SFEN string
	// BaselineCp 는 그 국면의 「형세 0」이다. **下手(先手) 관점 cp.**
	//
	// 水匠5 · depth 14 · **`FV_SCALE=24`** 실측이다. 마지막 값이 조건인 것이 요점이다 —
	// 그것을 안 걸고 재면 같은 국면이 1.5배로 나오고(첫 측정이 그랬다, journal §84) 표가
	// 통째로 다른 척도가 된다. 그 숫자가 무엇을 정하는지는 패키지 주석에 있다.
	//
	// **[미확정]** K=600이 초기값인 것과 같은 벽이다 — 재측정은 baseline_measure_test.go.
	BaselineCp int
}

// list 의 순서가 곧 화면에 뜨는 순서다. **落とす 駒가 늘어나는 쪽으로 간다** — 手合割의
// 관례 순서이고, 첫 항목이 平手에 가장 가까운 것이라 「조금만 접어 본다」가 위에 온다.
//
// **八枚落ち·十枚落ち는 아직 넣지 않았다.** 기준점을 옮기면 판정은 거기서도 돌지만
// (실측 +3258·+4010), 上手에 攻め駒가 하나도 없어 판이 개입이 아니라 **詰め 연습**이 된다 —
// 그건 기준점이 고쳐 주는 종류의 문제가 아니다. 넣을지는 플레이테스트가 답한다(journal §84).
var list = []Handicap{
	{
		ID:         "kyoochi",
		Name:       "香落ち",
		Note:       "相手の左の香車を落とします。平手にいちばん近い手合割です。",
		SFEN:       "lnsgkgsn1/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1",
		BaselineCp: 270,
	},
	{
		ID:         "kakuochi",
		Name:       "角落ち",
		Note:       "相手の角行を落とします。斜めから来る攻めがなくなります。",
		SFEN:       "lnsgkgsnl/1r7/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1",
		BaselineCp: 756,
	},
	{
		ID:         "hishaochi",
		Name:       "飛車落ち",
		Note:       "相手の飛車を落とします。縦の攻めがなくなります。",
		SFEN:       "lnsgkgsnl/7b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1",
		BaselineCp: 741,
	},
	{
		ID:         "hikyoochi",
		Name:       "飛香落ち",
		Note:       "相手の飛車と左の香車を落とします。",
		SFEN:       "lnsgkgsn1/7b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1",
		BaselineCp: 865,
	},
	{
		ID:         "nimaiochi",
		Name:       "二枚落ち",
		Note:       "相手の飛車と角行を落とします。駒落ちの定跡がいちばん整っている手合割です。",
		SFEN:       "lnsgkgsnl/9/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1",
		BaselineCp: 1490,
	},
	{
		ID:         "yonmaiochi",
		Name:       "四枚落ち",
		Note:       "飛車・角行と香車二枚を落とします。端から攻める形をおぼえられます。",
		SFEN:       "1nsgkgsn1/9/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1",
		BaselineCp: 1834,
	},
	{
		ID:         "rokumaiochi",
		Name:       "六枚落ち",
		Note:       "飛車・角行・香車二枚・桂馬二枚を落とします。攻め方をおぼえるのに向いています。",
		SFEN:       "2sgkgs2/9/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1",
		BaselineCp: 2011,
	},
}

// All 은 고를 수 있는 手合割 전부다. 화면이 목록을 그리는 데 쓴다.
func All() []Handicap { return list }

// Find 는 id 로 手合割을 찾는다. **빈 id 는 平手라 없는 것으로 답한다** — 부르는 쪽이
// 그때 지금까지처럼 평수 초기 국면으로 판을 연다(패키지 주석).
func Find(id string) (Handicap, bool) {
	if id == "" {
		return Handicap{}, false
	}
	for _, h := range list {
		if h.ID == id {
			return h, true
		}
	}
	return Handicap{}, false
}

// Of 는 시작 국면으로 手合割을 되짚는다. 이어하는 판과 되짚기가 **기록의 `start_sfen`
// 하나에서** 手合을 다시 얻는 자리다 — 그래서 칸을 새로 만들지 않았다.
//
// **手数를 안 본다.** 앞 세 칸(판·手番·持ち駒)으로만 맞춘다 — `positions` 캐시 키와 같은
// 규약이고(001_init.sql), 왕복하며 `Position.SFEN()` 을 거친 문자열도 같은 표에 붙어야 한다.
func Of(startSFEN string) (Handicap, bool) {
	k := key(startSFEN)
	if k == "" {
		return Handicap{}, false
	}
	for _, h := range list {
		if key(h.SFEN) == k {
			return h, true
		}
	}
	return Handicap{}, false
}

// NameOf 는 화면에 나갈 이름이다. 平手나 모르는 국면은 빈 문자열이다 —
// 스냅샷이 그때 그 칸을 아예 안 보낸다(game.Snapshot.HandicapJa).
func NameOf(startSFEN string) string {
	h, ok := Of(startSFEN)
	if !ok {
		return ""
	}
	return h.Name
}

// BaselineCp 는 그 판의 「형세 0」이다. **下手(先手) 관점 cp.** 平手는 0이다.
func BaselineCp(startSFEN string) int {
	h, ok := Of(startSFEN)
	if !ok {
		return 0
	}
	return h.BaselineCp
}

// BaselineCpFor 는 같은 값을 **c 관점**으로 돌려준다.
//
// 부호를 뒤집는 자리를 여기 하나로 모아 둔다 — 판정(사람 관점)과 밴드(플레이어 관점)가
// 같은 표를 서로 다른 관점으로 쓰므로, 부르는 쪽마다 뒤집으면 한쪽만 고쳐지는 날이 온다.
// 표의 값이 언제나 下手(先手) 관점인 것이 이 함수가 성립하는 조건이다(Handicap.BaselineCp).
func BaselineCpFor(startSFEN string, c shogi.Color) int {
	cp := BaselineCp(startSFEN)
	if c == shogi.White {
		return -cp
	}
	return cp
}

// key 는 手数를 뺀 SFEN 이다. 칸이 셋보다 적으면 手合을 말할 수 없으므로 빈 값이다.
func key(sfen string) string {
	f := strings.Fields(sfen)
	if len(f) < 3 {
		return ""
	}
	return strings.Join(f[:3], " ")
}
