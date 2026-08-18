package match

import (
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// 클라이언트가 보는 타입들이다. **json 태그가 곧 웹과의 계약이다** — `game.Snapshot` 과
// 같은 규약이고, 갈라 둔 이유는 **여기 없는 것들** 때문이다: 개입·힌트·무르기·게이지·
// 태그·상대의 강함이 전부 없고, 대신 시계와 상대의 접속이 있다.
//
// 스냅샷은 언제나 통째로 나간다 — 저쪽과 같은 이유이고, 여기는 하나가 더 있다:
// 화면이 둘이라 부분 갱신을 재구성하게 두면 두 화면이 서로 다르게 어긋난다.

// Status 는 판이 끝났는지, 끝났다면 왜인지다.
type Status string

const (
	StatusPlaying    Status = "playing"
	StatusCheckmate  Status = "checkmate"
	StatusStalemate  Status = "stalemate"
	StatusResigned   Status = "resigned"
	StatusRepetition Status = "repetition"
	// StatusTimeout 은 수번 쪽이 1手 제한시간을 넘긴 것이다. **승패가 난다** —
	// 엔진 대국의 `aborted`(상대의 수를 못 얻어 접은 것)와 갈라 두는 자리다.
	StatusTimeout Status = "timeout"
	// StatusAborted 는 승패 없이 접힌 것이다. **서버가 내려갈 때뿐이다.**
	StatusAborted Status = "aborted"
	// StatusExpired 는 **한 수도 안 둔 채 시간이 다 된 것**이다. 승패가 없다.
	//
	// **`aborted` 와 갈라 둔다.** 둘 다 승패가 없지만 화면이 할 말이 정반대다 — 저쪽은
	// 「서버 사정」이고 이쪽은 「아무도 안 뒀다」인데, 하나로 뭉치면 그냥 자리를 비운 판에서
	// 두 사람 다 서버를 탓하게 된다.
	//
	// **승패를 안 적는 이유**는 0手짜리 판이 두 사람의 전적에 win/loss 로 남고 이긴 쪽의
	// 「振り返り」 링크가 빈 판을 열기 때문이다.
	StatusExpired Status = "expired"
)

// Side 는 **보는 사람 기준**이다. `game.Side`(human/engine)와 어휘가 다른 이유가 그것이다 —
// 대인전에는 사람이 둘이라 절대 이름을 쓰면 같은 기보가 두 화면에서 같은 색으로 보인다.
type Side string

const (
	SideYou      Side = "you"
	SideOpponent Side = "opponent"
)

// Move 는 기보 한 수다.
type Move struct {
	USI string `json:"usi"`
	Ja  string `json:"ja"` // 棋譜 표기(▲7六歩). 서버가 만든 것을 그대로 그린다
	By  Side   `json:"by"`
}

// Snapshot 은 **한쪽이** 보는 판 상태 전부다.
type Snapshot struct {
	SFEN     string `json:"sfen"`
	Ply      int    `json:"ply"`
	Turn     string `json:"turn"` // "b" | "w"
	YourTurn bool   `json:"yourTurn"`
	InCheck  bool   `json:"inCheck"`

	// YourColor 는 이 사람이 잡은 쪽이다. 판을 어느 쪽에서 그릴지가 여기 걸려 있고,
	// 한 판에서 안 바뀐다(`game.Snapshot.YourColor` 와 같은 규약).
	YourColor string `json:"yourColor"`

	// LegalMoves 는 **자기 차례일 때만** 채운다. 상대 차례에 주면 그 사람이 상대의 수를
	// 화면에서 훑어볼 수 있고, 그건 대인전에서 그냥 부정행위 보조다.
	LegalMoves []string `json:"legalMoves"`

	Moves  []Move `json:"moves"`
	Status Status `json:"status"`
	Winner Side   `json:"winner,omitempty"`

	// OpponentName 은 상대의 표시 이름이다(`users.display_name`).
	//
	// **여기서 나가는 상대 정보는 이 하나뿐이다.** 段級도 전적도 안 보낸다 —
	// 실력 프로파일은 본인만 보는 값이다(02-architecture.md §7 위협 2).
	OpponentName string `json:"opponentName"`
	// OpponentOnline 은 상대가 지금 화면을 보고 있는가다.
	//
	// **판은 이 값과 무관하게 돈다.** 나가 있어도 시계는 흐르고, 그것이 판이 끝나는
	// 유일한 장치다(DefaultTurnLimit).
	OpponentOnline bool `json:"opponentOnline"`

	// TurnLimitMs·TurnLeftMs 는 시계다. **서버가 정본이고 화면은 세기만 한다** —
	// 남은 시간을 화면이 계산하면 탭을 멈춰 둔 브라우저에서 시간이 안 간다.
	TurnLimitMs int `json:"turnLimitMs"`
	TurnLeftMs  int `json:"turnLeftMs"`
}

// snapshotData 는 관점이 아직 안 붙은 한 벌이다. 先手·後手마다 한 벌씩 만들지 않는 이유는
// table.go 의 `viewSnapshot`.
type snapshotData struct {
	sfen    string
	ply     int
	turn    shogi.Color
	inCheck bool
	legal   []string
	moves   []recordedMove
	status  Status
	winner  shogi.Color
	hasWin  bool
	names   map[shogi.Color]string
	online  map[shogi.Color]bool
	limit   time.Duration
	left    time.Duration
}

func (st *state) snapshot() *snapshotData {
	d := &snapshotData{
		sfen:    st.pos.SFEN(),
		ply:     len(st.moves),
		turn:    st.pos.Turn,
		inCheck: st.pos.InCheck(st.pos.Turn),
		moves:   st.moves,
		status:  st.status,
		winner:  st.winner,
		hasWin:  st.hasWin,
		names:   map[shogi.Color]string{shogi.Black: st.cfg.Black.Name, shogi.White: st.cfg.White.Name},
		online:  map[shogi.Color]bool{shogi.Black: st.online[shogi.Black] > 0, shogi.White: st.online[shogi.White] > 0},
		limit:   st.limit,
		left:    st.leftForTurn(),
	}
	if st.status == StatusPlaying {
		legal := st.pos.LegalMoves()
		d.legal = make([]string, 0, len(legal))
		for _, m := range legal {
			d.legal = append(d.legal, m.USI())
		}
	}
	return d
}

// for_ 는 그쪽이 보는 한 벌로 편다. **여기가 「너」와 「상대」가 정해지는 유일한 자리다.**
func (d *snapshotData) for_(you shogi.Color) Snapshot {
	s := Snapshot{
		SFEN:           d.sfen,
		Ply:            d.ply,
		Turn:           colorCode(d.turn),
		YourTurn:       d.status == StatusPlaying && d.turn == you,
		InCheck:        d.inCheck && d.turn == you,
		YourColor:      colorCode(you),
		Status:         d.status,
		OpponentName:   d.names[you.Other()],
		OpponentOnline: d.online[you.Other()],
		TurnLimitMs:    int(d.limit / time.Millisecond),
	}
	// **자기 차례일 때만 준다**(Snapshot.LegalMoves).
	if s.YourTurn {
		s.LegalMoves = d.legal
	}
	// **시계도 자기 차례일 때만 흐르는 값이다.** 상대 차례의 남은 시간을 그대로 보내면
	// 두 화면이 같은 숫자를 세면서 서로 다른 사람의 시간이라고 말한다 — 그래서 「지금
	// 수번에 남은 시간」 하나로만 보내고, 누구의 것인지는 `YourTurn` 이 말한다.
	if d.status == StatusPlaying {
		left := d.left
		if left < 0 {
			left = 0
		}
		s.TurnLeftMs = int(left / time.Millisecond)
	}
	if d.hasWin {
		if d.winner == you {
			s.Winner = SideYou
		} else {
			s.Winner = SideOpponent
		}
	}
	s.Moves = make([]Move, 0, len(d.moves))
	for _, m := range d.moves {
		by := SideOpponent
		if m.by == you {
			by = SideYou
		}
		s.Moves = append(s.Moves, Move{USI: m.usi, Ja: m.ja, By: by})
	}
	return s
}
