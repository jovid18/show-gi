package server

import (
	"context"
	"errors"
	"math"
	"os"
	"sync/atomic"
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
	st, _, seats := matchSeatsForAnalysis(t, "", plyList(3))

	a := analyzerFor(st, func() game.Analyst { return stubAnalyst{} })
	a.analyze(t.Context(), "", seats)

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
	st, _, seats := matchSeatsForAnalysis(t, "", plyList(2))

	a := analyzerFor(st, func() game.Analyst { return stubAnalyst{fail: true} })
	a.analyze(t.Context(), "", seats)

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
	st, _, seats := matchSeatsForAnalysis(t, "", plyList(skill.AnchorToPly))

	a := analyzerFor(st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	})
	a.analyze(t.Context(), "", seats)

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
	st, _, seats := matchSeatsForAnalysis(t, "", plyList(skill.AnchorToPly+40))

	a := analyzerFor(st, func() game.Analyst {
		return stubAnalyst{failFrom: skill.AnchorToPly + 20, lossOdd: 0.02, lossEven: 0.20}
	})
	a.analyze(t.Context(), "", seats)

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
	st, _, seats := matchSeatsForAnalysis(t, kyoochi.SFEN, plyList(skill.AnchorToPly))

	a := analyzerFor(st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	})
	a.analyze(t.Context(), "", seats)

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
	st, _, seats := matchSeatsForAnalysis(t, "not a sfen", plyList(skill.AnchorToPly))

	a := analyzerFor(st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	})
	a.analyze(t.Context(), "", seats)

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
	st, _, seats := matchSeatsForAnalysis(t, "", plyList(total))

	a := analyzerFor(st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	})
	a.analyze(t.Context(), "", seats)

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
	st, _, seats := matchSeatsForAnalysis(t, "", gapped)

	a := analyzerFor(st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	})
	a.analyze(t.Context(), "", seats)

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
	st, _, seats := matchSeatsForAnalysis(t, "", plyList(skill.AnchorToPly+40))

	a := analyzerFor(st, func() game.Analyst {
		return stubAnalyst{blindFrom: skill.AnchorFromPly + 10, lossOdd: 0.02, lossEven: 0.20}
	})
	a.analyze(t.Context(), "", seats)

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
	st, _, seats := matchSeatsForAnalysis(t, "", gapped)
	// 두 번째 자리의 행만 메운다 — 첫 행이 구멍 난 채로 남는다.
	if err := st.InsertMove(t.Context(), seats[1].gameID, missing, "7g7f"); err != nil {
		t.Fatalf("insert move: %v", err)
	}

	a := analyzerFor(st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	})
	a.analyze(t.Context(), "", seats)

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
	st, _, seats := matchSeatsForAnalysis(t, "", plyList(total))

	a := analyzerFor(st, func() game.Analyst {
		return stubAnalyst{failFrom: total, lossOdd: 0.02, lossEven: 0.20}
	})
	a.analyze(t.Context(), "", seats)

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
	st, _, seats := matchSeatsForAnalysis(t, "", plyList(skill.AnchorToPly))
	// 첫 자리를 없는 번호로 바꾼다 — 읽기가 실패하는 자리와 같은 모양이다.
	seats[0].gameID = -1

	a := analyzerFor(st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	})
	a.analyze(t.Context(), "", seats)

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
	st, _, seats := matchSeatsForAnalysis(t, "", plyList(short))
	for _, ply := range plyList(skill.AnchorToPly)[short:] {
		if ply == short+5 {
			continue // 구멍
		}
		if err := st.InsertMove(t.Context(), seats[0].gameID, ply, "7g7f"); err != nil {
			t.Fatalf("insert move %d: %v", ply, err)
		}
	}

	a := analyzerFor(st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	})
	a.analyze(t.Context(), "", seats)

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
	st, _, seats := matchSeatsForAnalysis(t, "", plyList(short))
	// 두 번째 자리만 끝까지 채운다. 첫 자리는 뒤가 잘린 채로 남는다.
	for _, ply := range plyList(skill.AnchorToPly)[short:] {
		if err := st.InsertMove(t.Context(), seats[1].gameID, ply, "7g7f"); err != nil {
			t.Fatalf("insert move %d: %v", ply, err)
		}
	}

	a := analyzerFor(st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	})
	a.analyze(t.Context(), "", seats)

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
	st, _, seats := matchSeatsForAnalysis(t, "", nil)
	for _, ply := range plyList(skill.AnchorToPly) {
		if err := st.InsertMove(t.Context(), seats[1].gameID, ply, "7g7f"); err != nil {
			t.Fatalf("insert move %d: %v", ply, err)
		}
	}

	a := analyzerFor(st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	})
	a.analyze(t.Context(), "", seats)

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
	st, _, seats := matchSeatsForAnalysis(t, "", plyList(skill.AnchorFromPly-1))

	a := analyzerFor(st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.02, lossEven: 0.20}
	})
	a.analyze(t.Context(), "", seats)

	for _, seat := range seats {
		if _, ok, err := st.SkillProfile(t.Context(), seat.userID); ok || err != nil {
			t.Errorf("%s 에 창 밖의 手가 쌓였다: ok=%v err=%v", seat.color, ok, err)
		}
	}
}

