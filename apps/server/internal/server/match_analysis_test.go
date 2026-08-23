package server

import (
	"context"
	"errors"
	"math"
	"os"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/handicap"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/metrics"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/skill"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// stubAnalyst 는 엔진 없이 평가치와 낙폭을 돌려준다. 개입은 안 건다 — 대인전에 개입이
// 없다는 것을 이 테스트가 같이 지킨다. 실력 추정은 낙폭만 보므로 그것과 무관하다.
type stubAnalyst struct {
	fail bool
	// failFrom 이 0이 아니면 그 手数부터 실패한다. 반쪽으로 끝나는 판을 만든다.
	failFrom int
	// blindFrom 이 0이 아니면 그 手数부터 국면을 못 되만든 것으로 답한다. 엔진은 답했는데
	// 판정이 부호를 못 정한 자리이고, 실엔진에서는 그 뒤가 전부 같이 실패한다.
	blindFrom int
	// lossOdd·lossEven 은 홀수·짝수 手의 승률 낙폭이다. 둘을 갈라 두면 누구의 프로파일에
	// 무엇이 쌓였는지를 값으로 셀 수 있다.
	lossOdd, lossEven float64
}

func (s stubAnalyst) Judge(_ context.Context, _ string, _ []string, ply int) (game.Judgement, error) {
	if s.fail || (s.failFrom > 0 && ply >= s.failFrom) {
		return game.Judgement{}, errors.New("engine down")
	}
	loss := s.lossOdd
	if ply%2 == 0 {
		loss = s.lossEven
	}
	// 手数를 그대로 값으로 쓴다 — 어느 칸에 무엇이 들어갔는지 눈으로 셀 수 있다.
	return game.Judgement{
		HasEvals:      s.blindFrom == 0 || ply < s.blindFrom,
		SenteCpBefore: ply * 10, SenteCpAfter: ply*10 + 1,
		Verdict:   intervene.Verdict{DeltaWin: loss},
		Threshold: stubLevel.Threshold(),
	}, nil
}

// stubLevel 은 프로덕션이 쓰는 레벨이다(cmd/api). 임계치를 여기서 따로 정하면 정규화가
// 실제와 다른 자로 돈다.
var stubLevel = intervene.Beginner

// 평가치는 두 행에 다 들어간다. 대인전 한 판이 games 행 둘이라, 한쪽만 채우면
// 같은 판을 두 사람이 다르게 본다.
//
//	SHOWGI_TEST_DATABASE_URL=postgres://showgi:showgi@localhost:5432/showgi go test ./internal/server/
func TestAnalysisFillsBothRowsOfAMatch(t *testing.T) {
	st, seats := matchSeatsForAnalysis(t, "", plyList(3))

	a := newMatchAnalyzer(t.Context(), st, func() game.Analyst { return stubAnalyst{} }, nil)
	a.analyze(t.Context(), seats)

	for _, id := range gameIDsOf(seats) {
		rec, err := st.GameRecordAnyOwner(t.Context(), id)
		if err != nil {
			t.Fatalf("read game %d: %v", id, err)
		}
		for _, m := range rec.Moves {
			if m.EvalCp == nil {
				t.Fatalf("game %d ply %d has no eval", id, m.Ply)
			}
		}
		// 마지막 手数만 After 로 남는다. 앞의 칸은 다음 회차가 Before 로 덮는다
		// (kifu/import.go 와 같은 모양).
		last := rec.Moves[len(rec.Moves)-1]
		if *last.EvalCp != len(rec.Moves)*10+1 {
			t.Errorf("game %d last eval = %d, want %d", id, *last.EvalCp, len(rec.Moves)*10+1)
		}
		if first := rec.Moves[0]; *first.EvalCp != 20 {
			t.Errorf("game %d first eval = %d, want 20 (2手째가 Before 로 덮는다)", id, *first.EvalCp)
		}
	}
}

// 엔진이 답을 못 하면 거기서 멈춘다. 판정 없이 남는 것이 잘못 채우는 것보다 낫고,
// 그때 화면은 「분석 중」이 아니라 「남지 않았다」로 돌아간다.
func TestAnalysisStopsWhenTheEngineFails(t *testing.T) {
	st, seats := matchSeatsForAnalysis(t, "", plyList(2))

	a := newMatchAnalyzer(t.Context(), st, func() game.Analyst { return stubAnalyst{fail: true} }, nil)
	a.analyze(t.Context(), seats)

	rec, err := st.GameRecordAnyOwner(t.Context(), seats[0].gameID)
	if err != nil {
		t.Fatalf("read game: %v", err)
	}
	for _, m := range rec.Moves {
		if m.EvalCp != nil {
			t.Errorf("ply %d got an eval %d, want none", m.Ply, *m.EvalCp)
		}
	}
}

// 한 판의 手가 둔 사람에게 간다. 판 번호 하나에만 쓰면 두 사람의 手가 한쪽에 쌓이고,
// 그 값은 아무 테스트도 안 잡는다 — 두 프로파일 다 멀쩡한 범위에 있다.
func TestSkillFromAMatchGoesToWhoPlayedTheMove(t *testing.T) {
	// 창(21~60手)을 다 덮는 길이다. 그 밖은 안 센다(skill.AnchorFromPly).
	st, seats := matchSeatsForAnalysis(t, "", plyList(skill.AnchorToPly))

	a := newMatchAnalyzer(t.Context(), st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	}, nil)
	a.analyze(t.Context(), seats)

	want := map[shogi.Color]float64{shogi.Black: 0.02, shogi.White: 0.20}
	// 창 안의 手는 40개이고 홀짝으로 반씩 갈린다.
	const wantSamples = (skill.AnchorToPly - skill.AnchorFromPly + 1) / 2

	got := map[shogi.Color]skill.Estimate{}
	for _, seat := range seats {
		e, ok, err := st.SkillProfile(t.Context(), seat.userID)
		if err != nil || !ok {
			t.Fatalf("%s 의 프로파일이 없다: ok=%v err=%v", seat.color, ok, err)
		}
		if math.Abs(e.AbsLoss-want[seat.color]) > 1e-6 {
			t.Errorf("%s AbsLoss = %v, want %v", seat.color, e.AbsLoss, want[seat.color])
		}
		if e.AbsSamples != wantSamples || e.Samples != wantSamples {
			t.Errorf("%s 표본 = %d·%d, want %d 둘 다 (창 밖의 手가 섞였다)",
				seat.color, e.Samples, e.AbsSamples, wantSamples)
		}
		got[seat.color] = skillEstimateOf(e)
	}

	// 밴드 축도 갈린다. 段級과 다른 자이지만 같은 手에서 나오므로 방향은 같아야 한다.
	if got[shogi.Black].Loss >= got[shogi.White].Loss {
		t.Errorf("Loss 가 안 갈렸다: 先手 %v · 後手 %v", got[shogi.Black].Loss, got[shogi.White].Loss)
	}
}

