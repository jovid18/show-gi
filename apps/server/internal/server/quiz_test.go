package server

import (
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/quiz"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 문항 만들기는 DB도 엔진도 안 타는 함수 둘에 걸려 있다 — 기록을 입력으로 옮기는 자리와
// 문장을 만드는 자리다. 여기가 틀리면 한 번도 벌어지지 않은 국면이 문항이 되거나,
// 화면이 사실과 다른 문장을 말한다.

func quizRecord(myColor string, result store.GameResult, moves ...store.RecordedMove) store.GameRecord {
	rec := store.GameRecord{GameSummary: store.GameSummary{ID: 3, MyColor: myColor, Result: result}}
	rec.Moves = moves
	return rec
}

func move(ply int, usiMove string, cp *int) store.RecordedMove {
	return store.RecordedMove{Ply: ply, USI: usiMove, EvalCp: cp}
}

func cpOf(v int) *int { return &v }

func TestQuizInputCarriesColorAndResult(t *testing.T) {
	rec := quizRecord("w", store.ResultWin, move(1, "7g7f", cpOf(30)), move(2, "3c3d", cpOf(20)))
	in := quizInput(rec)

	if in.Human != shogi.White {
		t.Errorf("human = %v, want white", in.Human)
	}
	if !in.Won {
		t.Error("won = false, but the record says win — Converted hangs on this")
	}
	if len(in.Moves) != 2 || len(in.EvalCp) != 2 {
		t.Fatalf("moves = %d, evals = %d, want 2 and 2", len(in.Moves), len(in.EvalCp))
	}
	if in.StartSFEN != shogi.StartSFEN {
		t.Errorf("startSfen = %q, want the even-game start position", in.StartSFEN)
	}
}

func TestQuizInputStopsAtAHoleInTheKifu(t *testing.T) {
	// 기록은 큐가 넘치면 이벤트를 버린다(recorder.go). 구멍을 무시하고 이어 담으면 그 뒤가
	// 통째로 밀려서, 문항이 한 번도 벌어지지 않은 국면을 가리킨다.
	rec := quizRecord("b", store.ResultLoss,
		move(1, "7g7f", cpOf(30)),
		move(2, "3c3d", cpOf(20)),
		move(4, "8h3c+", cpOf(500)), // 3手目가 없다
	)
	in := quizInput(rec)

	if len(in.Moves) != 2 {
		t.Fatalf("moves = %v, want the first two only", in.Moves)
	}
	if in.Won {
		t.Error("won = true on a loss")
	}
}

func TestQuizInputEvalsLineUpWithMoves(t *testing.T) {
	// EvalCp[i] 는 i+1 手目 뒤의 값이다. 한 칸이라도 밀리면 낙폭의 부호가 뒤집힌다.
	rec := quizRecord("b", store.ResultWin,
		move(1, "7g7f", cpOf(10)),
		move(2, "3c3d", nil), // 평가치는 수보다 늦게 온다 — 빈 칸이 실제로 있다
		move(3, "2g2f", cpOf(-40)),
	)
	in := quizInput(rec)

	if len(in.EvalCp) != 3 {
		t.Fatalf("evals = %d, want 3", len(in.EvalCp))
	}
	if in.EvalCp[1] != nil {
		t.Error("evals[1] should stay nil — a missing eval is not zero, zero is even")
	}
	if got, ok := in.PlayerEval(0); !ok || got != 10 {
		t.Errorf("PlayerEval(0) = (%d, %v), want (10, true)", got, ok)
	}
}

func TestQuizInputFlipsEvalsForWhite(t *testing.T) {
	// DB는 先手 관점이다. 안 뒤집으면 後手로 둔 판의 낙폭이 통째로 반대가 된다.
	rec := quizRecord("w", store.ResultLoss, move(1, "7g7f", cpOf(120)))
	in := quizInput(rec)

	if got, ok := in.PlayerEval(0); !ok || got != -120 {
		t.Errorf("PlayerEval(0) = (%d, %v), want (-120, true)", got, ok)
	}
}

func TestOpeningPliesFromTheBook(t *testing.T) {
	rec := quizRecord("b", store.ResultWin)
	if got := openingPlies(rec); got != 0 {
		t.Errorf("openingPlies = %d, want 0 when the opening was left to the computer", got)
	}

	rec.OpeningID = "shikenbisha"
	got := openingPlies(rec)
	if got <= 0 {
		t.Fatalf("openingPlies = %d, want the book line to cover some plies", got)
	}
	if got%2 != 0 {
		t.Errorf("openingPlies = %d, want an even ply count (the book holds one side's moves)", got)
	}

	rec.OpeningID = "no-such-opening"
	if got := openingPlies(rec); got != 0 {
		t.Errorf("openingPlies = %d, want 0 for an unknown opening id", got)
	}
}

// 오답을 한 문장으로 뭉치지 않는다. 「この手では詰みません」은 詰み이 남는 수에는
// 거짓이고, 초심자는 그것이 거짓인지 확인할 수단이 없다.
func TestMateMessageSplitsTheTwoWrongAnswers(t *testing.T) {
	// Rest == 0 은 「한계 안에서 못 찾았다」거나 「안 물어봤다」다(1手 노드). 그래서
	// 詰み이 사라졌다고도, 아예 없다고도 말할 수 없다 — 「이 수로는 詰み이 안 된다」만 참이다.
	lost := mateMessage(quiz.MateProgress{Outcome: quiz.MateWrong, Rest: 0}, "▲5二金")
	if strings.Contains(lost, "詰みません") || strings.Contains(lost, "消え") {
		t.Errorf("message = %q, must not claim the mate is gone or never existed", lost)
	}
	if !strings.Contains(lost, "詰みになりません") {
		t.Errorf("a move that does not reach the mate says %q", lost)
	}

	longer := mateMessage(quiz.MateProgress{Outcome: quiz.MateWrong, Rest: 7}, "▲5二金")
	if strings.Contains(longer, "詰みになりません") {
		t.Errorf("a move that still mates must not be told the mate is gone: %q", longer)
	}
	if !strings.Contains(longer, "9手") {
		t.Errorf("the message should say how long the mate became, got %q", longer)
	}
}

// 첫 오답부터 정답을 싣던 자리다(2026-08-14-human-2.md §6 #10 · #11). 세 번째까지는
// 다시 풀라고만 하고, 그 뒤로도 나가는 것은 「무엇을 움직이나」 한 마디뿐이다.
func TestMateMessageWithholdsTheAnswer(t *testing.T) {
	wrong := quiz.MateProgress{Outcome: quiz.MateWrong}
	early := mateMessage(wrong, "")
	if strings.Contains(early, "正解は") {
		t.Errorf("message = %q, must not name the answer on a wrong attempt", early)
	}
	// 정답도 없이 문항이 끝난 것으로 읽히면 남는 것이 아무것도 없다.
	if !strings.Contains(early, "最初から") {
		t.Errorf("message = %q, want it to say the question can be tried again", early)
	}

	hinted := mateMessage(wrong, "7九の銀")
	if !strings.Contains(hinted, "7九の銀") {
		t.Errorf("message = %q, want the third attempt to say what to move", hinted)
	}
	if strings.Contains(hinted, "正解は") {
		t.Errorf("message = %q, the hint must not become the answer", hinted)
	}
}

func TestMateMessageOnANonCheck(t *testing.T) {
	// 오답이 아니라 안내다 — 규약을 모른 채 오답 처리되는 것이 배움을 막는다.
	got := mateMessage(quiz.MateProgress{Outcome: quiz.MateNotCheck}, "")
	if !strings.Contains(got, "王手") {
		t.Errorf("message = %q, want it to name the rule it is teaching", got)
	}
	if strings.Contains(got, "不正解") {
		t.Errorf("message = %q, must not read as a wrong answer", got)
	}
}

func TestMateMessageWhileOngoing(t *testing.T) {
	got := mateMessage(quiz.MateProgress{Outcome: quiz.MateOngoing, Plies: 3, Line: []string{"G*5b", "6b5b"}}, "")
	if !strings.Contains(got, "3手") {
		t.Errorf("message = %q, want the remaining ply count", got)
	}
	if !strings.Contains(got, "正解") {
		t.Errorf("message = %q, want it to confirm the move was right", got)
	}
}

// 수를 하나도 안 낸 요청은 정답이 아니다. 문항을 여는 자리가 바로 그 요청이라
// (rootChecks) 여기서 「正解です」라고 하면 아무것도 안 한 사람에게 맞혔다고 말한다.
func TestMateMessageOnAnEmptyAttempt(t *testing.T) {
	got := mateMessage(quiz.MateProgress{Outcome: quiz.MateOngoing, Plies: 3}, "")
	if strings.Contains(got, "正解") {
		t.Errorf("message = %q, must not call an empty attempt correct", got)
	}
	if !strings.Contains(got, "王手") {
		t.Errorf("message = %q, want the opening instruction", got)
	}
}

func TestBestMessageNamesWhatWasPlayedInTheGame(t *testing.T) {
	// 이 문항이 문제집이 아니라 자기 기보인 이유가 이 한 줄이다.
	item := quiz.BestItem{SFEN: quizCollisionSFEN, Answer: "2f2e", Played: "6g6f", AnswerCp: 300, SecondCp: 50}
	got := bestMessage(bestResponse{
		Correct: false,
		Move:    "3g3f", MoveJa: "▲3六歩",
		Played: "6g6f", PlayedJa: "▲6六歩",
	}, item)
	if !strings.Contains(got, "▲6六歩") {
		t.Errorf("message = %q, want the move actually played", got)
	}

	right := bestMessage(bestResponse{Correct: true, MoveJa: "▲2五歩"}, item)
	if !strings.Contains(right, "250") {
		t.Errorf("a correct answer should say the gap it was picked for, got %q", right)
	}
}

// 오답에는 정답이 없다(§6 #10 · #11). 세 번째부터 나가는 것은 「무엇을 움직이나」뿐이고,
// 그것도 도착 칸을 말하지 않는다 — 말하면 그 한 줄이 정답 전체가 된다.
func TestBestMessageWithholdsTheAnswer(t *testing.T) {
	item := quiz.BestItem{SFEN: quizCollisionSFEN, Answer: "4f3e", Played: "G*3h"}
	early := bestMessage(bestResponse{Correct: false, Move: "P*3e", MoveJa: "▲3五歩"}, item)
	if strings.Contains(early, "正解は") || strings.Contains(early, "3五金") {
		t.Errorf("message = %q, must not name the answer on a wrong attempt", early)
	}
	if !strings.Contains(early, "もう一度") {
		t.Errorf("message = %q, want it to say the question can be tried again", early)
	}

	hinted := bestMessage(bestResponse{
		Correct: false, Move: "P*3e", MoveJa: "▲3五歩", Hint: "4六の金",
	}, item)
	if !strings.Contains(hinted, "4六の金") {
		t.Errorf("message = %q, want the third attempt to say what to move", hinted)
	}
	if strings.Contains(hinted, "3五金") {
		t.Errorf("message = %q, the hint must not name where the answer goes", hinted)
	}
}

// 낸 수부터 말한다. 회차 1의 #17이 이 문장 하나다. 정답을 문장에서 뺀 뒤에는 그 상처가
// 낸 수와 그 판의 수 사이로 옮겨 온다 — 그 둘도 打 한 글자로만 갈릴 수 있다.
func TestBestMessageNamesTheMoveJustPlayed(t *testing.T) {
	item := quiz.BestItem{SFEN: quizCollisionSFEN, Answer: "4f3e", Played: "G*3e", AnswerCp: 910, SecondCp: 548}
	got := bestMessage(bestResponse{
		Correct: false,
		Move:    "4f3e", MoveJa: "▲3五金",
		Played: "G*3e", PlayedJa: "▲3五金打",
	}, item)
	if !strings.Contains(got, "▲3五金打") {
		t.Errorf("message = %q, want the move played in the game", got)
	}
	if !strings.Contains(got, "持ち駒の金") {
		t.Errorf("message = %q, want the two same-square notations told apart", got)
	}

	// 낸 수가 그 판에서 둔 수와 같으면 되풀이하지 않는다.
	same := bestMessage(bestResponse{
		Correct: false,
		Move:    "G*3h", MoveJa: "▲3八金打",
		Played: "G*3h", PlayedJa: "▲3八金打",
	}, quiz.BestItem{SFEN: quizCollisionSFEN, Answer: "4f3e", Played: "G*3h"})
	if strings.Contains(same, "この対局では") {
		t.Errorf("message = %q, must not repeat the same move as a second fact", same)
	}

	// 표기가 없어도 채점은 사실이다 — 문장이 비지 않아야 한다.
	bare := bestMessage(bestResponse{Correct: false}, quiz.BestItem{})
	if !strings.HasPrefix(bare, "不正解です。") {
		t.Errorf("message = %q, want the verdict first when nothing can be named", bare)
	}
}

// 회차 1의 110手 국면. 여기서 3五로 가는 수가 셋이고(4f3e · G*3e · P*3e) 두 金이 打 한
// 글자로만 갈린다 — #17의 원인으로 적혀 있던 「표기 구분이 없다」는 틀린 진단이었다.
const quizCollisionSFEN = "8l/1r5k1/4ppp2/pn5N1/1S1L1N2P/P2PPG3/1P3P+b1p/1KGGR4/LN4+b2 b G3P3sl4p 111"

func TestAfterMoveOpensThePositionThatWasPlayed(t *testing.T) {
	canon, ja, next, _ := afterMove(quizCollisionSFEN, "G*3e")
	if canon != "G*3e" {
		t.Errorf("canon = %q, want the canonical usi", canon)
	}
	if ja != "▲3五金打" {
		t.Errorf("ja = %q, want ▲3五金打", ja)
	}
	if next == "" || next == quizCollisionSFEN {
		t.Errorf("sfen = %q, want the position after the move", next)
	}
	// 打과 반상 이동이 갈리는 것이 판에 그려져야 한다 — 반상의 金은 4六에 남는다.
	if !strings.Contains(next, "G") {
		t.Errorf("sfen = %q, want the gold still on the board", next)
	}

	moved, movedJa, movedNext, _ := afterMove(quizCollisionSFEN, "4f3e")
	if movedJa != "▲3五金" || moved != "4f3e" {
		t.Errorf("ja/canon = %q/%q, want ▲3五金/4f3e", movedJa, moved)
	}
	if movedNext == next {
		t.Errorf("the drop and the board move must not produce the same position: %q", movedNext)
	}

	// 못 두는 수는 넷 다 빈 값이다. 여기서 실패를 오류로 올리면 맞은 답이 500이 된다.
	if c, j, n, k := afterMove(quizCollisionSFEN, "1a1b"); c != "" || j != "" || n != "" || k != "" {
		t.Errorf("afterMove on an illegal move = %q/%q/%q/%q, want all empty", c, j, n, k)
	}
	if c, _, _, _ := afterMove("not a sfen", "4f3e"); c != "" {
		t.Errorf("canon = %q, want empty for an unreadable position", c)
	}
}

func TestMoveOriginJaOnlyWhenTheNotationsCollide(t *testing.T) {
	// 같은 칸으로 가는 다른 수 — 이때만 붙인다.
	if got := moveOriginJa(quizCollisionSFEN, "4f3e", "G*3e"); got != "4六の金" {
		t.Errorf("origin = %q, want 4六の金", got)
	}
	// 뒤집어도 같다. 정답이 打이면 「持ち駒の」다 — 반상에 그 칸을 가리킬 자리가 없다.
	if got := moveOriginJa(quizCollisionSFEN, "G*3e", "4f3e"); got != "持ち駒の金" {
		t.Errorf("origin = %q, want 持ち駒の金", got)
	}
	// 같은 수면 붙일 이유가 없다 — 맞힌 사람에게 어디서 왔는지 설명할 자리가 아니다.
	if got := moveOriginJa(quizCollisionSFEN, "4f3e", "4f3e"); got != "" {
		t.Errorf("origin = %q, want empty when the two moves are the same", got)
	}
	// 칸이 다르면 표기가 이미 갈려 있다. 붙이면 문장만 길어진다.
	if got := moveOriginJa(shogi.StartSFEN, "7g7f", "2g2f"); got != "" {
		t.Errorf("origin = %q, want empty when the moves land on different squares", got)
	}
	if got := moveOriginJa(quizCollisionSFEN, "4f3e", ""); got != "" {
		t.Errorf("origin = %q, want empty when nothing was played", got)
	}
}

func TestWithOriginKeepsTheNotationWhenThereIsNothingToAdd(t *testing.T) {
	if got := withOrigin("▲3五金", "4六の金"); got != "▲3五金（4六の金）" {
		t.Errorf("annotated = %q", got)
	}
	if got := withOrigin("▲3五金", ""); got != "▲3五金" {
		t.Errorf("annotated = %q, want the notation untouched", got)
	}
	if got := withOrigin("", "4六の金"); got != "" {
		t.Errorf("annotated = %q, want empty when there is no notation", got)
	}
}

func TestJaAtLeavesUnreadableMovesEmpty(t *testing.T) {
	// 표기가 없어도 수는 사실이다. 여기서 죽으면 채점 응답이 통째로 500이 된다.
	if got := jaAt(shogi.StartSFEN, "1a1b", -1); got != "" {
		t.Errorf("ja = %q, want empty for an illegal move", got)
	}
	if got := jaAt("not a sfen", "7g7f", -1); got != "" {
		t.Errorf("ja = %q, want empty for an unreadable position", got)
	}
	if got := jaAt(shogi.StartSFEN, "7g7f", -1); got != "▲7六歩" {
		t.Errorf("ja = %q, want ▲7六歩", got)
	}
}

// jaOfLine 은 처음부터 두어 와야 「同」 표기가 맞는다. 마지막 국면에서 한 수만 두어
// 보면 그 표기가 빠진다.
func TestJaOfLineKeepsTheSameSquareNotation(t *testing.T) {
	// ▲7六歩 △3四歩 ▲同歩 는 없으니, 잡는 수순으로 「同」이 나오는 줄을 쓴다.
	line := []string{"7g7f", "8c8d", "7f7e", "8d8e", "7e7d", "8e8f", "7d7c+"}
	got := jaOfLine(shogi.StartSFEN, line, true)
	if got == "" {
		t.Fatal("the line did not replay")
	}
	if !strings.Contains(got, "成") {
		t.Errorf("last move ja = %q, want a promotion", got)
	}

	if got := jaOfLine(shogi.StartSFEN, line, false); got != "" {
		t.Errorf("ja = %q, want empty when the caller did not ask for it", got)
	}
}

// 수순은 정답과 같은 취급이다 — 첫 수가 곧 정답이라, 오답에 실어 보내면 문항이 그
// 자리에서 끝난다(§61이 정답에 대해 닫은 것과 같은 자리).
func TestLineIsRenderedFromTheAnswerPosition(t *testing.T) {
	pos := shogi.StartPosition()
	got := lineFrom(pos.SFEN(), "7g7f", []string{"3c3d", "8h2b+"})

	if len(got) != 2 {
		t.Fatalf("길이 = %d, want 2: %+v", len(got), got)
	}
	// 표기가 정답을 둔 뒤의 국면에서 나와야 한다. 문제 국면에서 만들면 후수의 수가
	// 선수 차례의 표기로 적힌다.
	if got[0].Ja != "△3四歩" {
		t.Errorf("첫 수 표기 = %q, want △3四歩", got[0].Ja)
	}
	if got[0].SFEN == "" || got[1].SFEN == "" {
		t.Error("국면이 비었다 — 화면은 규칙을 모르므로 스스로 못 둔다")
	}
	if got[1].USI != "8h2b+" {
		t.Errorf("둘째 수 = %q", got[1].USI)
	}
}

// 저장된 수순이 그 국면에서 안 서면 거기까지만 준다. 500으로 답하면 맞은 답이 오류가 된다.
func TestLineStopsInsteadOfFailing(t *testing.T) {
	pos := shogi.StartPosition()
	got := lineFrom(pos.SFEN(), "7g7f", []string{"3c3d", "9i9b"})
	if len(got) != 1 {
		t.Fatalf("길이 = %d, want 1: %+v", len(got), got)
	}
}

// 옛 판에는 이 칸이 없다. 그때 화면이 그 줄을 통째로 안 그린다.
func TestNoLineForOlderQuizzes(t *testing.T) {
	pos := shogi.StartPosition()
	if got := lineFrom(pos.SFEN(), "7g7f", nil); got != nil {
		t.Errorf("%+v, want nil", got)
	}
}

// 정답이 그 국면에서 안 서면 수순도 없다 — 문항이 깨진 것이고, 반쪽을 그리지 않는다.
func TestNoLineWhenTheStoredAnswerDoesNotStand(t *testing.T) {
	pos := shogi.StartPosition()
	if got := lineFrom(pos.SFEN(), "9i9b", []string{"3c3d"}); got != nil {
		t.Errorf("%+v, want nil", got)
	}
}
