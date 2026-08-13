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
	// Attempt 는 이 문항에서 **몇 번째 시도**인가(1부터). **화면이 센다** — 서버에 남기지
	// 않는다. 몇 번 틀렸는지는 그 판의 사실이 아니라 지금 이 사람이 이 화면에서 하고 있는
	// 일이고, 남기면 되짚기를 다시 열 때마다 「이미 세 번 틀린 문항」이 된다.
	//
	// **이 값으로 정답을 살 수는 없다.** 크게 적어 보내도 나가는 것은 `Hint` 뿐이고, 정답은
	// 맞힐 때까지 응답에 실리지 않는다 — 그것이 채점을 서버에 둔 이유다(quizHintAttempt).
	Attempt int `json:"attempt,omitempty"`
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
	// Hint 는 「무엇을 어디서 움직이나」다(「7九の銀」). **세 번째 오답에서만 온다.**
	//
	// **정답 수는 이 응답에 아예 없다.** 첫 오답부터 답을 실어 보내던 자리이고, 그러면 한 번
	// 틀리는 것으로 문항이 끝난다 — 사람이 그걸 지적했다(§6 #10 · #11). 채점기는 여전히
	// 정답을 알지만(quiz.MateProgress.Best) 전송 계층이 내보내지 않는다.
	Hint string `json:"hint,omitempty"`
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
	}
	if pos, err := shogi.ParseSFEN(prog.SFEN); err == nil {
		out.Checked = checkedSquare(pos)
	}
	out.DefenseJa = jaOfLine(q.Mate.SFEN, prog.Line, prog.Defense != "")
	// **힌트는 정답이 성립하는 국면에서 만든다.** `prog.SFEN` 은 오답이면 그 수만큼 나아가
	// 있어서 거기서는 정답이 불법이고, 그러면 「무엇을 움직이나」가 통째로 빠진다
	// (quiz.MateProgress.BestFrom).
	if hinting(req.Attempt) && prog.Outcome == quiz.MateWrong {
		out.Hint = originJa(prog.BestFrom, prog.Best)
	}
	out.Message = mateMessage(prog, out.Hint)
	writeJSON(w, http.StatusOK, out)
}

// bestRequest 는 「이 문항에 이 수」다.
type bestRequest struct {
	Index int    `json:"index"`
	Move  string `json:"move"`
	// Attempt 는 이 문항에서 몇 번째 시도인가(1부터). `mateRequest.Attempt` 와 같은 규약이다.
	Attempt int `json:"attempt,omitempty"`
}

// bestResponse 는 채점 결과다. **정답과 두 cp를 여기서 처음 보낸다** — 문항에 실어 보내면
// 화면을 열자마자 답이 손에 있다.
type bestResponse struct {
	Correct bool `json:"correct"`
	// Answer·AnswerJa·AnswerCp·SecondCp 는 **맞혔을 때만 있다.**
	//
	// 첫 오답부터 정답을 싣고 있었고, 그러면 한 번 틀리는 것으로 문항이 끝난다 — 사람이
	// 그걸 지적했다(2026-08-14-human-2.md §6 #10 · #11). 문구에서 지우는 것으로는 안 된다:
	// 응답에 남아 있으면 화면이 그것을 다른 칸에 그대로 적고 있었다(`quiz-scores`).
	//
	// **cp가 포인터다.** 0은 호각이라는 실제 값이라 `omitempty` 로는 「없다」와 안 갈린다.
	Answer   string `json:"answer,omitempty"`
	AnswerJa string `json:"answerJa,omitempty"`
	// AnswerCp·SecondCp 는 **사람 관점** cp다. 둘의 차가 이 문항이 뽑힌 기준이다.
	AnswerCp *int `json:"answerCp,omitempty"`
	SecondCp *int `json:"secondCp,omitempty"`
	// Hint 는 「무엇을 어디서 움직이나」다. **세 번째 오답에서만 온다**(quizHintAttempt).
	Hint string `json:"hint,omitempty"`
	// Move·MoveJa 는 **지금 이 문항에 낸 수**다. 정본 USI이고, 읽을 수 없었으면 빈 값이다.
	//
	// **Played 와 다르다** — 저쪽은 그 판에서 실제로 둔 수다. 이 둘을 뭉치면 오답 문구가
	// 그 판에서 둔 수만 말하게 되고, 방금 낸 사람에게는 **자기가 낸 수가 어디에도 없는
	// 문장**이 된다(회차 1 #17 · §57).
	Move   string `json:"move"`
	MoveJa string `json:"moveJa,omitempty"`
	// SFEN·Checked 는 **그 수를 둔 뒤의 판**이다. 못 만들었으면 빈 값이고, 그때 화면은 문제
	// 국면을 그대로 세운다.
	//
	// 낸 수를 판에서 보여주는 유일한 길이다 — 화면은 규칙을 모르므로 스스로 한 수 둘 수 없다.
	// **打과 반상 이동이 갈리는 자리도 여기다**: 출발 칸이 빈다는 것을 판이 그려야 「▲3五金」과
	// 「▲3五金打」가 한 글자 차이인 것이 눈에 걸린다(회차 1 #18).
	SFEN    string `json:"sfen,omitempty"`
	Checked string `json:"checked,omitempty"`
	// Played·PlayedJa 는 사람이 대국에서 실제로 둔 수다.
	Played   string `json:"played"`
	PlayedJa string `json:"playedJa,omitempty"`
	// Line 은 정답 뒤에 서로 최선으로 뒀을 때의 흐름이다. **맞혔을 때만 있다** —
	// 첫 수가 곧 정답이라 이것만 내보내도 정답을 말한 것이 된다(quiz.BestItem.Line).
	//
	// **옛 판에는 없다.** 이 칸이 생기기 전에 만들어진 문항은 영영 비어 있다.
	Line    []lineMove `json:"line,omitempty"`
	Message string     `json:"message"`
}