// 창을 다 지난 뒤에 엔진이 죽으면 그 표본은 남긴다. 창 뒤는 애초에 안 세는 구간이라
// 온전한 표본이고, 버리면 긴 판일수록 기여가 사라진다.
func TestAFailureAfterTheWindowKeepsTheSamples(t *testing.T) {
	st, seats := matchSeatsForAnalysis(t, "", plyList(skill.AnchorToPly+40))

	a := newMatchAnalyzer(t.Context(), st, func() game.Analyst {
		return stubAnalyst{failFrom: skill.AnchorToPly + 20, lossOdd: 0.02, lossEven: 0.20}
	}, nil)
	a.analyze(t.Context(), seats)

	want := map[shogi.Color]float64{shogi.Black: 0.02, shogi.White: 0.20}
	for _, seat := range seats {
		e, ok, err := st.SkillProfile(t.Context(), seat.userID)
		if err != nil || !ok {
			t.Fatalf("%s 의 온전한 창을 버렸다: ok=%v err=%v", seat.color, ok, err)
		}
		if math.Abs(e.AbsLoss-want[seat.color]) > 1e-6 {
			t.Errorf("%s AbsLoss = %v, want %v", seat.color, e.AbsLoss, want[seat.color])
		}
	}
}

// 手番이 시작 SFEN 에서 나온다. 駒落ち는 上手(後手)가 1手目를 두므로, `ply` 홀짝으로
// 가르면 두 사람의 값이 통째로 바뀐 채 멀쩡해 보인다(journal §88).
//
// 대인전은 지금 平手 확정이라 이 국면은 실제로 안 나온다. 그래도 재는 것은, 홀짝으로
// 되돌리는 회귀를 잡는 것이 여기 하나뿐이기 때문이다.
//
// 재는 것은 手番 하나다. 駒落ち에서 「이미 갈렸다」가 기준점을 안 빼는 것은 다른 자리의
// 문제이고 아직 안 고쳤다(journal §95의 남은 것).
func TestTheFirstMoverComesFromTheStartingPosition(t *testing.T) {
	kyoochi, ok := handicap.Find("kyoochi")
	if !ok {
		t.Fatal("香落ち 가 표에 없다")
	}
	st, seats := matchSeatsForAnalysis(t, kyoochi.SFEN, plyList(skill.AnchorToPly))

	a := newMatchAnalyzer(t.Context(), st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	}, nil)
	a.analyze(t.Context(), seats)

	// 1手目가 上手(後手)이므로 홀수 手의 낙폭이 後手에게 간다 — 平手의 반대다.
	want := map[shogi.Color]float64{shogi.White: 0.02, shogi.Black: 0.20}
	for _, seat := range seats {
		e, ok, err := st.SkillProfile(t.Context(), seat.userID)
		if err != nil || !ok {
			t.Fatalf("%s 의 프로파일이 없다: ok=%v err=%v", seat.color, ok, err)
		}
		if math.Abs(e.AbsLoss-want[seat.color]) > 1e-6 {
			t.Errorf("%s AbsLoss = %v, want %v (手番을 手数 홀짝으로 갈랐다)",
				seat.color, e.AbsLoss, want[seat.color])
		}
	}
}

