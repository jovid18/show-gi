package server

import (
	"errors"
	"log"
	"net/http"

	"github.com/jovid18/show-gi/apps/server/internal/match"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// 대인전의 HTTP 표면. **방을 만들고 들여다보는 데까지**이고, 실제로 두는 것은
// `/ws/match` 다(ws_match.go) — 이어하기가 `/api/resumable` 과 `/ws/game?resume=` 으로
// 갈려 있는 것과 같은 자리다.
//
// **여기 있는 두 경로 다 로그인이 필요하다.** 익명은 서로 구별할 수단이 없어서
// (002_anonymous_games.sql) 「이 방의 상대가 아까 그 사람인가」에 답할 수가 없고,
// 그러면 정원 2명이라는 규칙 자체가 성립하지 않는다.

type matchHandler struct {
	hub  *match.Hub
	auth *authHandler
}

// roomPayload 는 방 하나다. **id 말고는 아무것도 안 준다** — 상대의 段級도 전적도
// 여기 없다(02-architecture.md §7 위협 2).
type roomPayload struct {
	// ID 는 초대 링크에 그대로 들어가는 값이다(128비트 난수, `internal/match`).
	ID string `json:"id"`
	// YourColor 는 이 사람이 잡을 쪽이다. 방을 만든 사람이 고른 색에서 정해진다.
	YourColor string `json:"yourColor"`
	// HostName 은 방을 만든 사람의 이름이다. 손님이 「누구의 방인가」를 확인하는 자리다.
	HostName string `json:"hostName"`
	// Waiting 은 아직 상대가 안 들어왔는가다. 화면이 초대 링크를 그릴지 정한다.
	Waiting bool `json:"waiting"`
	// IsHost 는 보는 사람이 이 방을 만든 사람인가다.
	//
	// **`Waiting` 과 함께 「아직 안 앉은 손님」을 가른다.** 그 사람에게만 확인 화면이
	// 뜨는데, 자리에 앉는 순간 시계가 돌기 시작하므로(WebSocket 이 붙을 때) 링크를 잘못
	// 누른 사람이 모르는 사이에 남의 방 자리를 태우면 안 된다.
	IsHost bool `json:"isHost"`
}

// notFound 는 대인전의 **유일한 거절 응답**이다.
//
// **왜 안 되는지를 절대 안 갈라 준다.** 없는 방·만료된 방·남이 이미 찬 방이 같은 답을
// 받아야 방 id 를 훑어보는 것이 성립하지 않고(match.ErrNoRoom), 로그인 안 한 요청까지
// 여기로 보내는 이유는 그 답이 401이면 **「그 방은 있다」가 로그인 없이 새어 나가기**
// 때문이다 — 401은 「로그인하면 볼 수 있다」는 뜻이라서다.
func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]any{
		"error": "not_found", "message": "その対局部屋は見つかりません。",
	})
}

// create 는 방을 연다. **만든 사람이 색을 고른다** — 상대는 나머지를 잡는다.
func (h *matchHandler) create(w http.ResponseWriter, r *http.Request) {
	s, ok := h.auth.viewer(r)
	if !ok {
		// **여기는 401이다.** 위 notFound 와 갈리는 이유는 새어 나갈 것이 없기 때문이다 —
		// 아직 방이 없으므로 「있다/없다」를 말할 대상이 아니고, 화면은 이 답을 보고
		// 「로그인하고 다시」를 그려야 한다.
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "unauthorized", "message": "対局部屋を作るにはログインが必要です。",
		})
		return
	}

	// **색은 쿼리로 받고, 못 읽으면 先手다.** 시작 화면과 같은 규약이다(ws.go 의 newSetup) —
	// 목록을 서버가 아는 값이라 이상한 값이 오는 것은 클라이언트가 틀린 경우이고,
	// 그때 방을 거절하는 것보다 先手로 여는 쪽이 낫다.
	color := shogi.Black
	if r.URL.Query().Get("color") == "w" {
		color = shogi.White
	}

	room, err := h.hub.Create(match.Player{UserID: s.UserID, Name: s.Name}, color)
	if err != nil {
		// 난수를 못 얻은 경우다. **자리를 메우지 않는다** — 유추 가능한 id 를 주느니
		// 방을 안 만드는 쪽이다(match.newRoomID).
		log.Printf("match: cannot create a room: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "対局部屋を作れませんでした。",
		})
		return
	}

	writeJSON(w, http.StatusOK, roomPayload{
		ID: room.ID, YourColor: colorCode(color), HostName: s.Name, Waiting: true, IsHost: true,
	})
}

// get 은 **들어가기 전에** 방을 확인한다. 자리를 잡지 않는다 — 앉는 것은 WebSocket 이다.
//
// 손님이 링크를 열면 이 답으로 「◯◯さんの対局に参加しますか」가 선다. 그 화면 없이
// 곧바로 붙이면, 링크를 잘못 누른 사람이 **자기도 모르게 자리를 차지하고** 그 방은
// 그때부터 아무도 못 들어간다(정원 2명).
func (h *matchHandler) get(w http.ResponseWriter, r *http.Request) {
	s, ok := h.auth.viewer(r)
	if !ok {
		notFound(w) // 401이 아닌 이유는 notFound 주석
		return
	}

	room, err := h.hub.Peek(r.PathValue("id"), s.UserID)
	if err != nil {
		if !errors.Is(err, match.ErrNoRoom) {
			log.Printf("match: peek: %v", err)
		}
		notFound(w)
		return
	}

	seat, waiting := h.hub.SeatOf(room, s.UserID)
	writeJSON(w, http.StatusOK, roomPayload{
		ID:        room.ID,
		YourColor: colorCode(seat),
		HostName:  room.HostName(),
		Waiting:   waiting,
		IsHost:    room.IsHost(s.UserID),
	})
}

// colorCode 는 색을 화면·`games.my_color` 와 같은 어휘로 옮긴다.
func colorCode(c shogi.Color) string {
	if c == shogi.White {
		return "w"
	}
	return "b"
}
