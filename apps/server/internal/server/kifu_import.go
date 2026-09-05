package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/auth"
	"github.com/jovid18/show-gi/apps/server/internal/handicap"
	"github.com/jovid18/show-gi/apps/server/internal/kifu"
	"github.com/jovid18/show-gi/apps/server/internal/kifunorm"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 밖에서 둔 자기 기보를 취해 오는 표면. 근거와 정한 것은 journal §126.
//
// 두 단계다. 읽기(POST /api/kifu/parse)는 엔진도 DB도 안 쓰고 즉시 답하며, 취해 오기
// (POST /api/kifu/import)가 판을 만들어 줄에 세운다 — 잘못 읽은 기보에 엔진 몇 분을
// 쓰지 않기 위해서고, 그 사이에 사람이 手数와 앞뒤의 수를 눈으로 확인한다.
//
// 원문을 두 번 받는다. 서버에 중간 상태를 안 두기 위해서다 — 파싱이 결정적이라 같은
// 원문이 같은 결과를 주고, 그 한 번을 아끼자고 세션 표를 만들 값이 없다.
//
// **로그인한 사람만이다.** 익명끼리는 구별할 수단이 없어서(002_anonymous_games.sql)
// 「누구의 기보인가」에 답할 수가 없다.

// maxImportsPerDay 는 한 사람이 하루에 취해 올 수 있는 판 수다.
//
// 엔진 예산의 벽이다. 판 하나가 手数만큼의 판정이고 §91 실측으로 판당 2~8분이라,
// 이 값이 곧 「한 사람이 분석 대를 얼마나 오래 잡을 수 있나」다.
//
// [미확정] 표본으로 잡은 값이 아니다. 사람이 하루에 되짚고 싶은 판이 몇인지를 회차가
// 답하면 그때 옮긴다.
const maxImportsPerDay = 10

// maxImportPlies 는 한 판으로 받아들이는 手数의 상한이다.
//
// **결정적 파서에도 건다.** 정규화 계층에 같은 벽이 있지만(kifunorm.MaxMoves) 그쪽은
// 자기 응답을 묶는 것이고, 여기를 안 걸면 KIF 하나로 그 벽을 통째로 지나간다 — 千日手는
// shogi.ValidateMove 가 안 막으므로 합법 수순만으로 몇 천 手를 적을 수 있고, 그 판이
// 手数만큼의 엔진 판정을 줄에 세운다.
//
// 값을 실측으로 잡은 것이 아니다 [미확정]. 막으려는 것은 「사람이 둔 한 판」이 아니라
// 千日手를 이어 붙인 수순이고, 실사용에서 이 값에 닿는 기보가 나오면 그때 옮긴다.
const maxImportPlies = 512

// importPreviewHead·importPreviewTail 은 미리보기에 세우는 手数다.
//
// 앞뒤를 같이 보여 준다. 앞만 보여 주면 「뒤가 잘렸는가」를 사람이 알 수 없고, 그것이
// 취해 오기에서 가장 흔한 오류다.
const (
	importPreviewHead = 6
	importPreviewTail = 3
)

// importBodyMax 는 요청 몸통의 상한이다. 원문 상한(kifunorm.MaxInput)보다 넉넉하다 —
// JSON 이스케이프와 나머지 칸이 원문 위에 얹히므로, 딱 맞게 걸면 상한 안쪽의 기보가
// 길이 때문에 거절된다.
const importBodyMax = 4*kifunorm.MaxInput + 1<<10

type kifuHandler struct {
	store    *store.Store
	auth     *authHandler
	norm     *kifunorm.Client
	analyzer *matchAnalyzer
	// budget 은 정규화를 부르는 횟수의 벽이다. 하루 몫이 판을 세는 자리라 이쪽을
	// 안 막는다(kifu_budget.go).
	budget *hourlyBudget
	// cached 는 방금 옮겨 적은 결과다. 미리보기에서 확인한 판이 취해 오는 판과 같아야
	// 하고, 정규화는 다시 물으면 같은 답을 준다는 보장이 없다(kifu_budget.go).
	cached *transcribeCache
}