// 시작 국면을 못 읽으면 아무에게도 안 쌓는다. 누가 뒀는지를 모르는 채로 쌓으면 그 값이
// 반은 남의 것이다.
//
// 평가치까지 같이 빠지는지는 여기서 안 잰다 — 실엔진은 판정 안에서 같은 문자열을
// 되만들다 같이 실패하지만(HasEvals), 이 stub 은 그 자리를 흉내 내지 않는다.
func TestAnUnreadableStartPositionFeedsNobody(t *testing.T) {
	st, seats := matchSeatsForAnalysis(t, "not a sfen", plyList(skill.AnchorToPly))

	a := newMatchAnalyzer(t.Context(), st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	}, nil)
	a.analyze(t.Context(), seats)

	for _, seat := range seats {
		if _, ok, err := st.SkillProfile(t.Context(), seat.userID); ok || err != nil {
			t.Errorf("%s 에 手番을 모르는 판이 쌓였다: ok=%v err=%v", seat.color, ok, err)
		}
	}
}

// 짧게 끝난 판은 창에 걸친 만큼만 들어간다. 버리는 것은 분석이 끊긴 판이지 짧은 판이
// 아니다 — 앵커도 그렇게 쟀다(journal §94).
func TestAGameThatEndsInsideTheWindowStillFeeds(t *testing.T) {
	const total = skill.AnchorFromPly + 9 // 21~30手가 창에 걸친다
	st, seats := matchSeatsForAnalysis(t, "", plyList(total))

	a := newMatchAnalyzer(t.Context(), st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	}, nil)
	a.analyze(t.Context(), seats)

	want := map[shogi.Color]float64{shogi.Black: 0.02, shogi.White: 0.20}
	for _, seat := range seats {
		e, ok, err := st.SkillProfile(t.Context(), seat.userID)
		if err != nil || !ok {
			t.Fatalf("%s 의 짧은 판을 버렸다: ok=%v err=%v", seat.color, ok, err)
		}
		if e.AbsSamples != (total-skill.AnchorFromPly+1)/2 {
			t.Errorf("%s AbsSamples = %d, want %d", seat.color, e.AbsSamples, (total-skill.AnchorFromPly+1)/2)
		}
		if math.Abs(e.AbsLoss-want[seat.color]) > 1e-6 {
			t.Errorf("%s AbsLoss = %v, want %v", seat.color, e.AbsLoss, want[seat.color])
		}
	}
}

