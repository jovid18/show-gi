package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/jovid18/show-gi/apps/server/internal/quiz"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 되짚기 퀴즈의 표면. **엔진이 없다** — 문항은 판이 끝나는 자리에서 이미 만들어져 있고
// (ws.go generateQuiz) 여기는 그것을 읽어 채점만 한다(06-status.md §53).
//
// 그래서 이 표면은 `/api/games/{id}` 와 같은 성질이다: DB에 매여 있고 엔진과 무관하다.
// 가정 수순처럼 503이 되는 자리가 없다.

// quizBodyLimit 은 본문 상한이다. 詰み 문항의 수순 하나가 전부라 이보다 클 이유가 없다.
const quizBodyLimit = 4 << 10

// quizHandler 는 문항을 내주고 채점한다.
type quizHandler struct {
	// review 는 **기록을 읽는 문**이다. 같은 함수를 지나야 「자기 판만」과 「끝난 판만」이
	// 한 자리에 남는다(review.go record) — 거르는 규칙이 두 벌이 되면 한쪽만 고쳐진 채로
	// 남고, 그 한쪽이 곧 구멍이다.
	review *reviewHandler
}

// quizPayload 는 화면이 받는 문항 전부다. **정답이 없다** — 채점은 서버에 있다.
type quizPayload struct {
	// Ready 는 생성이 끝났는가다. **거짓은 「아직 만드는 중」이고 「문항이 없다」와 다르다** —
	// 만드는 데 수십 초가 걸려서, 그 사이에 화면이 「問題はありません」을 그리면 거짓이 된다.
	//
	// 판이 끝나는 자리에서 문항이 하나도 안 나와도 행을 남기는 것이 이 값을 위해서다(ws.go).
	Ready bool          `json:"ready"`
	Mate  *matePayload  `json:"mate,omitempty"`
	Best  []bestPayload `json:"best,omitempty"`
}

// matePayload 는 詰み 문항의 첫 장면이다.
type matePayload struct {
	// Ply 는 문제 국면이 만들어진 手数다. 화면이 「○手目」로 그 판의 어디였는지 말한다.
	Ply  int    `json:"ply"`
	SFEN string `json:"sfen"`
	// Plies 는 詰みまでの手数다. **문항의 일부다** — 몇 手인지를 모르면 詰将棋는 풀 수 없다.
	Plies int `json:"plies"`
	// Converted 는 사람이 그 詰み을 대국에서 실제로 決めた가. 화면의 문장이 여기서 갈린다 —
	// 놓친 詰み과 決めた 詰み은 사람에게 전혀 다른 이야기다.
	Converted bool `json:"converted"`
	// LegalMoves 는 **王手인 수만**이다. 詰将棋에서 攻方은 매 수 王手를 걸어야 하므로
	// 그 밖은 애초에 문항의 입력이 아니다 — 화면은 이 배열만 빛낸다.
	LegalMoves []string `json:"legalMoves"`
	// Checked 는 王手를 받고 있는 玉의 칸이다.
	//
	// **詰ます 쪽이 자기도 王手를 받고 있을 수 있다** — 王手를 풀면서 거는 수만 남은 국면이
	// 그렇다. 이 칸을 안 보내면 첫 수를 둘 때까지 판이 그 사실을 안 그리다가 갑자기 그린다.
	// 화면은 규칙을 모르므로 이것도 서버가 준다(replay.go checkedSquare).
	Checked string `json:"checked,omitempty"`
}

// bestPayload 는 「この局面の最善手は?」 문항 하나다.
type bestPayload struct {
	// Index 는 채점 요청이 어느 문항인지 가리키는 값이다. 手数로 가리키지 않는 이유는
	// 문항이 뽑히는 기준이 바뀌어도 이 값의 뜻은 「목록의 몇 번째」로 안 바뀌기 때문이다.
	Index int    `json:"index"`
	Ply   int    `json:"ply"`
	SFEN  string `json:"sfen"`
	// LegalMoves 는 합법수 전부다. **王手로 좁히지 않는다** — 그 규약은 詰み 문항만의 것이다.
	LegalMoves []string `json:"legalMoves"`
	// Checked 는 王手를 받고 있는 玉의 칸이다. 이 문항은 王手 중일 수도 있다.
	Checked string `json:"checked,omitempty"`
}