// importRequest 는 두 뿌리가 같이 쓰는 몸통이다. parse 는 Text 만 본다.
type importRequest struct {
	Text string `json:"text"`
	// MyColor 는 그 판에서 자기 자리다. "b"(先手·下手) 또는 "w"(後手·上手).
	MyColor string `json:"myColor"`
	// Result 는 기보가 결과를 안 말할 때 사람이 고른 값이다. "win"·"loss"·"draw".
	//
	// 기보가 말하면 그쪽이 이긴다. 사람이 자기 승패를 잘못 고르는 것보다 기록이 맞다.
	Result string `json:"result"`
}

// importPreview 는 읽은 결과다. 판은 아직 안 만들어졌다.
type importPreview struct {
	Plies int `json:"plies"`
	// HandicapJa 는 手合割 이름이다. 平手면 안 보낸다 — 되짚기의 같은 칸과 규약이 같다.
	HandicapJa string `json:"handicapJa,omitempty"`
	Sente      string `json:"sente,omitempty"`
	Gote       string `json:"gote,omitempty"`
	// Result 는 기보가 말한 결과다. "sente"·"gote"·"draw", 안 말하면 빈 값이고 그때
	// 화면이 사람에게 묻는다.
	Result string `json:"result,omitempty"`
	// Transcribed 는 결정적 파서가 못 읽어 정규화 계층을 지났는가다. 참이면 화면이
	// 「AI が書式を読み取りました」를 한 줄 붙인다 — 사람이 확인할 수 있게 하는 것이
	// 지어내기에 대한 두 번째 방어다(첫 번째는 룰 엔진의 전수 검증).
	Transcribed bool `json:"transcribed"`
	// Head·Tail 은 棋譜 표기의 앞뒤다. 사람이 자기 판인지 알아보는 유일한 단서다.
	Head []string `json:"head"`
	Tail []string `json:"tail,omitempty"`
}

func (h *kifuHandler) parse(w http.ResponseWriter, r *http.Request) {
	s, ok := h.viewer(w, r)
	if !ok {
		return
	}
	req, ok := decodeImport(w, r)
	if !ok {
		return
	}

	g, notation, err := h.read(r.Context(), s.UserID, req.Text)
	if err != nil {
		writeImportError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, previewOf(g, notation))
}

// importResponse 는 취해 온 판의 번호다. 화면이 그 자리에서 되짚기로 옮겨 간다.
type importResponse struct {
	GameID int64 `json:"gameId"`
}

func (h *kifuHandler) create(w http.ResponseWriter, r *http.Request) {
	s, ok := h.viewer(w, r)
	if !ok {
		return
	}
	req, ok := decodeImport(w, r)
	if !ok {
		return
	}
	color, ok := colorCodeOf(req.MyColor)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "color", "message": "先手か後手かを選んでください。",
		})
		return
	}

	// 벽이 먼저다. 읽기는 값이 들고(정규화 계층은 돈을 쓴다) 그 뒤가 엔진 몇 분이라,
	// 넘긴 요청은 아무것도 하기 전에 돌려보낸다.
	if n, err := h.store.CountImportsSince(r.Context(), s.UserID, time.Now().Add(-24*time.Hour)); err != nil {
		log.Printf("kifu: could not count today's imports for %d: %v", s.UserID, err)
	} else if n >= maxImportsPerDay {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error":   "quota",
			"message": fmt.Sprintf("棋譜の取り込みは1日%d局までです。明日またお試しください。", maxImportsPerDay),
		})
		return
	}

	g, notation, err := h.read(r.Context(), s.UserID, req.Text)
	if err != nil {
		writeImportError(w, err)
		return
	}

	result, ok := importedResultOf(g.Result, color, req.Result)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "result", "message": "この棋譜には結果が書かれていません。勝ち・負け・引き分けを選んでください。",
		})
		return
	}

	gameID, err := h.save(r.Context(), s.UserID, color, string(notation), g, result)
	if err != nil {
		log.Printf("kifu: could not save the imported game of %d: %v", s.UserID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "棋譜を保存できませんでした。",
		})
		return
	}

	// 줄에 세우는 데 실패해도 판은 남는다. 평가치 없는 판이 되짚기에 그대로 뜨고,
	// 사람은 자기 기보를 잃지 않는다 — 다시 취해 오면 새 판으로 다시 선다.
	if err := h.analyzer.enqueueImport(r.Context(), gameID, g.StartSFEN, g.Moves); err != nil {
		log.Printf("kifu: could not queue imported game %d: %v", gameID, err)
	}

	writeJSON(w, http.StatusOK, importResponse{GameID: gameID})
}