// 기보에 구멍이 있으면 아무것도 안 한다. 색인을 手数로 쓰므로 한 칸이 비면 그 뒤가
// 전부 밀리고, 수순이 불법이 되는 것보다 手番이 뒤집히는 쪽이 조용해서 더 나쁘다.
//
// 기록기가 큐가 차면 이벤트를 버리고 계속하는 자리가 그것이다(dbRecorder.send).
func TestAGapInTheRecordStopsTheAnalysis(t *testing.T) {
	gapped := append(plyList(9), plyList(skill.AnchorToPly)[10:]...) // 10手目가 없다
	st, seats := matchSeatsForAnalysis(t, "", gapped)

	a := newMatchAnalyzer(t.Context(), st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	}, nil)
	a.analyze(t.Context(), seats)

	for _, seat := range seats {
		if _, ok, err := st.SkillProfile(t.Context(), seat.userID); ok || err != nil {
			t.Errorf("%s 에 구멍 난 기보가 쌓였다: ok=%v err=%v", seat.color, ok, err)
		}
		rec, err := st.GameRecordAnyOwner(t.Context(), seat.gameID)
		if err != nil {
			t.Fatalf("read game: %v", err)
		}
		if rec.Moves[0].EvalCp != nil {
			t.Error("구멍 난 기보에 평가치를 채웠다")
		}
	}
}

// 국면을 못 되만든 자리도 엔진 실패와 같은 규칙으로 끊는다. 그 뒤가 전부 같이 실패하므로
// 창 안에서 걸리면 남는 것이 더 긴 판의 앞부분뿐이다.
func TestAReplayFailureInsideTheWindowFeedsNobody(t *testing.T) {
	st, seats := matchSeatsForAnalysis(t, "", plyList(skill.AnchorToPly+40))

	a := newMatchAnalyzer(t.Context(), st, func() game.Analyst {
		return stubAnalyst{blindFrom: skill.AnchorFromPly + 10, lossOdd: 0.02, lossEven: 0.20}
	}, nil)
	a.analyze(t.Context(), seats)

	for _, seat := range seats {
		if _, ok, err := st.SkillProfile(t.Context(), seat.userID); ok || err != nil {
			t.Errorf("%s 에 창의 앞부분만 쌓였다: ok=%v err=%v", seat.color, ok, err)
		}
	}
}

// 한쪽 행에만 구멍이 나면 다른 행으로 잰다. 두 행을 기록기가 각자 쓰므로 한쪽만 비는
// 것이 실제 모양이고, 그때 멀쩡한 행이 있는데 판을 통째로 버리면 두 사람 다 잃는다.
func TestAGapInOneRowFallsBackToTheOther(t *testing.T) {
	const missing = 10
	gapped := append(plyList(missing-1), plyList(skill.AnchorToPly)[missing:]...)
	st, seats := matchSeatsForAnalysis(t, "", gapped)
	// 두 번째 자리의 행만 메운다 — 첫 행이 구멍 난 채로 남는다.
	if err := st.InsertMove(t.Context(), seats[1].gameID, missing, "7g7f"); err != nil {
		t.Fatalf("insert move: %v", err)
	}

	a := newMatchAnalyzer(t.Context(), st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	}, nil)
	a.analyze(t.Context(), seats)

	want := map[shogi.Color]float64{shogi.Black: 0.02, shogi.White: 0.20}
	for _, seat := range seats {
		e, ok, err := st.SkillProfile(t.Context(), seat.userID)
		if err != nil || !ok {
			t.Fatalf("%s 를 잃었다 — 멀쩡한 행이 있다: ok=%v err=%v", seat.color, ok, err)
		}
		if math.Abs(e.AbsLoss-want[seat.color]) > 1e-6 {
			t.Errorf("%s AbsLoss = %v, want %v", seat.color, e.AbsLoss, want[seat.color])
		}
	}
}

// 마지막 手에서 끊긴 것은 짧게 끝난 판과 같은 표본이다. 잃은 것이 그 한 手뿐인데
// 버리면 「끊겼나」가 아니라 「어느 手에서 끊겼나」로 결과가 갈린다.
func TestAFailureOnTheLastPlyKeepsTheSamples(t *testing.T) {
	const total = skill.AnchorToPly - 15 // 창 안에서 끝나는 판
	st, seats := matchSeatsForAnalysis(t, "", plyList(total))

	a := newMatchAnalyzer(t.Context(), st, func() game.Analyst {
		return stubAnalyst{failFrom: total, lossOdd: 0.02, lossEven: 0.20}
	}, nil)
	a.analyze(t.Context(), seats)

	for _, seat := range seats {
		if _, ok, err := st.SkillProfile(t.Context(), seat.userID); !ok || err != nil {
			t.Errorf("%s 를 잃었다 — 마지막 한 手만 못 잰 판이다: ok=%v err=%v", seat.color, ok, err)
		}
	}
}