// get 은 그 판의 문항을 준다. 문항이 없으면 **빈 몸통이고 404가 아니다** — 「문항이 없는
// 판」은 흔한 결과이고(10수 만에 投了한 판), 실패로 답하면 화면이 고장과 구별하지 못한다.
func (h *quizHandler) get(w http.ResponseWriter, r *http.Request) {
	rec, ok := h.review.record(w, r)
	if !ok {
		return
	}
	q, ready, ok := h.load(w, r, rec.ID)
	if !ok {
		return
	}

	out := quizPayload{Ready: ready}
	if q.Mate != nil {
		// **트리의 키가 곧 王手 목록이다.** 여기서 룰 엔진으로 다시 걸러 만들면 화면이
		// 빛내는 수와 트리가 아는 수가 갈릴 수 있고, 갈리면 화면에서 둘 수 있는 수가
		// 서버에서 거절된다.
		legal := rootChecks(*q.Mate)

		// **둘 수 있는 수가 없으면 문항을 안 보낸다.** 詰み이 있는 국면에는 王手가 반드시
		// 하나는 있으므로 여기가 비는 것은 트리가 깨진 것이고, 그때 카드를 그리면 사람은
		// 누를 수 없는 문제를 본다. 덤으로 `null` 이 나가는 길이 막힌다 — Go의 nil 슬라이스는
		// `[]` 가 아니라 `null` 로 직렬화되고, 화면이 그것을 순회하면 그 자리에서 죽는다
		// (protocol/whatif.ts 가 같은 함정을 이미 적어 뒀다).
		if len(legal) == 0 {
			log.Printf("quiz: game %d: the mate item at ply %d has no playable move", rec.ID, q.Mate.Ply)
		} else {
			out.Mate = &matePayload{
				Ply:        q.Mate.Ply,
				SFEN:       q.Mate.SFEN,
				Plies:      q.Mate.Plies,
				Converted:  q.Mate.Converted,
				LegalMoves: legal,
				Checked:    checkedAt(q.Mate.SFEN),
			}
		}
	}
	for i, b := range q.Best {
		pos, err := shogi.ParseSFEN(b.SFEN)
		if err != nil {
			log.Printf("quiz: game %d: best item %d sfen: %v", rec.ID, i, err)
			continue
		}
		legal, err := quiz.LegalMovesAt(b.SFEN)
		if err != nil {
			log.Printf("quiz: game %d: best item %d legal moves: %v", rec.ID, i, err)
			continue
		}
		out.Best = append(out.Best, bestPayload{
			Index: i, Ply: b.Ply, SFEN: b.SFEN, LegalMoves: legal, Checked: checkedSquare(pos),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// mateRequest 는 「지금까지 내가 낸 수들」이다.
//
// **玉方의 응수는 안 받는다.** 응수는 트리에 있어서 서버가 다시 만들 수 있고, 그러면
// 화면과 서버가 어긋날 수 없다 — 가정 수순이 상대 수까지 함께 받는 것과 갈리는 자리이고
// (whatif.go), 저쪽 응수는 엔진 탐색이라 같은 국면에서 같은 답이라는 보장이 없다.
type mateRequest struct {
	Moves []string `json:"moves"`
}

// mateResponse 는 채점 결과이자 다음 장면이다.
type mateResponse struct {
	// Line 은 판 위에서 진행된 수 전부다(내 수와 玉方 응수가 번갈아).
	Line []string `json:"line"`
	SFEN string   `json:"sfen"`
	// Defense·DefenseJa 는 직전 내 수에 玉方이 답한 수다. 화면이 그것을 짚어야 사람이
	// 무엇이 달라졌는지 안다.
	Defense   string `json:"defense,omitempty"`
	DefenseJa string `json:"defenseJa,omitempty"`
	// LegalMoves 는 다음에 둘 수 있는 王手들이다. 끝났으면 비어 있다.
	LegalMoves []string `json:"legalMoves,omitempty"`
	Checked    string   `json:"checked,omitempty"`
	// Plies 는 지금 국면에서 詰みまでの手数. 끝났으면 0.
	Plies int `json:"plies"`
	// Outcome 은 `ongoing` | `solved` | `wrong` | `not_check` 다.
	Outcome string `json:"outcome"`
	// Message 는 화면에 그대로 나가는 일본어다. **서버가 만든다** — 화면이 코드로 문장을
	// 짓기 시작하면 어휘가 두 벌이 된다(review.go 의 같은 규약).
	Message string `json:"message"`
	// Best·BestJa 는 오답일 때의 정답 수다.
	Best   string `json:"best,omitempty"`
	BestJa string `json:"bestJa,omitempty"`
}

// mate 는 詰み 문항을 채점한다.
func (h *quizHandler) mate(w http.ResponseWriter, r *http.Request) {
	rec, ok := h.review.record(w, r)
	if !ok {
		return
	}
	var req mateRequest
	if !decodeQuizBody(w, r, &req) {
		return
	}
	q, ready, ok := h.load(w, r, rec.ID)
	if !ok {
		return
	}
	// **「아직 안 만들어졌다」를 「없다」로 답하지 않는다.** 이 PR이 화면에서 내내 가르는
	// 그 둘이고(quizPayload.Ready), 채점 쪽만 뭉치면 늦게 온 요청 하나가 「이 판엔 詰み이
	// 없었다」는 거짓을 받는다.
	if !ready {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "not_ready", "message": "問題はまだ準備中です。",
		})
		return
	}
	if q.Mate == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "no_item", "message": "この対局には詰みの問題がありません。",
		})
		return
	}

	prog, err := quiz.GradeMate(*q.Mate, req.Moves)
	if errors.Is(err, quiz.ErrBadMove) {
		// **오답과 다르다.** 화면이 王手만 빛내므로 여기 오는 것은 프론트 버그이거나
		// 조작된 요청이고, 오답과 같은 응답으로 뭉치면 버그가 오답으로 위장해 안 보인다.
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_move", "message": "その手はこの局面で指せません。",
		})
		return
	}
	if err != nil {
		log.Printf("quiz: game %d: grade mate: %v", rec.ID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "問題を進められませんでした。",
		})
		return
	}

	// **`null` 을 안 보낸다.** Go의 nil 슬라이스는 `[]` 가 아니라 `null` 로 직렬화되고,
	// 화면이 그것을 순회하면 그 자리에서 죽는다(protocol/whatif.ts).
	line := prog.Line
	if line == nil {
		line = []string{}
	}
	out := mateResponse{
		Line:       line,
		SFEN:       prog.SFEN,
		Defense:    prog.Defense,
		LegalMoves: prog.Legal,
		Plies:      prog.Plies,
		Outcome:    string(prog.Outcome),
		Best:       prog.Best,
	}
	if pos, err := shogi.ParseSFEN(prog.SFEN); err == nil {
		out.Checked = checkedSquare(pos)
	}
	out.DefenseJa = jaOfLine(q.Mate.SFEN, prog.Line, prog.Defense != "")
	// **정답 수의 표기는 그 수가 성립하는 국면에서 만든다.** `prog.SFEN` 은 오답이면 그
	// 수만큼 나아가 있어서 거기서는 정답이 불법이고, 표기가 비면 문구에서 「正解は○でした」가
	// 통째로 빠진다(quiz.MateProgress.BestFrom).
	out.BestJa = jaAt(prog.BestFrom, prog.Best, lastTo(prog.BestPrev))
	out.Message = mateMessage(prog, out.BestJa)
	writeJSON(w, http.StatusOK, out)
}

