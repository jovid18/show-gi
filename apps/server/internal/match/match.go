// Package match 는 사람 대 사람 대국이다.
//
// 개입이 없다. 판정도 힌트도 무르기도 여기 없고, 그래서 internal/game 과 갈라져
// 있다(02-architecture.md §7 위협 1, journal §83).
//
// 상태 주인이 둘이고 지키는 방법이 다르다. 방은 Hub.mu 가 지키고, 대국은 Table 의
// goroutine 이 소유한다(internal/game 과 같은 규약).
//
// 세션이 연결에 매여 있지 않다. 한쪽이 끊겨도 상대가 남아 있어 끝낼 수가 없고, 그래서
// 시계가 돈다(journal §83).
package match

import (
	"crypto/rand"

	"errors"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// DefaultTurnLimit 은 한 수에 주는 시간이다. 持ち時間 대신 1手 제한이고, 시계는 판이
// 끝나게 하려고 있다. 60초는 실측 없이 정한 값이다(journal §83).
const DefaultTurnLimit = 60 * time.Second

// OpenTTL 은 상대가 들어오지 않은 방이 사는 시간이다. 초대 링크가 곧 열쇠라
// (NewRoomID) 링크가 오래 살수록 새어 나간 링크가 오래 통한다.
const OpenTTL = 30 * time.Minute

// FinishedTTL 은 끝난 판을 방에 남겨 두는 시간이다. 둘 다 결과를 보고 나갈 만큼만이고,
// 이 뒤로는 그 링크가 404다.
const FinishedTTL = 10 * time.Minute

// roomIDLen 은 방 id 의 글자 수다. 이 상수가 하는 일은 유추를 막는 것 하나이고,
// 그래서 연번을 쓰지 않는다(journal §83).
//
// 레이트 리밋이 없으므로 안전 폭은 열려 있는 방 수에 달렸다. 방 10개에 초당 1만 번을
// 찍어도 1% 확률에 250일이 걸리지만, 방이 수만 개가 되면 그 계산이 달라진다.
const roomIDLen = 8

// roomIDAlphabet 은 영문 대소문자와 숫자뿐이다. -·_ 를 넣지 않는다. 링크를 손으로
// 옮겨 적거나 읽어 주는 자리에서 그 둘이 가장 잘 틀린다.
const roomIDAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// ErrNoRoom 은 그 사람이 그 방에 들어갈 수 없다는 것 하나다.
//
// 왜인지는 알려주지 않는다(journal §83). 이어하기가 남의 판 번호에 대해 하는 것과 같은
// 판단이다(server/ws.go 의 errNoResume).
var ErrNoRoom = errors.New("match: no such room")

// ErrNotYourTurn·ErrFinished 는 착수 거절이다. 룰 위반은 shogi 가 내보낸다.
//
// 「아직 시작하지 않았다」가 없다. 읽는 쪽이 room.Ready() 뒤에야 돌아서
// (server/ws_match.go) 그 상태에서는 착수가 도달할 자리가 없다.
var (
	ErrNotYourTurn = errors.New("match: not your turn")
	ErrFinished    = errors.New("match: the game is over")
	// ErrClosed 는 끝난 테이블에 명령을 보냈을 때다.
	ErrClosed = errors.New("match: table closed")
)

// Player 는 대국자 하나다. skill_profile 에서 오는 것이 하나도 없다. 실력
// 프로파일은 본인만 보는 값이다(02-architecture.md §7 위협 2).
type Player struct {
	UserID int64
	// Name 은 화면에 나가는 이름이다(users.display_name).
	Name string
}

// NewRoomID 는 유추할 수 없는 방 id 하나를 만든다. 영숫자 8자다.
func NewRoomID() string {
	const limit = byte(256 - 256%len(roomIDAlphabet)) // 248

	id := make([]byte, 0, roomIDLen)
	buf := make([]byte, roomIDLen)
	for len(id) < roomIDLen {
		rand.Read(buf)
		for _, b := range buf {
			if b >= limit {
				continue
			}
			id = append(id, roomIDAlphabet[int(b)%len(roomIDAlphabet)])
			if len(id) == roomIDLen {
				break
			}
		}
	}
	return string(id)
}

// RandomColor 는 振り駒다. 방을 만든 사람도 결과를 모르고, 응답의 yourColor 가
// 처음 알려 준다.
func RandomColor() shogi.Color {
	var b [1]byte
	rand.Read(b[:])
	return shogi.Color(b[0] & 1)
}

// ColorCode 는 先手·後手를 화면·games.my_color 와 같은 어휘로 옮긴다. 이 규약의
// 자리는 여기 하나다. 두 벌이면 한쪽을 고칠 때 기록과 화면이 갈린다.
func ColorCode(c shogi.Color) string {
	if c == shogi.White {
		return "w"
	}
	return "b"
}