// 한 手가 워커를 영영 붙잡지 못한다. 판정이 매 手 詰み solver 를 부르는데 그것이
// 스스로 안 끝나고 취소로만 풀리며(usi.Engine.SearchMate), 그 풀은 대국 중인 사람들과
// 공유다.
func TestAJudgementCannotHangTheAnalyzer(t *testing.T) {
	// 시한만 줄여서 잰다. 기본값(60초)으로 재면 이 테스트가 그만큼 걸린다.
	const deadline = 50 * time.Millisecond
	a := &matchAnalyzer{judgeDeadline: deadline}

	start := time.Now()
	_, err := a.judge(t.Context(), hangingAnalyst{}, shogi.StartSFEN, []string{"7g7f"}, 1)
	if err == nil {
		t.Fatal("멈추지 않는 판정이 답을 냈다")
	}
	if took := time.Since(start); took > 5*deadline {
		t.Errorf("시한 %v 을 한참 넘겨 %v 걸렸다", deadline, took)
	}
	if a.judgeDeadline = 0; a.deadlineOf() != analysisJudgeDeadline {
		t.Errorf("기본 시한 = %v, want %v", a.deadlineOf(), analysisJudgeDeadline)
	}
}

// hangingAnalyst 는 취소될 때까지 안 돌아온다 — `go mate infinite` 이 걸린 자리와 같다.
type hangingAnalyst struct{}

func (hangingAnalyst) Judge(ctx context.Context, _ string, _ []string, _ int) (game.Judgement, error) {
	<-ctx.Done()
	return game.Judgement{}, ctx.Err()
}

// 못 읽은 행이 있으면 실력을 안 쌓는다. 「판 끝까지인가」는 두 행을 견줘 아는 값이라
// 한쪽이 없으면 뜻이 없고, 그때 넣으면 긴 판이 짧게 끝난 판으로 들어간다.
func TestAnUnreadableRowBlocksTheSkillUpdate(t *testing.T) {
	st, seats := matchSeatsForAnalysis(t, "", plyList(skill.AnchorToPly))
	// 첫 자리를 없는 번호로 바꾼다 — 읽기가 실패하는 자리와 같은 모양이다.
	seats[0].gameID = -1

	a := newMatchAnalyzer(t.Context(), st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	}, nil)
	a.analyze(t.Context(), seats)

	for _, seat := range seats {
		if _, ok, err := st.SkillProfile(t.Context(), seat.userID); ok || err != nil {
			t.Errorf("%s 에 못 읽은 판이 쌓였다: ok=%v err=%v", seat.color, ok, err)
		}
	}
}

// 구멍 때문에 버린 행이 더 길면 고른 행은 뒤가 잘린 것이다. 그때 실력은 안 쌓는다 —
// 「구멍이 없다」만 보면 짧은 쪽이 이기고, 긴 판이 짧게 끝난 판으로 둔갑한다.
func TestAGappedButLongerRowBlocksTheSkillUpdate(t *testing.T) {
	const short = 30
	// 첫 자리는 구멍 난 채로 60手까지, 두 번째 자리는 성한 채로 30手까지.
	st, seats := matchSeatsForAnalysis(t, "", plyList(short))
	for _, ply := range plyList(skill.AnchorToPly)[short:] {
		if ply == short+5 {
			continue // 구멍
		}
		if err := st.InsertMove(t.Context(), seats[0].gameID, ply, "7g7f"); err != nil {
			t.Fatalf("insert move %d: %v", ply, err)
		}
	}

	a := newMatchAnalyzer(t.Context(), st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	}, nil)
	a.analyze(t.Context(), seats)

	for _, seat := range seats {
		if _, ok, err := st.SkillProfile(t.Context(), seat.userID); ok || err != nil {
			t.Errorf("%s 에 잘린 행이 쌓였다: ok=%v err=%v", seat.color, ok, err)
		}
	}
	// 평가치는 있는 만큼 채운다 — 그쪽은 手마다 독립이다.
	rec, err := st.GameRecordAnyOwner(t.Context(), seats[1].gameID)
	if err != nil {
		t.Fatalf("read game: %v", err)
	}
	if rec.Moves[0].EvalCp == nil {
		t.Error("평가치까지 건너뛰었다")
	}
}