// bestRequest 는 「이 문항에 이 수」다.
type bestRequest struct {
	Index int    `json:"index"`
	Move  string `json:"move"`
}

// bestResponse 는 채점 결과다. **정답과 두 cp를 여기서 처음 보낸다** — 문항에 실어 보내면
// 화면을 열자마자 답이 손에 있다.
type bestResponse struct {
	Correct  bool   `json:"correct"`
	Answer   string `json:"answer"`
	AnswerJa string `json:"answerJa,omitempty"`
	// AnswerCp·SecondCp 는 **사람 관점** cp다. 둘의 차가 이 문항이 뽑힌 기준이다.
	AnswerCp int `json:"answerCp"`
	SecondCp int `json:"secondCp"`
	// Played·PlayedJa 는 사람이 대국에서 실제로 둔 수다.
	Played   string `json:"played"`
	PlayedJa string `json:"playedJa,omitempty"`
	Message  string `json:"message"`
}

// best 는 「최선수는?」 문항을 채점한다. **엔진이 필요 없다** — 정답이 저장돼 있다.
func (h *quizHandler) best(w http.ResponseWriter, r *http.Request) {
	rec, ok := h.review.record(w, r)
	if !ok {
		return
	}
	var req bestRequest
	if !decodeQuizBody(w, r, &req) {
		return
	}
	q, ready, ok := h.load(w, r, rec.ID)
	if !ok {
		return
	}
	if !ready {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "not_ready", "message": "問題はまだ準備中です。",
		})
		return
	}
	if req.Index < 0 || req.Index >= len(q.Best) {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "no_item", "message": "その問題はありません。",
		})
		return
	}

	item := q.Best[req.Index]
	correct, err := quiz.GradeBest(item, req.Move)
	if errors.Is(err, quiz.ErrBadMove) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_move", "message": "その手はこの局面で指せません。",
		})
		return
	}
	if err != nil {
		log.Printf("quiz: game %d: grade best %d: %v", rec.ID, req.Index, err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "採点できませんでした。",
		})
		return
	}

	out := bestResponse{
		Correct:  correct,
		Answer:   item.Answer,
		AnswerCp: item.AnswerCp,
		SecondCp: item.SecondCp,
		Played:   item.Played,
	}
	out.AnswerJa = jaAt(item.SFEN, item.Answer, -1)
	out.PlayedJa = jaAt(item.SFEN, item.Played, -1)
	out.Message = bestMessage(out)
	writeJSON(w, http.StatusOK, out)
}

