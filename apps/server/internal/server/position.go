package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/jovid18/show-gi/apps/server/internal/auth"
	"github.com/jovid18/show-gi/apps/server/internal/boardread"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// 판이 찍힌 그림에서 국면을 취해 오는 표면. 근거와 정한 것은 journal §129.
//
// 두 뿌리다. 읽기(POST /api/position/read)가 그림을 국면 하나로 옮기고, 검사
// (POST /api/position/check)가 「이 국면이 성립하는가」에 답한다 — 확인 화면이 한 칸을
// 고칠 때마다 후자를 부르므로, 二歩가 되는 순간 그 자리에서 보인다.
//
// **검사가 엔진을 안 쓴다.** 순수 룰 계산이라 슬롯도 로그인도 필요 없고, 그래서 편집이
// 얼마나 잦아도 대국이 쓰는 풀에 닿지 않는다.
//
// 그림을 안 남긴다. 요청 하나에 실려 저쪽 API 로 나가고 응답을 만든 뒤 버린다 —
// 남겨 두면 「사람이 올린 사진」이라는 지울 규약이 하나 더 생기고, 그 값이 없다.
//
// **읽기는 로그인한 사람만이다.** 지키는 것이 돈이라 사람마다 세야 하고, 익명끼리는
// 구별할 수단이 없다(002_anonymous_games.sql). 검사와 분석에는 그 벽이 없다.

// maxBoardReadsPerHour 는 한 사람이 한 시간에 그림을 읽힐 수 있는 횟수다.
//
// 기보 정규화(maxTranscribesPerHour=20)보다 빡빡하다. 그림 한 장이 큰 해상도로 보는
// 호출이라 한 번의 값이 저쪽보다 크고, 여기서는 「형식을 고쳐 가며 다시 붙여 넣는」
// 정상적인 반복이 없다 — 같은 사진을 다시 읽혀도 같은 판이 나온다.
//
// [미확정] 실측이 아니다. 사람이 한 판을 읽히는 데 몇 장을 올리는지를 회차가 답하면 옮긴다.
const maxBoardReadsPerHour = 10

// positionBodyMax 는 읽기 요청 몸통의 상한이다.
//
// base64 가 원본의 4/3이고 그 위에 JSON 이스케이프와 data: 앞머리가 얹힌다. 딱 맞게
// 걸면 상한 안쪽의 사진이 길이 때문에 거절된다(importBodyMax 와 같은 이유).
const positionBodyMax = boardread.MaxImage/3*4 + 1<<10

// positionCheckBodyMax 는 검사 요청 몸통의 상한이다. SFEN 한 줄이 전부다.
const positionCheckBodyMax = 4 << 10

type positionHandler struct {
	auth *authHandler
	// read 는 그림을 읽는 창구다. 키가 없으면 nil 이고, 그때 이 표면은 안 열린다.
	read   *boardread.Client
	budget *hourlyBudget
}

// positionReadRequest 는 그림 한 장이다.
type positionReadRequest struct {
	// Image 는 base64 다. 브라우저가 주는 data: URL 앞머리가 붙어 있어도 받는다 —
	// 화면이 그 앞머리를 떼는 코드를 갖는 것보다 여기서 떼는 편이 낫다.
	Image string `json:"image"`
}

// positionCheckRequest 는 국면 하나다. 手番이 그 안에 들어 있다.
type positionCheckRequest struct {
	SFEN string `json:"sfen"`
}

// positionResponse 는 국면 하나와 그것에 대해 룰 엔진이 말할 수 있는 전부다.
//
// 읽기와 검사가 같은 모양을 준다. 확인 화면이 「방금 읽은 판」과 「내가 고친 판」을
// 같은 코드로 그리는 자리이고, 갈라 두면 그 화면에 표가 둘 생긴다.
type positionResponse struct {
	SFEN string `json:"sfen"`
	// Faults 는 이 국면이 어긴 규칙 전부다. 비어 있어야 분석으로 넘어갈 수 있다.
	Faults []positionFault `json:"faults"`
	// Warnings 는 거절이 아닌 것들이다. 말이 몇 장 모자라다·이미 詰んでいる.
	Warnings []string `json:"warnings"`
}