// read 는 결정적 파서를 먼저 대 보고, 전부 실패했을 때만 정규화 계층을 부른다.
//
// **순서가 이 기능의 전제다.** 같은 기보가 언제나 같은 결과를 주는 것이 기본값이고,
// 정규화는 그 기본값이 성립하지 않는 자리에만 선다(internal/kifunorm).
func (h *kifuHandler) read(ctx context.Context, userID int64, text string) (kifu.ParsedGame, kifu.Notation, error) {
	if len(text) > kifunorm.MaxInput {
		return kifu.ParsedGame{}, "", kifunorm.ErrTooLarge
	}
	g, notation, err := kifu.Read(text)
	if err == nil {
		return g, notation, plyLimit(g)
	}
	if h.norm == nil {
		return kifu.ParsedGame{}, "", err
	}

	// 방금 옮겨 적은 것이 있으면 그것을 쓴다. 미리보기와 취해 오기가 원문을 두 번
	// 보내는데(위 패키지 주석) 「같은 원문이면 같은 결과」가 이 계층에는 없어서,
	// 다시 물으면 사람이 확인한 것과 다른 판이 들어올 수 있다.
	if got, ok := h.cached.get(userID, text); ok {
		return replay(got)
	}

	// 부르기 전에 몫을 센다. 여기서 막히면 결정적 파서가 낸 오류가 그대로 나가고,
	// 사람에게는 「읽을 수 없는 기보」와 같은 화면이다 — 벽에 닿았다는 것을 알려 줄
	// 값이 없다(알려 주면 그것이 곧 「다시 시도하면 된다」로 읽힌다).
	if !h.budget.take(userID) {
		log.Printf("kifu: user %d is over the transcription budget", userID)
		return kifu.ParsedGame{}, "", err
	}

	got, normErr := h.norm.Normalize(ctx, text)
	if normErr != nil {
		log.Printf("kifu: could not transcribe: %v", normErr)
		// 결정적 파서가 낸 오류를 돌려준다. 그쪽이 「몇 手目가 이상한가」를 알고,
		// 정규화의 실패는 사람이 고칠 수 있는 것이 아니다.
		return kifu.ParsedGame{}, "", err
	}
	log.Printf("kifu: transcribed with %s: %d tokens, %d moves", h.norm.Model(), got.Tokens, len(got.Moves))
	h.cached.put(userID, text, got)
	return replay(got)
}

// replay 는 옮겨 적은 표기를 룰 엔진으로 지나 판을 만든다.
//
// **여기가 정규화 계층의 출력이 수가 되는 유일한 문이다.** 캐시에서 온 것도 같은 문을
// 지난다 — 옮겨 적은 글자를 들고 있는 것이고, 수를 들고 있는 것이 아니다.
//
// 오류는 룰 엔진의 것을 그대로 돌려준다. 「몇 手目가 이상한가」를 아는 것이 그쪽이다.
func replay(got kifunorm.Result) (kifu.ParsedGame, kifu.Notation, error) {
	g, err := kifu.ParseMoves(got.Handicap, got.Moves)
	if err != nil {
		return kifu.ParsedGame{}, "", err
	}
	// 이름은 옮겨 적는 쪽이 낸 값이다. 길이를 잘라 든다 — 스키마가 모양만 묶고 크기는
	// 안 묶어서, 그대로 두면 남의 응답 하나가 미리보기의 크기를 정한다.
	g.Sente, g.Gote = clip(got.Sente), clip(got.Gote)
	if g.Result == kifu.ResultUnknown {
		g.Result = resultFromWord(got.Result)
	}
	return g, kifu.NotationLLM, plyLimit(g)
}

// plyLimit 은 한 판의 手数가 상한 안인가다.
func plyLimit(g kifu.ParsedGame) error {
	if len(g.Moves) > maxImportPlies {
		return errTooManyPlies
	}
	return nil
}