// load 는 저장된 문항을 읽는다.
//
// ready=false 는 **아직 안 만들어졌다**이고 실패가 아니다. ok=false 만 그 자리에서 답을 이미 썼다.
func (h *quizHandler) load(w http.ResponseWriter, r *http.Request, gameID int64) (q quiz.Quiz, ready, ok bool) {
	failed := func() (quiz.Quiz, bool, bool) {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "問題を読み込めませんでした。",
		})
		return quiz.Quiz{}, false, false
	}

	raw, err := h.review.store.GameQuiz(r.Context(), gameID, quiz.Version)
	if errors.Is(err, store.ErrNoQuiz) {
		return quiz.Quiz{}, false, true
	}
	if err != nil {
		log.Printf("quiz: game %d: read: %v", gameID, err)
		return failed()
	}
	if err := json.Unmarshal(raw, &q); err != nil {
		log.Printf("quiz: game %d: decode: %v", gameID, err)
		return failed()
	}
	return q, true, true
}

// decodeQuizBody 는 본문을 읽는다. 상한을 넘기거나 못 읽으면 그 자리에서 답하고 false 다.
func decodeQuizBody(w http.ResponseWriter, r *http.Request, into any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, quizBodyLimit))
	if err := dec.Decode(into); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_request", "message": "リクエストを読み取れませんでした。",
		})
		return false
	}
	return true
}

// rootChecks 는 문제 국면에서 트리가 아는 王手들이다. **수를 하나도 안 낸 채점**이 곧
// 첫 장면이라, 여는 자리와 진행하는 자리가 같은 함수를 지난다.
func rootChecks(item quiz.MateItem) []string {
	prog, err := quiz.GradeMate(item, nil)
	if err != nil {
		log.Printf("quiz: mate item at ply %d has no root node: %v", item.Ply, err)
		return nil
	}
	return prog.Legal
}

// checkedAt 은 그 국면에서 王手를 받고 있는 玉의 칸이다. 못 읽으면 빈 값.
func checkedAt(sfen string) string {
	pos, err := shogi.ParseSFEN(sfen)
	if err != nil {
		return ""
	}
	return checkedSquare(pos)
}