// 뒤가 잘린 행은 「구멍이 없다」로 걸러지지 않는다 — 그 행도 빈틈없이 이어진다. 그대로
// 쓰면 그 판이 짧게 끝난 판으로 둔갑해 앞부분만 실력에 들어간다.
func TestATruncatedRowLosesToTheLongerOne(t *testing.T) {
	const short = 30
	st, seats := matchSeatsForAnalysis(t, "", plyList(short))
	// 두 번째 자리만 끝까지 채운다. 첫 자리는 뒤가 잘린 채로 남는다.
	for _, ply := range plyList(skill.AnchorToPly)[short:] {
		if err := st.InsertMove(t.Context(), seats[1].gameID, ply, "7g7f"); err != nil {
			t.Fatalf("insert move %d: %v", ply, err)
		}
	}

	a := newMatchAnalyzer(t.Context(), st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	}, nil)
	a.analyze(t.Context(), seats)

	// 창이 다 찼다는 것이 긴 행을 읽었다는 뜻이다. 잘린 행을 읽었으면 21~30手뿐이다.
	const wantSamples = (skill.AnchorToPly - skill.AnchorFromPly + 1) / 2
	for _, seat := range seats {
		e, ok, err := st.SkillProfile(t.Context(), seat.userID)
		if err != nil || !ok {
			t.Fatalf("%s 의 프로파일이 없다: ok=%v err=%v", seat.color, ok, err)
		}
		if e.AbsSamples != wantSamples {
			t.Errorf("%s AbsSamples = %d, want %d (뒤가 잘린 행을 읽었다)", seat.color, e.AbsSamples, wantSamples)
		}
	}
}

// 한 행이 통째로 비면 평가치는 다른 행으로 채우고 실력은 안 쌓는다. 빈 행은 판 길이에
// 대해 아무 말도 안 하므로, 남은 행이 끝까지인지 잘렸는지를 가릴 수가 없다.
func TestAnEmptyRowFillsEvalsButNotSkill(t *testing.T) {
	st, seats := matchSeatsForAnalysis(t, "", nil)
	for _, ply := range plyList(skill.AnchorToPly) {
		if err := st.InsertMove(t.Context(), seats[1].gameID, ply, "7g7f"); err != nil {
			t.Fatalf("insert move %d: %v", ply, err)
		}
	}

	a := newMatchAnalyzer(t.Context(), st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	}, nil)
	a.analyze(t.Context(), seats)

	for _, seat := range seats {
		if _, ok, err := st.SkillProfile(t.Context(), seat.userID); ok || err != nil {
			t.Errorf("%s 에 견줄 것 없는 판이 쌓였다: ok=%v err=%v", seat.color, ok, err)
		}
	}
	// 성한 행의 평가치는 채운다 — 그쪽은 手마다 독립이다.
	rec, err := st.GameRecordAnyOwner(t.Context(), seats[1].gameID)
	if err != nil {
		t.Fatalf("read game: %v", err)
	}
	if rec.Moves[0].EvalCp == nil {
		t.Error("성한 행의 평가치까지 건너뛰었다")
	}
}

// 창(21手) 앞에서 끝난 판은 아무에게도 안 쌓인다. 46手에 끝난 판이 실제로 그랬다
// (journal §94 — 그 판은 창에 남은 手가 0개였다).
func TestAShortMatchFeedsNobody(t *testing.T) {
	st, seats := matchSeatsForAnalysis(t, "", plyList(skill.AnchorFromPly-1))

	a := newMatchAnalyzer(t.Context(), st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	}, nil)
	a.analyze(t.Context(), seats)

	for _, seat := range seats {
		if _, ok, err := st.SkillProfile(t.Context(), seat.userID); ok || err != nil {
			t.Errorf("%s 에 창 밖의 手가 쌓였다: ok=%v err=%v", seat.color, ok, err)
		}
	}
}