// lineMove 는 수순 한 수다. 되짚기의 기보 한 줄과 같은 어휘이고, **국면까지 준다** —
// 화면은 규칙을 모르므로 스스로 한 수도 못 둔다(reviewMove 와 같은 근거).
type lineMove struct {
	USI string `json:"usi"`
	Ja  string `json:"ja"`
	// SFEN 은 이 수를 **둔 뒤**의 국면이다.
	SFEN string `json:"sfen"`
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

	out := bestResponse{Correct: correct, Played: item.Played}
	out.PlayedJa = jaAt(item.SFEN, item.Played, -1)
	out.Move, out.MoveJa, out.SFEN, out.Checked = afterMove(item.SFEN, req.Move)
	// **정답은 맞혔을 때만 응답에 넣는다.** 채점은 이미 끝나 있고, 여기서 갈리는 것은
	// 「무엇을 내보내는가」뿐이다(bestResponse).
	if correct {
		out.Answer, out.AnswerJa = item.Answer, jaAt(item.SFEN, item.Answer, -1)
		out.AnswerCp, out.SecondCp = &item.AnswerCp, &item.SecondCp
		out.Line = lineFrom(item.SFEN, item.Answer, item.Line)
	} else if hinting(req.Attempt) {
		out.Hint = originJa(item.SFEN, item.Answer)
	}
	out.Message = bestMessage(out, item)
	writeJSON(w, http.StatusOK, out)
}

// lineFrom 은 저장된 수순을 표기와 국면까지 붙여 편다.
//
// **정답을 둔 자리에서 시작한다.** 저장된 것은 그 뒤의 수순뿐이라(quiz.BestItem.Line)
// 여기서 한 수 먼저 둬야 판이 맞는다.
//
// **막히면 거기까지만 준다.** 저장된 수순이 그 국면에서 안 서는 것은 문항이 깨졌다는
// 뜻인데, 그때 500으로 답하면 **맞은 답이 오류가 된다**(afterMove 와 같은 판단).
//
// 「同」이 여기서부터는 산다 — 앞 수의 도착 칸을 이어 넘긴다. 문항의 첫 수에만 못 쓴다
// (그 국면에는 직전 수가 없다, jaAt 의 prevTo = -1).
func lineFrom(sfen, answer string, line []string) []lineMove {
	if len(line) == 0 {
		return nil
	}
	pos, err := shogi.ParseSFEN(sfen)
	if err != nil {
		return nil
	}
	first, err := shogi.ParseUSIMove(answer)
	if err != nil || pos.ValidateMove(first) != nil {
		return nil
	}
	pos = pos.Apply(first)
	prevTo := int(first.To)

	out := make([]lineMove, 0, len(line))
	for _, u := range line {
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			break
		}
		next, ja, ok := advance(pos, prevTo, u)
		if !ok {
			break
		}
		out = append(out, lineMove{USI: m.USI(), Ja: ja, SFEN: next.SFEN()})
		pos, prevTo = next, int(m.To)
	}
	if len(out) == 0 {
		return nil
	}
	return out
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

