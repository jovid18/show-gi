package server

import (
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/quiz"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 문항 만들기는 DB도 엔진도 안 타는 함수 둘에 걸려 있다 — 기록을 입력으로 옮기는 자리와
// 문장을 만드는 자리다. 여기가 틀리면 **한 번도 벌어지지 않은 국면**이 문항이 되거나,
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
	// 통째로 밀려서, 문항이 **한 번도 벌어지지 않은 국면**을 가리킨다.
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
	// `EvalCp[i]` 는 `i+1` 手目 뒤의 값이다. 한 칸이라도 밀리면 낙폭의 부호가 뒤집힌다.
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

// **오답을 한 문장으로 뭉치지 않는다.** 「この手では詰みません」은 詰み이 남는 수에는
// 거짓이고, 초심자는 그것이 거짓인지 확인할 수단이 없다.
func TestMateMessageSplitsTheTwoWrongAnswers(t *testing.T) {
	// `Rest == 0` 은 「한계 안에서 못 찾았다」거나 **「안 물어봤다」**다(1手 노드). 그래서
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
	if !strings.Contains(longer, "▲5二金") {
		t.Errorf("a wrong answer has to be told what did work, got %q", longer)
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

// **수를 하나도 안 낸 요청은 정답이 아니다.** 문항을 여는 자리가 바로 그 요청이라
// (`rootChecks`) 여기서 「正解です」라고 하면 아무것도 안 한 사람에게 맞혔다고 말한다.
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
	// 이 문항이 문제집이 아니라 **자기 기보**인 이유가 이 한 줄이다.
	got := bestMessage(bestResponse{
		Correct: false, Answer: "2f2e", AnswerJa: "▲2五歩",
		Played: "6g6f", PlayedJa: "▲6六歩", AnswerCp: 300, SecondCp: 50,
	})
	if !strings.Contains(got, "▲2五歩") {
		t.Errorf("message = %q, want the answer", got)
	}
	if !strings.Contains(got, "▲6六歩") {
		t.Errorf("message = %q, want the move actually played", got)
	}

	right := bestMessage(bestResponse{Correct: true, AnswerCp: 300, SecondCp: 50})
	if !strings.Contains(right, "250") {
		t.Errorf("a correct answer should say the gap it was picked for, got %q", right)
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

// jaOfLine 은 **처음부터 두어 와야** 「同」 표기가 맞는다. 마지막 국면에서 한 수만 두어
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