// 반쪽으로 끝난 판은 추정에 안 들어간다. 남는 것이 초반·중반뿐인데 그 구간이 체계적으로
// 쉬워서 낙폭이 낮게 나온다 — 평가치는 앞쪽까지 채우고 추정만 버린다.
func TestAHalfAnalyzedMatchFeedsNobody(t *testing.T) {
	st, seats := matchSeatsForAnalysis(t, "", plyList(skill.AnchorToPly))

	a := newMatchAnalyzer(t.Context(), st, func() game.Analyst {
		return stubAnalyst{failFrom: skill.AnchorFromPly + 10, lossOdd: 0.02, lossEven: 0.20}
	}, nil)
	a.analyze(t.Context(), seats)

	for _, seat := range seats {
		if _, ok, err := st.SkillProfile(t.Context(), seat.userID); ok || err != nil {
			t.Errorf("%s 에 반쪽 판이 쌓였다: ok=%v err=%v", seat.color, ok, err)
		}
	}
	// 앞쪽 평가치는 그대로 남는다.
	rec, err := st.GameRecordAnyOwner(t.Context(), seats[0].gameID)
	if err != nil {
		t.Fatalf("read game: %v", err)
	}
	if rec.Moves[0].EvalCp == nil {
		t.Error("멈추기 전의 평가치까지 버렸다")
	}
}

// 手番은 手数 홀짝이 아니라 1手目를 둔 색에서 나온다. 駒落ち는 上手(後手)가 먼저 두므로
// 홀짝으로 가르면 그날 실력이 반대 사람에게 쌓인다(journal §88).
func TestMoverFollowsTheSideThatMovedFirst(t *testing.T) {
	for _, first := range []shogi.Color{shogi.Black, shogi.White} {
		if got := moverAt(first, 1); got != first {
			t.Errorf("first=%s 1手目 = %s, want %s", first, got, first)
		}
		if got := moverAt(first, 2); got != first.Other() {
			t.Errorf("first=%s 2手目 = %s, want %s", first, got, first.Other())
		}
		if got := moverAt(first, 21); got != first {
			t.Errorf("first=%s 21手目 = %s, want %s", first, got, first)
		}
	}
}

// 줄에 서는 순간부터 「분석 중」이다. 워커가 그 판에 닿기 전이라도 화면이 기다릴 것을
// 알아야 한다.
func TestQueuedGamesReadAsAnalyzing(t *testing.T) {
	a := &matchAnalyzer{queue: make(chan analysisJob, 1), pending: map[int64]struct{}{}}
	seats := []analysisSeat{
		{gameID: 7, userID: 1, color: shogi.Black},
		{gameID: 8, userID: 2, color: shogi.White},
	}

	if a.analyzing(7) {
		t.Error("아무것도 안 세운 판이 분석 중이다")
	}
	a.enqueue(seats, 0)
	if !a.analyzing(7) || !a.analyzing(8) {
		t.Error("줄에 선 판이 분석 중이 아니다")
	}
	a.forget(gameIDsOf(seats))
	if a.analyzing(7) || a.analyzing(8) {
		t.Error("끝난 판이 아직 분석 중이다")
	}
}

// 분석기가 없어도 부르는 쪽이 안 죽는다. 엔진 없는 배포에서 대인전이 그대로 도는
// 규약이라, 그 배포에서는 이 값이 nil 인 채로 같은 자리를 지난다.
func TestANilAnalyzerIsSafeToUse(t *testing.T) {
	var a *matchAnalyzer
	a.hold(1)
	a.enqueue([]analysisSeat{{gameID: 1}, {gameID: 2}}, 0)
	a.forget([]int64{1, 2})
	if a.analyzing(1) {
		t.Error("없는 분석기가 분석 중이라고 답했다")
	}
	if newMatchAnalyzer(t.Context(), nil, func() game.Analyst { return stubAnalyst{} }, nil) != nil {
		t.Error("store 가 없으면 분석기를 만들지 않는다")
	}
}

// plyList 는 1부터 n까지의 手数다. 구멍을 뚫어 보려면 부르는 쪽이 직접 만든다.
func plyList(n int) []int {
	out := make([]int, 0, n)
	for i := range n {
		out = append(out, i+1)
	}
	return out
}