// errTooManyPlies 는 한 판으로 보기에 너무 긴 수순이다.
var errTooManyPlies = errors.New("kifu: too many plies for one game")

// maxNameRunes 는 미리보기에 싣는 이름의 길이다. 대국자 이름이 이보다 길 일이 없다.
const maxNameRunes = 64

func clip(v string) string {
	r := []rune(v)
	if len(r) <= maxNameRunes {
		return v
	}
	return string(r[:maxNameRunes])
}

// save 는 판과 기보를 남긴다. 평가치는 아직 비어 있다 — 채우는 것은 분석 워커다.
func (h *kifuHandler) save(
	ctx context.Context, userID int64, color, notation string, g kifu.ParsedGame, result store.GameResult,
) (int64, error) {
	gameID, err := h.store.CreateImportedGame(ctx, userID, color, g.StartSFEN, notation)
	if err != nil {
		return 0, err
	}
	for i, m := range g.Moves {
		if err := h.store.InsertMove(ctx, gameID, i+1, m); err != nil {
			return 0, err
		}
	}
	// 결과를 여기서 적는다. 되짚기 목록에 뜨는 조건이 result IN (win, loss, draw) 라,
	// 안 적으면 방금 취해 온 판이 어디에도 안 보인다.
	if err := h.store.FinishGame(ctx, gameID, result); err != nil {
		return 0, err
	}
	return gameID, nil
}

// viewer 는 로그인한 사람이다. 아니면 401 을 적고 false 다.
func (h *kifuHandler) viewer(w http.ResponseWriter, r *http.Request) (auth.Session, bool) {
	s, ok := h.auth.viewer(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "unauthorized", "message": "ログインが必要です。",
		})
	}
	return s, ok
}

func decodeImport(w http.ResponseWriter, r *http.Request) (importRequest, bool) {
	var req importRequest
	// 원문 상한을 몸통에서 건다. 여기서 안 걸면 남이 이 프로세스의 메모리를 정한다.
	//
	// 여유를 준다. JSON 이스케이프와 나머지 칸이 원문보다 크므로, 딱 맞게 걸면 상한
	// 안쪽의 기보가 「형식이 틀렸다」로 거절된다.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, importBodyMax)).Decode(&req); err != nil {
		// 넘쳐서 끊긴 것은 「못 읽었다」가 아니다. 같은 문장을 주면 사람이 형식을
		// 고치려 들고, 고칠 것은 길이다.
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeImportError(w, kifunorm.ErrTooLarge)
			return importRequest{}, false
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "body", "message": "棋譜を読み取れませんでした。",
		})
		return importRequest{}, false
	}
	if strings.TrimSpace(req.Text) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "empty", "message": "棋譜を貼り付けてください。",
		})
		return importRequest{}, false
	}
	return req, true
}

// writeImportError 는 못 읽은 이유를 일본어 한 줄로 만든다.
//
// 手数를 말하는 것이 이 함수의 값이다. 「読み取れませんでした」만으로는 사람이 자기
// 기보의 어디를 고쳐야 하는지 모른다(kifu.MoveError).
func writeImportError(w http.ResponseWriter, err error) {
	var me *kifu.MoveError
	switch {
	case errors.Is(err, errTooManyPlies):
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "too_many_plies",
			"message": fmt.Sprintf("手数が多すぎます。1局分（%d手まで）を貼り付けてください。", maxImportPlies),
		})
	case errors.Is(err, kifunorm.ErrTooLarge):
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "too_large", "message": "棋譜が大きすぎます。1局分だけ貼り付けてください。",
		})
	case errors.As(err, &me):
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "move",
			"ply":     me.Ply,
			"message": jaMoveError(me.Ply),
		})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "unreadable",
			"message": "棋譜を読み取れませんでした。KIF・KI2・CSA・USI のいずれかの形式で貼り付けてください。",
		})
	}
}

// jaMoveError 는 몇 手目에서 멈췄는지를 말한다. 「そこまでは読めた」를 같이 말하는 것이
// 사람이 고칠 자리를 찾는 유일한 단서다.
func jaMoveError(ply int) string {
	return fmt.Sprintf("%d手目を読み取れませんでした。その手の書き方をご確認ください。", ply)
}

