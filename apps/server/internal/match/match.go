// Package match 는 사람 대 사람 대국이다.
//
// 개입이 없다. 판정도 힌트도 무르기도 여기 없고, 그것이 internal/game 과 갈라져
// 있는 이유다(02-architecture.md §7 위협 1, journal §83).
//
// 상태 주인이 둘이고 지키는 방법이 다르다. 방은 Hub.mu 가 지키고, 대국은 Table
// 의 goroutine 이 소유한다(internal/game 과 같은 규약). 방은 「누가 들어올 수 있나」만,
// 대국은 「누가 무엇을 뒀나」만 안다.
//
// 세션이 연결에 안 매여 있다. 한쪽이 끊겨도 상대가 남아 있어 끝낼 수가 없고, 그래서
// 시계가 돈다(journal §83).
package match

import (
	"crypto/rand"

	"errors"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// DefaultTurnLimit 은 한 수에 주는 시간이다. 持ち時間이 아니라 1手 제한이고, 시계가
// 있는 이유는 빨리 두게 하려는 것이 아니라 판이 끝나게 하려는 것이다. 60초는 실측이
// 아니라 정한 값이다(journal §83).
const DefaultTurnLimit = 60 * time.Second

// OpenTTL 은 상대가 안 들어온 방이 사는 시간이다. 넘으면 링크가 죽는다 — 초대 링크가
// 곧 열쇠라(newRoomID), 링크가 오래 살수록 새어 나간 링크가 오래 통한다.
const OpenTTL = 30 * time.Minute

// FinishedTTL 은 끝난 판을 방에 남겨 두는 시간이다. 둘 다 결과를 보고 나갈 만큼만이고,
// 이 뒤로는 그 링크가 404다.
const FinishedTTL = 10 * time.Minute

// roomIDLen 은 방 id 의 글자 수다. 62자 알파벳 8글자 = 62^8, 약 47.6비트.
//
// 이 상수가 하는 일은 유추를 막는 것 하나다. 초대 링크 방식이라 「링크를 안다」가 곧 입장
// 자격의 절반이고(나머지 절반은 로그인과 정원 2명), games.id 같은 연번을 절대 쓰지
// 않는 이유다.
//
// 레이트 리밋이 없으므로 이 값은 열려 있는 방 수에 달렸다. 방 10개에 초당 1만 번을
// 찍어도 1% 확률에 250일이 걸리지만, 방이 수만 개가 되면 그 계산이 달라진다.
const roomIDLen = 8

// roomIDAlphabet 은 영문 대소문자와 숫자뿐이다. -·_ 를 넣지 않는다 —
// 링크를 손으로 옮겨 적거나 읽어 주는 자리가 있고, 그 둘이 거기서 가장 잘 틀린다.
const roomIDAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// ErrNoRoom 은 그 사람이 그 방에 못 들어간다는 것 하나다.
//
// 왜인지를 안 갈라 준다(journal §83). 이어하기가 남의 판 번호에 대해 하는 것과 같은
// 판단이다(server/ws.go 의 errNoResume).
var ErrNoRoom = errors.New("match: no such room")

// ErrNotYourTurn·ErrFinished 는 착수 거절이다. 룰 위반은 shogi 가 낸다.
//
// 「아직 시작 안 했다」가 없다. 그 상태에서는 착수가 도달할 자리가 없다 — 읽는 쪽이
// room.Ready() 뒤에야 돌기 때문이다(server/ws_match.go). 문구를 만들어 두면 영영
// 안 뜨는 문구가 하나 생긴다.
var (
	ErrNotYourTurn = errors.New("match: not your turn")
	ErrFinished    = errors.New("match: the game is over")
	// ErrClosed 는 끝난 테이블에 명령을 보냈을 때다.
	ErrClosed = errors.New("match: table closed")
)

// Player 는 대국자 하나다. skill_profile 에서 오는 것이 하나도 없다 —
// 실력 프로파일은 본인만 보는 값이고, 대인전 상대에게 넘어가면 안 된다
// (02-architecture.md §7 위협 2).
type Player struct {
	UserID int64
	// Name 은 화면에 나가는 이름이다(users.display_name).
	Name string
}

// newRoomID 는 유추할 수 없는 방 id 하나를 만든다. 영숫자 8자다.
func newRoomID() string {
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

// RandomColor 는 振り駒다. 방을 만든 사람도 결과를 모른다 — 응답의 yourColor 가
// 처음 알려 준다.
func RandomColor() shogi.Color {
	var b [1]byte
	rand.Read(b[:])
	return shogi.Color(b[0] & 1)
}

// ColorCode 는 先手·後手를 화면·games.my_color 와 같은 어휘로 옮긴다. 이 규약의
// 자리는 여기 하나다 — 두 벌이면 한쪽을 고칠 때 기록과 화면이 갈린다.
func ColorCode(c shogi.Color) string {
	if c == shogi.White {
		return "w"
	}
	return "b"
}