// matchSeatsForAnalysis 는 대인전 한 판(행 둘)을 만들고 그 手数들을 넣어 둔다.
//
// 수는 전부 같은 문자열이다. 분석기가 수를 두어 보지 않으므로(엔진이 판정한다) 합법일
// 필요가 없고, 여기서 재는 것은 手数와 자리다.
func matchSeatsForAnalysis(t *testing.T, startSFEN string, plies []int) (*store.Store, []analysisSeat) {
	t.Helper()
	url := os.Getenv("SHOWGI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SHOWGI_TEST_DATABASE_URL 미설정 — DB 테스트 건너뜀")
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)

	matchID := "analysis-" + time.Now().Format("150405.000000000")
	seats := make([]analysisSeat, 0, 2)
	for _, seat := range []struct {
		color shogi.Color
		code  string
	}{{shogi.Black, "b"}, {shogi.White, "w"}} {
		u, err := st.UpsertUser(t.Context(), "test", matchID+"-"+seat.code, "テスト")
		if err != nil {
			t.Fatalf("upsert user %s: %v", seat.code, err)
		}
		id, err := st.CreateMatchGame(t.Context(), u, seat.code, startSFEN, matchID)
		if err != nil {
			t.Fatalf("create match game %s: %v", seat.code, err)
		}
		for _, ply := range plies {
			if err := st.InsertMove(t.Context(), id, ply, "7g7f"); err != nil {
				t.Fatalf("insert move %d: %v", ply, err)
			}
		}
		seats = append(seats, analysisSeat{gameID: id, userID: u, color: seat.color})
	}
	return st, seats
}

// 밀린 양이 판과 手 둘로 세어진다. 판 수만으로는 밀린 일의 크기를 못 말한다 —
// 회차 4의 세 판이 27·34·123手였다(journal §91).
func TestBacklogCountsGamesAndPlies(t *testing.T) {
	reg := metrics.New("api", "test")
	a := &matchAnalyzer{
		queue:    make(chan analysisJob, 2),
		pending:  map[int64]struct{}{},
		analysis: reg.Analysis(),
	}

	a.enqueue([]analysisSeat{{gameID: 1}, {gameID: 2}}, 27)
	a.enqueue([]analysisSeat{{gameID: 3}, {gameID: 4}}, 123)
	if got := reg.AnalysisBacklogGames.Total(); got != 2 {
		t.Errorf("밀린 판 = %v, want 2", got)
	}
	if got := reg.AnalysisBacklogPlies.Total(); got != 150 {
		t.Errorf("밀린 手 = %v, want 150", got)
	}

	a.took(<-a.queue)
	if got := reg.AnalysisBacklogPlies.Total(); got != 123 {
		t.Errorf("꺼낸 뒤 밀린 手 = %v, want 123", got)
	}
	a.took(<-a.queue)
	if got := reg.AnalysisBacklogGames.Total(); got != 0 {
		t.Errorf("다 꺼낸 뒤 밀린 판 = %v, want 0", got)
	}
}

// 줄이 넘쳐 버린 판은 dropped 로 세어진다. 지금까지 로그 한 줄로만 남던 자리다.
func TestDroppedGamesAreCounted(t *testing.T) {
	reg := metrics.New("api", "test")
	a := &matchAnalyzer{
		queue:    make(chan analysisJob, 1),
		pending:  map[int64]struct{}{},
		analysis: reg.Analysis(),
	}

	a.enqueue([]analysisSeat{{gameID: 1}, {gameID: 2}}, 40)
	a.enqueue([]analysisSeat{{gameID: 3}, {gameID: 4}}, 40)

	if got := reg.AnalysisGames.Total(); got != 1 {
		t.Fatalf("센 판 = %v, want 1", got)
	}
	// 버려진 판은 「분석 중」 표시가 걷혀 있어야 한다. 안 걷으면 되짚기가 영영
	// 「分析しています」로 남는다.
	if a.analyzing(3) || a.analyzing(4) {
		t.Error("버려진 판이 아직 분석 중이다")
	}
	// 밀린 양에도 안 들어간다. 줄에 못 섰으므로 그 手는 아무도 안 잰다.
	if got := reg.AnalysisBacklogPlies.Total(); got != 40 {
		t.Errorf("밀린 手 = %v, want 40", got)
	}
}