// positionFault 는 어긴 규칙 하나다.
type positionFault struct {
	// Reason 은 사유의 영어 이름이다. 화면이 분기할 자리가 생기면 이것을 본다.
	Reason string `json:"reason"`
	// Square 는 화면 배열 인덱스(0~80)다. 칸으로 짚을 수 없는 사유면 안 온다 —
	// 판 위의 좌표 규약이 서버와 화면에서 같으므로(internal/shogi 패키지 doc ·
	// models/sfen.ts) 변환이 없다.
	Square *int `json:"square,omitempty"`
	// Message 는 화면에 그대로 나가는 일본어다. 화면이 문장을 만들지 않는다.
	Message string `json:"message"`
}

func (h *positionHandler) readImage(w http.ResponseWriter, r *http.Request) {
	s, ok := h.viewer(w, r)
	if !ok {
		return
	}

	var req positionReadRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, positionBodyMax)).Decode(&req); err != nil {
		// 넘쳐서 끊긴 것은 「못 읽었다」가 아니다. 고칠 것이 형식이 아니라 크기다.
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeBoardReadError(w, boardread.ErrTooLarge)
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_request", "message": "リクエストを読み取れませんでした。",
		})
		return
	}

	image, err := decodeImage(req.Image)
	if err != nil {
		writeBoardReadError(w, err)
		return
	}

	// 벽이 먼저다. 부른 뒤에 세면 시한에 걸린 호출이 몫을 안 쓰는데, 그 실패가 가장
	// 비싼 호출이다(hourlyBudget.take).
	if !h.budget.take(s.UserID) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error": "quota",
			"message": fmt.Sprintf(
				"画像から局面を読み取れるのは1時間に%d回までです。しばらくしてからお試しください。",
				maxBoardReadsPerHour,
			),
		})
		return
	}

	got, err := h.read.Read(r.Context(), image)
	if err != nil {
		// 키·크기·형식·판 없음은 사람이 고칠 수 있는 것이라 사유를 가른다. 나머지는
		// 저쪽 API 의 사정이므로 로그로 남기고 「다시 눌러 볼 수 있는 실패」로 낸다.
		if !isBoardReadRefusal(err) {
			log.Printf("position: could not read the image of %d (%s): %v", s.UserID, h.read.Model(), err)
		}
		writeBoardReadError(w, err)
		return
	}
	log.Printf("position: read an image for %d with %s in %d tokens", s.UserID, h.read.Model(), got.Tokens)

	writeJSON(w, http.StatusOK, checked(got.SFEN))
}

func (h *positionHandler) check(w http.ResponseWriter, r *http.Request) {
	var req positionCheckRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, positionCheckBodyMax)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_request", "message": "リクエストを読み取れませんでした。",
		})
		return
	}
	if _, err := shogi.ParseSFEN(req.SFEN); err != nil {
		// SFEN 자체가 안 읽히는 것은 화면의 버그다. 사람이 편집기에서 만들 수 있는
		// 모양이 아니라, 문구도 그 사실대로 둔다.
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_sfen", "message": "局面を読み取れませんでした。",
		})
		return
	}
	writeJSON(w, http.StatusOK, checked(req.SFEN))
}

func (h *positionHandler) viewer(w http.ResponseWriter, r *http.Request) (auth.Session, bool) {
	s, ok := h.auth.viewer(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "unauthorized", "message": "ログインが必要です。",
		})
	}
	return s, ok
}

// checked 는 국면 하나에 룰 엔진이 말할 수 있는 것을 붙인다.
//
// 못 읽는 SFEN 이 여기 오면 사유 없이 그대로 돌려준다 — 부르는 쪽이 이미 읽어 본
// 문자열이라, 여기서 두 번째 오류 경로를 만들면 어느 쪽이 진실인지가 흐려진다.
func checked(sfen string) positionResponse {
	res := positionResponse{SFEN: sfen, Faults: []positionFault{}, Warnings: []string{}}

	pos, err := shogi.ParseSFEN(sfen)
	if err != nil {
		return res
	}

	for _, f := range pos.Faults() {
		out := positionFault{Reason: f.Reason.String(), Message: f.Message()}
		if f.Square >= 0 {
			sq := f.Square
			out.Square = &sq
		}
		res.Faults = append(res.Faults, out)
	}

	// 말이 모자란 것은 거절이 아니다. 실물 한 판은 언제나 40장이라 이것이 곧 「한 장을
	// 놓쳤다」의 신호이지만, 駒台가 잘려 나간 사진도 정상이다.
	if short := pos.InventoryShortage(); len(short) > 0 {
		res.Warnings = append(res.Warnings, shortageJa(short))
	}

	// 이미 끝난 국면인지는 사유가 없을 때만 묻는다. 성립하지 않는 판의 합법수는
	// 물어봐야 뜻이 없다.
	if len(res.Faults) == 0 && pos.NoLegalMoves() {
		if pos.InCheck(pos.Turn) {
			res.Warnings = append(res.Warnings, "この局面はすでに詰んでいます。指す手がありません。")
		} else {
			res.Warnings = append(res.Warnings, "この局面には指せる手がありません。")
		}
	}

	return res
}