// afterMove 는 그 국면에서 그 수를 둔 결과다 — 정본 USI · 棋譜 표기 · 다음 국면 · 王手 칸.
//
// **못 두면 넷 다 빈 값이다.** 채점은 이미 끝났으므로(quiz.GradeBest) 여기서 실패하는 것은
// 표기와 판을 못 보여준다는 뜻일 뿐이고, 그때 500으로 답하면 맞은 답이 오류가 된다.
//
// 정본으로 돌려주는 이유는 문구가 이 값으로 **「그 판에서 둔 수」와 견주기** 때문이다
// (bestMessage). 요청 문자열을 그대로 쓰면 그 비교가 파서가 얼마나 느슨한가에 매인다.
func afterMove(sfen, usiMove string) (canon, ja, next, checked string) {
	pos, err := shogi.ParseSFEN(sfen)
	if err != nil {
		return "", "", "", ""
	}
	m, err := shogi.ParseUSIMove(usiMove)
	if err != nil {
		return "", "", "", ""
	}
	if err := pos.ValidateMove(m); err != nil {
		return "", "", "", ""
	}
	// **표기는 두기 전 국면에서 만든다.** 「同」은 쓰지 않는다(prevTo = -1) — 문항의 국면에는
	// 직전 수가 없고, 없는 것을 있는 것처럼 적으면 그 표기가 어느 칸인지 말하지 않게 된다.
	after := pos.Apply(m)
	return m.USI(), pos.MoveJa(m, -1), after.SFEN(), checkedSquare(after)
}

// moveOriginJa 는 정답 수가 **어디서 오는가**다 — 「4六の金」 혹은 「持ち駒の金」.
//
// **낸 수와 같은 칸으로 가는 다른 수일 때만 채운다.** 그때 두 표기는 打 한 글자로만 갈리고
// (▲3五金 / ▲3五金打) 나란히 놓아도 사람은 같은 수로 읽는다 — 회차 1 #17이 그것이다.
// 칸이 다르면 표기가 이미 갈려 있으므로 덧붙이면 문장만 길어진다.
func moveOriginJa(sfen, answer, played string) string {
	a, err := shogi.ParseUSIMove(answer)
	if err != nil {
		return ""
	}
	p, err := shogi.ParseUSIMove(played)
	if err != nil || a.To != p.To || a.USI() == p.USI() {
		return ""
	}
	return originJa(sfen, answer)
}

// originJa 는 그 수가 **무엇을 어디서** 움직이는가다 — 「4六の金」·「持ち駒の金」.
//
// **도착 칸을 말하지 않는다.** 세 번 틀린 사람에게 나가는 것이 이 한 마디뿐이라
// (2026-08-14-human-2.md §6 #11), 여기에 도착 칸이 섞이면 그 줄이 곧 정답 전체가 된다.
func originJa(sfen, move string) string {
	m, err := shogi.ParseUSIMove(move)
	if err != nil {
		return ""
	}
	if m.IsDrop() {
		return "持ち駒の" + shogi.PieceJa(m.Drop)
	}
	pos, err := shogi.ParseSFEN(sfen)
	if err != nil {
		return ""
	}
	// **빈 칸이면 부를 이름이 없다.** 여기 오는 것은 저장된 정답이 그 국면에서 성립하지
	// 않는다는 뜻이고(문항이 깨졌다), 그때 「4六の」까지만 적으면 문장이 말을 잃는다.
	from := pos.Board[m.From]
	if from.Empty() {
		return ""
	}
	// **成る 수여도 지금 그 칸에 서 있는 駒로 부른다.** 「4六の金」은 출발 칸을 가리키는 말이라
	// 성한 뒤의 이름으로 부르면 판에서 그 駒를 찾을 수 없다.
	return shogi.SquareJa(int(m.From)) + "の" + shogi.PieceJa(from.Type())
}