// jaAt 은 그 국면에서 그 수의 棋譜 표기다. 못 읽으면 빈 값 — 표기가 없어도 수는 사실이다.
func jaAt(sfen, usiMove string, prevTo int) string {
	if usiMove == "" {
		return ""
	}
	pos, err := shogi.ParseSFEN(sfen)
	if err != nil {
		return ""
	}
	_, ja, ok := advance(pos, prevTo, usiMove)
	if !ok {
		return ""
	}
	return ja
}

// jaOfLine 은 수순의 **마지막 수**의 표기다. 「同」 표기가 직전 수의 도착 칸을 보므로
// 처음부터 두어 와야 맞는다 — 마지막 국면에서 한 수만 두어 보면 「同」이 빠진다.
func jaOfLine(startSFEN string, line []string, want bool) string {
	if !want || len(line) == 0 {
		return ""
	}
	pos, err := shogi.ParseSFEN(startSFEN)
	if err != nil {
		return ""
	}
	prevTo, ja := -1, ""
	for _, u := range line {
		next, text, ok := advance(pos, prevTo, u)
		if !ok {
			return ""
		}
		pos, prevTo, ja = next, lastTo(u), text
	}
	return ja
}

// mateMessage 는 詰み 문항의 일본어 문장이다.
//
// **오답을 한 문장으로 뭉치지 않는다.** 「この手では詰みません」은 대부분 거짓이다 —
// 詰み이 9手로 늘어질 뿐 詰む 수도 있고, 그때 그 문장은 초심자가 검증할 수 없는 거짓이 된다.
func mateMessage(p quiz.MateProgress, bestJa string) string {
	switch p.Outcome {
	case quiz.MateSolved:
		return "詰みました。正解です。"

	case quiz.MateNotCheck:
		return "王手ではありません。詰将棋では一手ごとに王手をかけ続けます。"

	case quiz.MateWrong:
		// **「詰みません」이라고 단정하지 않는다.** `Rest == 0` 은 solver 가 자기 한계
		// (`ENGINE_MATE_PLIES`, 기본 11) 안에서 詰み을 못 찾았다는 뜻이고, 그 한계를 넘겨
		// 늘어난 詰み은 **여전히 강제된다** — 7手 뿌리에서 한 手 낭비하면 13手가 될 수 있다.
		// 그때 「詰みません」은 거짓이 되므로, 아는 만큼만 말한다.
		head := "この手では詰みが読めなくなります。"
		if p.Rest > 0 {
			head = fmt.Sprintf("詰みは残りますが、%d手に伸びてしまいます。", 2+p.Rest)
		}
		if bestJa != "" {
			return head + fmt.Sprintf("正解は%sでした。", bestJa)
		}
		return head

	default:
		// **수를 하나도 안 낸 요청에는 「正解です」라고 하지 않는다.** 문항을 여는 자리가
		// 바로 그 요청이고(rootChecks 가 빈 수순으로 채점을 부른다), 거기서 정답이라고
		// 말하면 아무것도 안 한 사람에게 맞혔다고 하는 셈이다.
		if len(p.Line) > 0 && p.Plies > 0 {
			return fmt.Sprintf("正解です。あと%d手で詰みます。", p.Plies)
		}
		return "王手を続けて詰ませてください。"
	}
}

// bestMessage 는 「최선수는?」 문항의 일본어 문장이다.
//
// 오답이어도 **그 판의 일로 되돌린다** — 사람이 실제로 둔 수를 함께 말하는 것이 이 문항이
// 문제집이 아니라 자기 기보인 이유다.
func bestMessage(r bestResponse) string {
	if r.Correct {
		return fmt.Sprintf("正解です。この局面はこの一手で、次善手とは%dの差があります。", r.AnswerCp-r.SecondCp)
	}
	if r.AnswerJa == "" {
		return "不正解です。"
	}
	if r.PlayedJa != "" && r.Played != r.Answer {
		return fmt.Sprintf("不正解です。正解は%sでした。この対局では%sを指しています。", r.AnswerJa, r.PlayedJa)
	}
	return fmt.Sprintf("不正解です。正解は%sでした。", r.AnswerJa)
}