// 반쪽으로 끝난 판은 추정에 안 들어간다. 남는 것이 초반·중반뿐인데 그 구간이 체계적으로
// 쉬워서 낙폭이 낮게 나온다 — 평가치는 앞쪽까지 채우고 추정만 버린다.
func TestAHalfAnalyzedMatchFeedsNobody(t *testing.T) {
	st, _, seats := matchSeatsForAnalysis(t, "", plyList(skill.AnchorToPly))

	a := analyzerFor(st, func() game.Analyst {
		return stubAnalyst{failFrom: skill.AnchorFromPly + 10, lossOdd: 0.02, lossEven: 0.20}
	})
	a.analyze(t.Context(), "", seats)

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
	st, matchID, seats := matchSeatsForAnalysis(t, "", plyList(2))
	a := &matchAnalyzer{store: st}
	one, two := seats[0].gameID, seats[1].gameID

	if a.analyzing(t.Context(), one) {
		t.Error("아무것도 안 세운 판이 분석 중이다")
	}

	// 자리 하나만 왔을 때부터 표시된다. 화면이 자기 번호만 알면 되짚기를 여는데,
	// 그때 「남지 않았다」로 보이면 그래프가 거기 굳는다.
	a.hold(t.Context(), matchID)
	if !a.analyzing(t.Context(), one) || !a.analyzing(t.Context(), two) {
		t.Error("세운 판이 분석 중이 아니다")
	}

	a.enqueue(t.Context(), matchID, 2)
	if !a.analyzing(t.Context(), one) {
		t.Error("줄에 선 판이 분석 중이 아니다")
	}

	a.dropJob(t.Context(), matchID)
	if a.analyzing(t.Context(), one) || a.analyzing(t.Context(), two) {
		t.Error("끝난 판이 아직 분석 중이다")
	}
}