// withOrigin 은 표기에 「어디서 오는가」를 괄호로 붙인다. 어느 쪽이든 비면 그대로 돌려준다.
func withOrigin(ja, origin string) string {
	if ja == "" || origin == "" {
		return ja
	}
	return ja + "（" + origin + "）"
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

// quizHintAttempt 는 「무엇을 움직이나」가 나오기 시작하는 시도 횟수다.
//
// **정답을 말하는 자리는 없다.** 세 번째부터는 이 한 마디가 나오고 그 뒤로는 몇 번 틀려도
// 같다 — 사람이 그렇게 정했다(2026-08-14-human-2.md §6 #11). 두 번은 스스로 다시 보라는
// 뜻이고, 세 번이면 판에서 무엇을 볼지조차 못 잡고 있다는 뜻이다.
const quizHintAttempt = 3

// hinting 은 이 시도에서 힌트를 줄 차례인가다. **화면이 보낸 값을 믿는다** — 그것으로 살 수
// 있는 것이 힌트뿐이라 믿어도 되는 자리다(mateRequest.Attempt).
func hinting(attempt int) bool { return attempt >= quizHintAttempt }

// mateMessage 는 詰み 문항의 일본어 문장이다. `hint` 는 「무엇을 어디서 움직이나」이고,
// **세 번째 오답에서만 채워져 온다**(originJa).
//
// **오답을 한 문장으로 뭉치지 않는다.** 「この手では詰みません」은 대부분 거짓이다 —
// 詰み이 9手로 늘어질 뿐 詰む 수도 있고, 그때 그 문장은 초심자가 검증할 수 없는 거짓이 된다.
func mateMessage(p quiz.MateProgress, hint string) string {
	switch p.Outcome {
	case quiz.MateSolved:
		return "詰みました。正解です。"

	case quiz.MateNotCheck:
		return "王手ではありません。詰将棋では一手ごとに王手をかけ続けます。"

	case quiz.MateWrong:
		// **아는 만큼만 말한다.** `Rest == 0` 은 두 가지다 — solver 가 자기 한계
		// (`ENGINE_MATE_PLIES`, 기본 11) 안에서 詰み을 못 찾았거나, **애초에 안 물어봤다**
		// (1手 노드에서는 답이 안 바뀌므로 묻지 않는다 — quiz.expand).
		//
		// 그래서 「詰みません」도 「詰みが消えました」도 안 된다. 한계를 넘겨 늘어난 詰み은
		// 여전히 강제되고(7手 뿌리에서 한 手 낭비하면 13手가 될 수 있다) 안 물어본 쪽은
		// 말할 것이 아예 없다. **「이 수로는 詰み이 안 된다」는 둘 다에서 참이다.**
		head := "この手では詰みになりません。"
		if p.Rest > 0 {
			head = fmt.Sprintf("詰みは残りますが、%d手に伸びてしまいます。", 2+p.Rest)
		}
		// **정답을 말하지 않는다.** 대신 다시 풀 수 있다고 말한다 — 그 말이 없으면 문항이
		// 끝난 것으로 읽히고, 정답도 없으니 남는 것이 아무것도 없다.
		if hint != "" {
			return head + fmt.Sprintf("動かすのは%sです。", hint)
		}
		return head + "「最初から」でもう一度考えてみてください。"

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

// bestMessage 는 「최선수는?」 문항의 일본어 문장이다. `answer` 는 「어디서 오는가」까지
// 붙은 정답 표기다(withOrigin) — **맞혔을 때만 문장에 들어간다.**
//
// **낸 수부터 말한다.** 정답만 말하는 문장은 정답과 한 글자 차이인 수를 낸 사람에게
// 「내가 그것을 뒀는데 틀렸다고 한다」가 된다 — 회차 1의 #17이 정확히 그 자리다.
//
// 오답이어도 **그 판의 일로 되돌린다** — 사람이 실제로 둔 수를 함께 말하는 것이 이 문항이
// 문제집이 아니라 자기 기보인 이유다.
//
// **`item` 을 함께 받는다.** 응답에서 정답과 cp를 뺐으므로(bestResponse) 문장이 그것들을
// 응답에서 읽을 수 없다 — 두 자리가 같은 값을 서로 다른 이유로 필요로 한다.
func bestMessage(r bestResponse, item quiz.BestItem) string {
	if r.Correct {
		gap := item.Gap()
		if r.MoveJa == "" {
			return fmt.Sprintf("正解です。この局面はこの一手で、次善手とは%dの差があります。", gap)
		}
		return fmt.Sprintf("%sで正解です。この局面はこの一手で、次善手とは%dの差があります。", r.MoveJa, gap)
	}

	head := "不正解です。"
	if r.MoveJa != "" {
		head += fmt.Sprintf("あなたが指したのは%sでした。", r.MoveJa)
	}
	// **정답을 말하지 않는다.** 세 번째부터 「무엇을 움직이나」만 얹고, 그 앞에서는 다시
	// 보라고만 한다 — 정답을 실어 보내면 한 번 틀리는 것으로 문항이 끝난다(§6 #10 · #11).
	if r.Hint != "" {
		head += fmt.Sprintf("動かすのは%sです。", r.Hint)
	} else {
		head += "もう一度考えてみてください。"
	}

	// **정답과 같으면 말하지 않는다.** 그때는 그 한 줄이 정답을 그대로 말해 버린다 — 낸 수와
	// 같을 때 안 말하는 것은 방금 한 말의 되풀이라서다.
	//
	// **여기서도 두 표기가 打 한 글자로만 갈릴 수 있다.** §57이 정답과 낸 수 사이에서 닫은
	// 그 상처가, 문장에서 정답을 뺀 뒤에는 **낸 수와 그 판의 수** 사이로 옮겨 온다.
	if r.PlayedJa != "" && r.Played != item.Answer && r.Played != r.Move {
		played := withOrigin(r.PlayedJa, moveOriginJa(item.SFEN, item.Played, r.Move))
		head += fmt.Sprintf("この対局では%sを指しています。", played)
	}
	return head
}
