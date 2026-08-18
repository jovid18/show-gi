// Package match 는 사람 대 사람 대국이다.
//
// **개입이 없다.** 판정도 힌트도 무르기도 여기 없고, 그것이 이 패키지가 `internal/game`
// 과 갈라져 있는 이유의 전부다 — 개입은 AI 연습 대국 한정이라고 정해 뒀고
// (02-architecture.md §7 위협 1), 대인전에서 한쪽의 수를 되무르면 상대가 그동안 아무것도
// 못 하고 기다리며 **「저쪽이 무슨 수를 뒀다가 물렀나」가 그 사람 화면에 드러난다.**
//
// **상태는 goroutine 하나가 소유한다** — `internal/game` 과 같은 규약이다. 다만 소유하는
// 것이 하나가 아니라 둘이다: 방(`Hub` 의 잠금)과 대국(`Table` 의 goroutine). 방은 「누가
// 들어올 수 있나」만 알고 판은 모르며, 판은 「누가 무엇을 뒀나」만 알고 방을 모른다.
//
// **세션이 연결에 안 매여 있다는 것이 `internal/game` 과 갈리는 두 번째 자리다.** 저쪽은
// 끊기면 판이 끝나지만(journal §46) 여기는 상대가 남아 있으므로 끝낼 수가 없다 — 대신
// 시계가 돈다(journal §83).
package match

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// DefaultTurnLimit 은 한 수에 주는 시간이다.
//
// **시계가 있는 이유는 「빨리 두게 하려고」가 아니라 판이 끝나게 하려고다.** 대인전은
// 상대가 사람이라 탭을 닫으면 그 판이 영원히 「두는 중」으로 남고, 그러면 되짚기에도
// 안 걸리고 기록에도 결과가 없다 — 엔진 대국에서 연결이 끊긴 판을 `abandoned` 로
// 닫는 것과 같은 자리를 여기서는 시계가 맡는다.
//
// **持ち時間(총 시간)이 아니라 1手 제한이다.** 총 시간은 화면에 시계 둘과 秒読み이 필요하고,
// 그 전에 「끝나기는 하는가」를 먼저 세워야 한다.
//
// **[미확정]** 60초는 실측으로 잡은 값이 아니다.
const DefaultTurnLimit = 60 * time.Second

// OpenTTL 은 상대가 안 들어온 방이 사는 시간이다. 넘으면 링크가 죽는다 — 초대 링크가
// 열쇠이므로(newRoomID) 오래 사는 링크는 오래 사는 열쇠다.
const OpenTTL = 30 * time.Minute

// FinishedTTL 은 끝난 판을 방에 남겨 두는 시간이다. 둘 다 결과를 보고 나갈 만큼만이고,
// 이 뒤로는 그 링크가 404다.
const FinishedTTL = 10 * time.Minute

// roomIDBytes 는 방 id 의 엔트로피다. 16바이트 = 128비트.
//
// **유추를 막는 것이 이 상수의 전부다.** 초대 링크 방식이라 「링크를 안다」가 곧 입장
// 자격의 절반이고(나머지 절반은 로그인과 정원 2명), 그 문자열이 열쇠인 이상 순번이나
// 짧은 난수를 쓰면 훑어볼 수 있다. `games.id` 같은 연번을 절대 쓰지 않는 이유다.
const roomIDBytes = 16

// ErrNoRoom 은 **그 사람이 그 방에 못 들어간다**는 것 하나다.
//
// **왜인지를 안 갈라 준다.** 없는 방·만료된 방·남이 이미 찬 방·로그인 안 한 요청이 전부
// 같은 답을 받아야 방 id 를 훑어보는 것이 성립하지 않는다 — 이어하기가 남의 판 번호에
// 대해 하는 것과 같은 판단이다(server/ws.go 의 errNoResume).
var ErrNoRoom = errors.New("match: no such room")

// ErrNotYourTurn·ErrFinished 는 착수 거절이다. 룰 위반은 `shogi` 가 낸다.
//
// **「아직 시작 안 했다」가 없다.** 그 상태에서는 착수가 도달할 자리가 없다 — 읽는 쪽이
// `room.Ready()` 뒤에야 서기 때문이다(server/ws_match.go). 문구를 만들어 두면 영영
// 안 뜨는 문구가 하나 생긴다.
var (
	ErrNotYourTurn = errors.New("match: not your turn")
	ErrFinished    = errors.New("match: the game is over")
	// ErrClosed 는 끝난 테이블에 명령을 보냈을 때다.
	ErrClosed = errors.New("match: table closed")
)

// Player 는 대국자 하나다. **`skill_profile` 에서 오는 것이 하나도 없다** —
// 실력 프로파일은 본인만 보는 값이고, 대인전 상대에게 넘어가면 안 된다
// (02-architecture.md §7 위협 2).
type Player struct {
	UserID int64
	// Name 은 화면에 나가는 이름이다(`users.display_name`).
	Name string
}

// newRoomID 는 유추할 수 없는 방 id 하나를 만든다. 22자 base64url 이다.
//
// **`rand.Read` 가 실패하면 방을 안 만든다.** 실패한 자리를 시각이나 카운터로 메우면
// 그 순간 id 가 유추 가능해지고, 그것이 이 함수가 막는 유일한 것이다.
func newRoomID() (string, error) {
	b := make([]byte, roomIDBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// colorCode 는 先手·後手를 `games.my_color` 의 어휘로 옮긴다.
func colorCode(c shogi.Color) string {
	if c == shogi.White {
		return "w"
	}
	return "b"
}