// 분석기가 없어도 부르는 쪽이 안 죽는다. 엔진 없는 배포에서 대인전이 그대로 도는
// 규약이라, 그 배포에서는 이 값이 nil 인 채로 같은 자리를 지난다.
func TestANilAnalyzerIsSafeToUse(t *testing.T) {
	var a *matchAnalyzer
	a.hold(t.Context(), "nil-1")
	a.enqueue(t.Context(), "nil-1", 0)
	a.dropJob(t.Context(), "nil-1")
	if a.analyzing(t.Context(), 1) {
		t.Error("없는 분석기가 분석 중이라고 답했다")
	}
	if newMatchAnalyzer(t.Context(), nil, func() game.Analyst { return stubAnalyst{} }, nil, 1) != nil {
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
func matchSeatsForAnalysis(t *testing.T, startSFEN string, plies []int) (*store.Store, string, []analysisSeat) {
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
	return st, matchID, seats
}

// 밀린 양이 판과 手 둘로 세어진다. 판 수만으로는 밀린 일의 크기를 못 말한다 —
// 회차 4의 세 판이 27·34·123手였다(journal §91).
func TestBacklogCountsGamesAndPlies(t *testing.T) {
	stA, matchA, _ := matchSeatsForAnalysis(t, "", plyList(1))
	_, matchB, _ := matchSeatsForAnalysis(t, "", plyList(1))
	clearQueues(t, stA)
	reg := metrics.New("api", "test")
	a := &matchAnalyzer{store: stA, analysis: reg.Analysis()}
	t.Cleanup(func() {
		a.dropJob(context.Background(), matchA)
		a.dropJob(context.Background(), matchB)
	})

	a.enqueue(t.Context(), matchA, 27)
	a.enqueue(t.Context(), matchB, 123)
	a.sampleBacklog(t.Context())
	if got := reg.AnalysisBacklogGames.Total(); got != 2 {
		t.Errorf("밀린 판 = %v, want 2", got)
	}
	if got := reg.AnalysisBacklogPlies.Total(); got != 150 {
		t.Errorf("밀린 手 = %v, want 150", got)
	}

	// 집어 간 판은 밀린 것이 아니다 — 지금 도는 일이다.
	if _, err := stA.ClaimAnalysisJob(t.Context(), time.Now().Add(-jobLease)); err != nil {
		t.Fatalf("claim: %v", err)
	}
	a.sampleBacklog(t.Context())
	if got := reg.AnalysisBacklogGames.Total(); got != 1 {
		t.Errorf("집어 간 뒤 밀린 판 = %v, want 1", got)
	}
	if got := reg.AnalysisBacklogPlies.Total(); got != 123 {
		t.Errorf("집어 간 뒤 밀린 手 = %v, want 123", got)
	}
}

// 자리가 반쪽인 판은 줄에서 걷히고 버린 것으로 세어진다.
//
// 걷지 않으면 되짚기가 영영 「分析しています」로 남는다. 자리를 표에 옮겨 적지 않으므로
// 반쪽인지는 games 를 읽어야 알고, 그 판정이 워커 쪽으로 옮겨 왔다(seatsOf).
func TestAHalfMatchLeavesTheQueue(t *testing.T) {
	st := testStore(t)
	clearQueues(t, st)
	reg := metrics.New("api", "test")
	a := &matchAnalyzer{store: st, analysis: reg.Analysis()}

	// 그 방의 games 행이 없다 — 자리가 0개인 판이다.
	const matchID = "half-1"
	t.Cleanup(func() { a.dropJob(context.Background(), matchID) })
	a.hold(t.Context(), matchID)
	a.enqueue(t.Context(), matchID, 40)

	if !a.runOneJob(t.Context()) {
		t.Fatal("줄에 선 판을 안 집었다")
	}
	if got := reg.AnalysisGames.SumFunc(func(l map[string]string) bool {
		return l["result"] == metrics.AnalysisDropped
	}); got != 1 {
		t.Errorf("버린 판 = %v, want 1", got)
	}
	a.sampleBacklog(t.Context())
	if got := reg.AnalysisBacklogGames.Total(); got != 0 {
		t.Errorf("걷힌 뒤 밀린 판 = %v, want 0", got)
	}
}

// 프로세스가 사라져도 줄에 선 판은 안 없어진다.
//
// 메모리 채널이던 동안은 재배포 한 번이 46판을 평가치 없이 남겼다(journal §105).
// 이제 판이 표에 있으므로 다음 워커가 그대로 집는다 — 리스가 낡기를 기다릴 것도 없이,
// 그 판을 집었던 프로세스가 아예 없었던 것과 같다.
func TestAQueuedGameSurvivesTheProcess(t *testing.T) {
	st := testStore(t)
	clearQueues(t, st)
	const matchID = "survive-1"
	gone := &matchAnalyzer{store: st, analysis: metrics.New("api", "test").Analysis()}
	t.Cleanup(func() { gone.dropJob(context.Background(), matchID) })

	gone.hold(t.Context(), matchID)
	gone.enqueue(t.Context(), matchID, 40)

	// 그 분석기는 이제 없다. 새로 뜬 쪽이 같은 판을 본다.
	fresh := &matchAnalyzer{store: st, analysis: metrics.New("api", "test").Analysis()}
	job, err := fresh.store.ClaimAnalysisJob(t.Context(), time.Now().Add(-jobLease))
	if err != nil {
		t.Fatalf("새 워커가 그 판을 못 집었다: %v", err)
	}
	if job.MatchID != matchID {
		t.Errorf("집은 판 = %q, want %q", job.MatchID, matchID)
	}
	if job.Plies != 40 {
		t.Errorf("안 잰 手数 = %d, want 40", job.Plies)
	}
}

// countingAnalyst 는 몇 번 불렸는지만 센다.
type countingAnalyst struct {
	calls *int
}

func (c countingAnalyst) Judge(context.Context, string, []string, int) (game.Judgement, error) {
	*c.calls++
	return game.Judgement{}, errors.New("engine down")
}

func repeatMove(n int) []string {
	out := make([]string, 0, n)
	for range n {
		out = append(out, "7g7f")
	}
	return out
}

// measureAhead 는 그 판의 手가 want 개 재어질 때까지 워커가 하는 일을 손으로 한다.
//
// 횟수로 안 센다. 집는 질의가 판을 안 가리므로(query/analysis.sql) 남의 자리가 남긴 행을
// 먼저 집을 수 있고, 그러면 「세 번 불렀으니 셋이 재어졌다」가 성립하지 않는다.
func measureAhead(t *testing.T, a *matchAnalyzer, matchID string, want int) {
	t.Helper()
	for range want * 4 {
		if a.aheadCount(t.Context(), matchID) >= want {
			return
		}
		if !a.measureOnePly(t.Context(), stubAnalyst{}) {
			break
		}
	}
	if got := a.aheadCount(t.Context(), matchID); got != want {
		t.Fatalf("measured ahead = %d, want %d", got, want)
	}
}

// analyzerFor 는 판이 끝난 뒤의 분석만 보는 분석기다. 워커를 안 띄운다.
//
// 띄우면 그 워커가 다음 테스트의 手를 집어 간다 — 줄이 표가 된 뒤로 집는 질의가 판을
// 안 가리기 때문이다(query/analysis.sql). 워커가 실제로 도는 것을 재는 자리는
// TestTheWorkerCountIsHonoured 하나다.
func analyzerFor(st *store.Store, newAnalyst func() game.Analyst) *matchAnalyzer {
	return &matchAnalyzer{
		store:      st,
		newAnalyst: newAnalyst,
		drain:      make(chan plyJob, drainBuffer),
		analysis:   metrics.New("api", "test").Analysis(),
	}
}

// testStore 는 표만 있으면 되는 테스트가 쓰는 연결이다.
func testStore(t *testing.T) *store.Store {
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
	return st
}

// clearPlies 는 미리 재는 줄을 비운다.
//
// 집는 질의가 판을 안 가리므로(ClaimAnalysisPly) 남의 회차가 남긴 행이 있으면 이 회차의
// 워커가 그것을 집는다. 비워도 잃는 사실이 없다 — 그 手는 판이 끝날 때 재어진다.
func clearPlies(t *testing.T, st *store.Store) {
	t.Helper()
	if err := st.SweepAnalysisPlies(t.Context(), time.Now()); err != nil {
		t.Fatalf("clear the ply queue: %v", err)
	}
}

// clearQueues 는 두 줄을 다 비운다. 집는 질의가 판을 안 가리므로 남의 회차가 남긴 행이
// 있으면 이 회차의 셈에 들어온다.
func clearQueues(t *testing.T, st *store.Store) {
	t.Helper()
	clearPlies(t, st)
	if err := st.SweepAnalysisJobs(t.Context(), time.Now()); err != nil {
		t.Fatalf("clear the job queue: %v", err)
	}
}

// plyAnalyzer 는 미리 재는 줄만 쓰는 분석기와 그 판의 id 를 준다.
//
// 워커는 안 띄운다. 여기서 재는 것이 줄의 셈이고, 워커가 돌면 같은 手를 그쪽이 가져갈
// 수 있어서 답이 실행마다 달라진다 — 워커가 하는 일은 손으로 한다(measureOnePly).
func plyAnalyzer(t *testing.T, st *store.Store) (*matchAnalyzer, string) {
	t.Helper()
	if st == nil {
		st = testStore(t)
	}
	clearQueues(t, st)
	a := &matchAnalyzer{
		store:    st,
		drain:    make(chan plyJob, drainBuffer),
		analysis: metrics.New("api", "test").Analysis(),
	}
	matchID := "plies-" + time.Now().Format("150405.000000000")
	t.Cleanup(func() { a.discard(context.Background(), matchID) })
	return a, matchID
}

// drainOne 은 배수구에 있는 것 하나를 표로 옮긴다. 프로덕션에서는 drainPlies 가 한다.
func drainOne(t *testing.T, a *matchAnalyzer) {
	t.Helper()
	select {
	case p := <-a.drain:
		a.writePly(t.Context(), p)
	default:
		t.Fatal("배수구가 비어 있다")
	}
}

// 두는 동안 잰 手는 판이 끝날 때 다시 안 잰다. 안 그러면 미리 재는 것이 일을 두 번
// 하는 것으로 끝나고, 판이 끝나는 순간의 봉우리도 그대로 남는다(journal §105).
//
//	SHOWGI_TEST_DATABASE_URL=postgres://showgi:showgi@localhost:5432/showgi go test ./internal/server/
func TestAMoveMeasuredWhilePlayingIsNotMeasuredAgain(t *testing.T) {
	st, _, seats := matchSeatsForAnalysis(t, "", plyList(3))
	a, matchID := plyAnalyzer(t, st)
	// 판이 끝날 때 쓰는 판정기는 죽어 있다. 그래도 완주하면 미리 잰 것을 쓴 것이다.
	a.newAnalyst = func() game.Analyst { return stubAnalyst{fail: true} }

	for ply := 1; ply <= 3; ply++ {
		a.prefetch(matchID, startSFENOf(""), repeatMove(ply), ply)
		drainOne(t, a)
	}
	measureAhead(t, a, matchID, 3)

	if got := a.analyze(t.Context(), matchID, seats); got != metrics.AnalysisDone {
		t.Fatalf("analyze = %q, want %q", got, metrics.AnalysisDone)
	}

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
		// 미리 잰 값이 그대로 들어갔는지 본다. 마지막 手만 After 로 남는다.
		if last := rec.Moves[len(rec.Moves)-1]; *last.EvalCp != 31 {
			t.Errorf("game %d last eval = %d, want 31", id, *last.EvalCp)
		}
	}

	a.discard(t.Context(), matchID)
	if got := a.aheadCount(t.Context(), matchID); got != 0 {
		t.Errorf("measured ahead after discard = %d, want 0", got)
	}
}

// 한 手가 실패하면 그 판은 미리 재는 것을 그만둔다. 뒤의 手도 전부 같은 자리에서
// 실패하므로, 안 그만두면 남은 手数만큼 탐색을 버린다.
func TestALookAheadFailureStopsMeasuringThatGame(t *testing.T) {
	a, matchID := plyAnalyzer(t, nil)
	for ply := 1; ply <= 2; ply++ {
		a.prefetch(matchID, startSFENOf(""), repeatMove(ply), ply)
		drainOne(t, a)
	}

	if !a.measureOnePly(t.Context(), stubAnalyst{fail: true}) {
		t.Fatal("첫 手를 못 집었다")
	}

	// 그만둔 판의 手는 아예 안 잡힌다. 집는 질의가 그 행을 안 준다.
	calls := 0
	if a.measureOnePly(t.Context(), countingAnalyst{&calls}) {
		t.Error("그만둔 판의 手를 다시 집었다")
	}
	if calls != 0 {
		t.Errorf("judge calls after giving up = %d, want 0", calls)
	}
	// 밀린 양에서도 빠진다. 아무도 재지 않을 것을 세면 백로그가 안 내려온다.
	if got, err := a.store.CountAnalysisBacklog(t.Context()); err != nil || got != 0 {
		t.Errorf("backlog = %d (err %v), want 0", got, err)
	}
}

// 판이 줄을 떠난 뒤에 도착한 미리 재기는 자리를 다시 만들지 않는다. 만들면 그 항목을
// 아무도 안 지워서 판마다 하나씩 샌다 — 워커가 둘 이상일 때 생기는 자리다(journal §106).
func TestALateMeasurementDoesNotResurrectTheMatch(t *testing.T) {
	a, matchID := plyAnalyzer(t, nil)
	a.prefetch(matchID, startSFENOf(""), repeatMove(1), 1)
	drainOne(t, a)

	// 판이 끝나고 자리가 걷혔다. 그 뒤에 늦은 것이 도착한다.
	a.discard(t.Context(), matchID)
	a.remember(t.Context(), matchID, judged{move: skill.Move{Ply: 1}})

	if got := a.aheadCount(t.Context(), matchID); got != 0 {
		t.Errorf("a late measurement put the match back: %d rows", got)
	}
}

// 워커가 사라진 手는 리스가 낡으면 도로 집힌다. 배포와 스팟 회수가 그 자리이고,
// 안 되찾으면 그 手를 판이 끝날 때까지 아무도 안 잰다.
func TestAStaleClaimIsTakenBack(t *testing.T) {
	a, matchID := plyAnalyzer(t, nil)
	a.prefetch(matchID, startSFENOf(""), repeatMove(1), 1)
	drainOne(t, a)

	got, err := a.store.ClaimAnalysisPly(t.Context(), time.Now().Add(-plyLease))
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if got.MatchID != matchID || got.Ply != 1 {
		t.Fatalf("claimed %q ply %d, want %q ply 1", got.MatchID, got.Ply, matchID)
	}

	// 방금 집은 것은 다시 안 잡힌다.
	_, err = a.store.ClaimAnalysisPly(t.Context(), time.Now().Add(-plyLease))
	if !errors.Is(err, store.ErrNoAnalysisPly) {
		t.Errorf("a fresh claim was taken again: %v", err)
	}
	// 리스가 낡으면 잡힌다.
	if _, err := a.store.ClaimAnalysisPly(t.Context(), time.Now().Add(time.Minute)); err != nil {
		t.Errorf("a stale claim was not taken back: %v", err)
	}
}

// 배수구가 차도 착수가 안 막힌다. 넘친 手는 판이 끝날 때 그 자리에서 잰다.
//
// 여기에 DB 가 필요 없다. 배수구는 표에 적기 전의 자리라, 착수 경로가 DB 를 안 기다리는
// 것이 이 자리에서 재는 전부다.
func TestAFullDrainDoesNotBlockTheMove(t *testing.T) {
	a := &matchAnalyzer{
		drain:    make(chan plyJob, 1),
		analysis: metrics.New("api", "test").Analysis(),
	}
	for ply := 1; ply <= 5; ply++ {
		a.prefetch("drain-1", startSFENOf(""), repeatMove(ply), ply)
	}
	if got := len(a.drain); got != 1 {
		t.Errorf("queued = %d, want 1", got)
	}
}

// 판이 끝나면 그 판의 남은 手가 밀린 양에 한 번만 남는다.
//
// 안 끊으면 그 手가 표에도 남고 queuedPlies 에도 더해져 두 번 세어진다. 프로덕션에서
// 실제로 그렇게 부풀었고(journal §116), 이 값이 오토스케일의 신호라 두 번 세면
// 스케일러가 과잉 대응한다.
func TestTheBacklogCountsAnUnmeasuredMoveOnce(t *testing.T) {
	a, matchID := plyAnalyzer(t, nil)
	reg := metrics.New("api", "test")
	a.analysis = reg.Analysis()
	const plies = 10

	// 세우기만 하고 아무것도 안 잰다. 판이 끝나면 열 手 전부를 analyze 가 맡는다.
	for ply := 1; ply <= plies; ply++ {
		a.prefetch(matchID, startSFENOf(""), repeatMove(ply), ply)
		drainOne(t, a)
	}
	a.sampleBacklog(t.Context())
	base := reg.AnalysisBacklogPlies.Total() - float64(plies)

	a.enqueue(t.Context(), matchID, plies)
	a.sampleBacklog(t.Context())
	if got := reg.AnalysisBacklogPlies.Total() - base; got != plies {
		t.Errorf("밀린 手 = %v, want %d (표와 줄이 같은 手를 같이 세고 있다)", got, plies)
	}

	// 끊은 手는 아무도 못 집는다. 집으면 analyze 와 같은 국면을 두 번 잰다.
	if a.measureOnePly(t.Context(), stubAnalyst{}) {
		t.Error("판이 끝난 뒤에도 그 판의 手가 집혔다")
	}
}

// 미리 다 잰 판이 줄에 서면 그 판이 드는 일이 0이다.
//
// 세는 값과 실제가 어긋나면 차액이 영구히 남는다. 지표만 보면 「밀려 있다」로 읽히는데
// 줄은 비어 있어서, 그 상태로는 밀린 것인지 못 센 것인지 가릴 수 없다.
//
// 게이지가 아니라 표를 본다. 게이지는 프로세스가 보는 전역 합이라 남의 자리가 남긴 행이
// 섞이고, 여기서 지키려는 것은 「이 판이 얼마를 남기나」다.
func TestAFullyMeasuredGameQueuesNoWork(t *testing.T) {
	a, matchID := plyAnalyzer(t, nil)
	const plies = 30
	t.Cleanup(func() { a.dropJob(context.Background(), matchID) })

	for ply := 1; ply <= plies; ply++ {
		a.prefetch(matchID, startSFENOf(""), repeatMove(ply), ply)
		drainOne(t, a)
	}
	measureAhead(t, a, matchID, plies)

	a.enqueue(t.Context(), matchID, plies)
	job, err := a.store.ClaimAnalysisJob(t.Context(), time.Now().Add(-jobLease))
	if err != nil {
		t.Fatalf("판이 줄에 안 섰다: %v", err)
	}
	if job.MatchID != matchID {
		t.Fatalf("집은 판 = %q, want %q", job.MatchID, matchID)
	}
	// 미리 다 쟀으므로 이 줄에서 엔진을 부를 일이 없다.
	if job.Plies != 0 {
		t.Errorf("안 잰 手数 = %d, want 0", job.Plies)
	}

	// 그 판의 手도 남지 않는다 — 미리 잰 것은 done 이고, 안 잰 것은 enqueue 가 끊는다.
	rows, err := a.store.MeasuredAnalysisPlies(t.Context(), matchID)
	if err != nil {
		t.Fatalf("read measured: %v", err)
	}
	if len(rows) != plies {
		t.Errorf("미리 잰 手 = %d, want %d", len(rows), plies)
	}
}

// blockingAnalyst 는 판정 안에서 멈춰 서 있는다. 동시에 몇이 들어왔는지를 세는 데 쓴다.
type blockingAnalyst struct {
	entered chan struct{}
	release chan struct{}
}

func (b blockingAnalyst) Judge(context.Context, string, []string, int) (game.Judgement, error) {
	b.entered <- struct{}{}
	<-b.release
	return game.Judgement{HasEvals: true}, nil
}

// 워커 수가 지켜진다. 하나면 vCPU 를 올려도 사후 분석 층이 안 빨라진다(journal §106) —
// 포화에서도 엔진 둘 중 하나만 썼다.
//
//	SHOWGI_TEST_DATABASE_URL=postgres://showgi:showgi@localhost:5432/showgi go test ./internal/server/
func TestTheWorkerCountIsHonoured(t *testing.T) {
	_, matchA, _ := matchSeatsForAnalysis(t, "", plyList(2))
	st, matchB, _ := matchSeatsForAnalysis(t, "", plyList(2))
	// 워커를 띄우기 전에 비운다. 남의 회차가 남긴 판을 집으면 아래 셈이 흔들린다.
	clearQueues(t, st)

	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	a := newMatchAnalyzer(t.Context(), st, func() game.Analyst {
		return blockingAnalyst{entered: entered, release: release}
	}, nil, 2)
	t.Cleanup(func() { close(release) })
	t.Cleanup(func() {
		a.dropJob(context.Background(), matchA)
		a.dropJob(context.Background(), matchB)
	})

	// 판 단위 줄로 잰다. 판 하나가 워커 하나를 통째로 잡으므로 「둘이 동시에 도는가」가
	// 그 자리에서 바로 보인다 — 手 쪽은 무엇이 언제 집히는지가 더 잘게 갈린다.
	a.enqueue(t.Context(), matchA, 2)
	a.enqueue(t.Context(), matchB, 2)

	// 둘이 같이 판정 안에 있어야 한다. 워커가 하나면 두 번째가 안 온다 — 첫 번째가
	// release 를 기다리며 서 있기 때문이다.
	for i := range 2 {
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatalf("only %d worker(s) took a job, want 2", i)
		}
	}
}