// shortageJa 는 모자란 말을 한 문장으로 적는다.
//
// 종류 순으로 돈다. 맵을 그대로 훑으면 같은 판이 요청마다 다른 순서의 문장을 준다.
func shortageJa(short map[shogi.PieceType]int) string {
	var parts []string
	total := 0
	for t := shogi.Pawn; t <= shogi.King; t++ {
		if n, ok := short[t]; ok {
			parts = append(parts, fmt.Sprintf("%s%d枚", shogi.PieceJa(t), n))
			total += n
		}
	}
	return fmt.Sprintf("駒が%d枚足りません（%s）。読み取れなかった駒がないか確かめてください。", total, strings.Join(parts, "・"))
}

// decodeImage 는 base64 를 바이트로 푼다. data: 앞머리가 붙어 있으면 떼고 본다.
func decodeImage(s string) ([]byte, error) {
	if s == "" {
		return nil, boardread.ErrNotImage
	}
	if strings.HasPrefix(s, "data:") {
		i := strings.Index(s, ",")
		if i < 0 {
			return nil, boardread.ErrNotImage
		}
		s = s[i+1:]
	}
	// 줄바꿈이 섞여 오는 base64 가 있다. 떼지 않으면 그림 전체가 「형식이 아니다」로 죽는다.
	s = strings.NewReplacer("\n", "", "\r", "").Replace(s)
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, boardread.ErrNotImage
	}
	if len(b) > boardread.MaxImage {
		return nil, boardread.ErrTooLarge
	}
	return b, nil
}

// boardReadMessages 는 사람이 고칠 수 있는 실패의 문구다.
var boardReadMessages = map[error]struct {
	status  int
	code    string
	message string
}{
	boardread.ErrDisabled: {
		http.StatusServiceUnavailable, "unavailable",
		"画像からの読み取りはいま利用できません。",
	},
	boardread.ErrTooLarge: {
		http.StatusRequestEntityTooLarge, "too_large",
		"画像が大きすぎます。6MB までのスクリーンショットをお使いください。",
	},
	boardread.ErrNotImage: {
		http.StatusBadRequest, "not_image",
		"PNG・JPEG・WebP の画像を選んでください。",
	},
	boardread.ErrNoBoard: {
		http.StatusUnprocessableEntity, "no_board",
		"画像から将棋盤を見つけられませんでした。盤全体が写るように切り取ってお試しください。",
	},
}

func isBoardReadRefusal(err error) bool {
	for known := range boardReadMessages {
		if errors.Is(err, known) {
			return true
		}
	}
	return false
}

func writeBoardReadError(w http.ResponseWriter, err error) {
	for known, msg := range boardReadMessages {
		if errors.Is(err, known) {
			writeJSON(w, msg.status, map[string]any{"error": msg.code, "message": msg.message})
			return
		}
	}
	// 남은 것은 저쪽 API 의 사정이다. 사람이 고칠 것이 없으므로 다시 눌러 볼 수 있는
	// 실패로 낸다 — 국면은 아직 아무것도 안 만들어졌으니 잃는 것이 없다.
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"error":   "read_failed",
		"message": "画像から局面を読み取れませんでした。もう一度お試しください。",
	})
}

// boardReadUnavailable 은 키가 없어 이 표면이 안 열린 자리다.
func boardReadUnavailable(w http.ResponseWriter, _ *http.Request) {
	writeBoardReadError(w, boardread.ErrDisabled)
}