// previewOf 는 읽은 것을 미리보기로 옮긴다.
//
// 표기를 여기서 만든다. 원문의 표기를 그대로 돌려주면 정규화를 지난 판과 안 지난 판이
// 화면에서 다른 모양이 되고, 사람이 「이게 내가 올린 그 판인가」를 표기로 확인할 수 없다.
func previewOf(g kifu.ParsedGame, notation kifu.Notation) importPreview {
	out := importPreview{
		Plies:       len(g.Moves),
		HandicapJa:  handicap.NameOf(g.StartSFEN),
		Sente:       g.Sente,
		Gote:        g.Gote,
		Result:      resultWord(g.Result),
		Transcribed: notation == kifu.NotationLLM,
		Head:        []string{},
	}

	ja := lineJa(g)
	if len(ja) <= importPreviewHead+importPreviewTail {
		out.Head = ja
		return out
	}
	out.Head = ja[:importPreviewHead]
	out.Tail = ja[len(ja)-importPreviewTail:]
	return out
}

// lineJa 는 수순을 棋譜 표기로 옮긴다. 못 옮기면 빈 목록이다 — 미리보기가 없는 것은
// 手数만 보고 취해 오는 화면이고, 그것 때문에 임포트를 막지는 않는다.
func lineJa(g kifu.ParsedGame) []string {
	pos, err := shogi.ParseSFEN(g.StartSFEN)
	if err != nil {
		return []string{}
	}
	ja, err := pos.LineJa(g.Moves)
	if err != nil {
		return []string{}
	}
	return ja
}

// colorCodeOf 는 사람이 고른 자리를 기록의 한 글자로 옮긴다.
func colorCodeOf(v string) (string, bool) {
	switch v {
	case "b", "w":
		return v, true
	}
	return "", false
}

// resultWord 는 기보가 말한 결과다. 안 말하면 빈 값이고, 그때 화면이 사람에게 묻는다.
func resultWord(r kifu.GameResult) string {
	switch r {
	case kifu.ResultSenteWin:
		return "sente"
	case kifu.ResultGoteWin:
		return "gote"
	case kifu.ResultDraw:
		return "draw"
	}
	return ""
}

// resultFromWord 는 그 반대 방향이다. 정규화 계층이 낸 낱말을 받는다.
func resultFromWord(v string) kifu.GameResult {
	switch v {
	case "sente":
		return kifu.ResultSenteWin
	case "gote":
		return kifu.ResultGoteWin
	case "draw":
		return kifu.ResultDraw
	}
	return kifu.ResultUnknown
}

// importedResultOf 는 기록에 적을 결과를 정한다. games.result 는 **주인 관점**이라 기보의
// 先手/後手 승패를 자리로 뒤집는다 — 안 뒤집으면 後手로 둔 판의 승패가 통째로 반대가 된다.
//
// 기보가 말하면 그쪽이 이긴다. 사람이 자기 승패를 잘못 고르는 것보다 기록이 맞고,
// 안 말할 때만 사람이 고른 값을 쓴다.
func importedResultOf(fromKifu kifu.GameResult, color, chosen string) (store.GameResult, bool) {
	switch fromKifu {
	case kifu.ResultDraw:
		return store.ResultDraw, true
	case kifu.ResultSenteWin:
		if color == "b" {
			return store.ResultWin, true
		}
		return store.ResultLoss, true
	case kifu.ResultGoteWin:
		if color == "w" {
			return store.ResultWin, true
		}
		return store.ResultLoss, true
	}

	switch store.GameResult(chosen) {
	case store.ResultWin:
		return store.ResultWin, true
	case store.ResultLoss:
		return store.ResultLoss, true
	case store.ResultDraw:
		return store.ResultDraw, true
	}
	// 중단된 판은 안 받는다. 되짚기 목록에 뜨는 조건이 셋 중 하나라(query/games.sql)
	// abandoned 로 적으면 취해 온 판이 어디에도 안 보인다.
	return "", false
}

// kifuImportUnavailable 은 취해 오기가 꺼져 있는 배포의 답이다. DB나 엔진이 없는 자리이고,
// 화면은 /healthz 를 보고 그 줄을 미리 감춘다.
func kifuImportUnavailable(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"error": "import_unavailable", "message": "いまは棋譜を取り込めません。",
	})
}