// 워커가 0이면 집지 않는다. 상호작용 티어가 그 모양이고(SERVER_ROLE=interactive), 그 티어가
// 판을 집으면 티어를 가른 이유가 없어진다 — 분석이 사람의 박스에서 돈다.
//
// 세우는 쪽은 그대로 돈다. 그것까지 멈추면 분석 티어가 집을 것이 없다.
//
//	SHOWGI_TEST_DATABASE_URL=postgres://showgi:showgi@localhost:5432/showgi go test ./internal/server/
func TestNoWorkersQueuesButNeverClaims(t *testing.T) {
	st, matchID, _ := matchSeatsForAnalysis(t, "", plyList(2))
	clearQueues(t, st)

	var built atomic.Int64
	a := newMatchAnalyzer(t.Context(), st, func() game.Analyst {
		built.Add(1)
		return stubAnalyst{}
	}, nil, 0)
	t.Cleanup(func() { a.dropJob(context.Background(), matchID) })

	a.enqueue(t.Context(), matchID, 2)
	// 워커는 뜨자마자 집으므로 이 창이면 넉넉하다(plyPollInterval 은 빈 줄에서만 쓴다).
	time.Sleep(time.Second)

	// 판정기 수로 잰다. run 이 맨 위에서 한 벌 만들므로 0이면 집는 쪽이 아예 없다.
	// 「줄에 남았는가」로 재면 앞 테스트의 워커가 취소를 알아채기 전에 집어 가서 답이
	// 실행마다 달라진다.
	if n := built.Load(); n != 0 {
		t.Errorf("판정기를 %d벌 만들었다 — 집는 쪽이 안 떠야 한다", n)
	}

	// 세우는 쪽. 手가 표에 적혀야 분석 티어가 집을 것이 생긴다.
	before, err := st.CountAnalysisBacklog(t.Context())
	if err != nil {
		t.Fatalf("read the ply backlog: %v", err)
	}
	_, other, _ := matchSeatsForAnalysis(t, "", nil)
	t.Cleanup(func() { a.discard(context.Background(), other) })
	a.prefetch(other, startSFENOf(""), repeatMove(1), 1)
	waitFor(t, func() bool {
		if n, err := st.CountAnalysisBacklog(t.Context()); err == nil && n > before {
			return true
		}
		// 앞 테스트의 워커가 그 사이에 재 버렸을 수 있다(06-status.md §7). 그때도
		// 「섰다」는 참이고, 전역 셈만 보면 다시 안 참이 되어 실행마다 답이 달라진다.
		rows, err := st.MeasuredAnalysisPlies(t.Context(), other)
		return err == nil && len(rows) > 0
	}, "미리 재는 줄에 手가 서지 않았다")
}

// 자리마다 판 번호와 사람이 짝지어 나간다. 실력 추정이 그 짝으로 手를 나누므로
// (matchAnalyzer.updateSkill) 여기서 어긋나면 두 사람의 手가 한 프로파일에 쌓이고,
// 그 값도 멀쩡한 범위에 있어서 아무것도 안 잡는다.
//
// 자리를 줄에 옮겨 적지 않는다(019). games 행 둘이 곧 두 자리라 이 함수가 그 규약의
// 유일한 자리이고, 색을 코드 한 글자에서 되돌리는 것도 여기다.
func TestSeatsComeFromTheGameRows(t *testing.T) {
	st, matchID, made := matchSeatsForAnalysis(t, "", plyList(1))
	a := &matchAnalyzer{store: st}

	seats := a.seatsOf(t.Context(), matchID)
	if len(seats) != 2 {
		t.Fatalf("자리 %d개, want 2", len(seats))
	}
	// 순서가 색으로 정해진다. 기보를 첫 자리에서만 읽으므로(analyze) 여기가 흔들리면
	// 「이 판을 잴 수 있나」가 실행마다 달라진다.
	if seats[0].color != shogi.Black {
		t.Errorf("첫 자리 = %s, want 先手", seats[0].color)
	}
	want := map[shogi.Color]analysisSeat{made[0].color: made[0], made[1].color: made[1]}
	for _, got := range seats {
		if got != want[got.color] {
			t.Errorf("%s 자리 = %+v, want %+v", got.color, got, want[got.color])
		}
	}

	// 그 방이 없으면 아무것도 안 준다. 반쪽 판을 분석하면 채운 평가치가 한 사람에게만 보인다.
	if got := a.seatsOf(t.Context(), "no-such-room"); got != nil {
		t.Errorf("없는 방의 자리 = %+v, want nil", got)
	}
}
