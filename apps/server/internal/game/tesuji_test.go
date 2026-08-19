package game

import (
	"os"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/tag"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 6七桂가 5五로 뛰면 4三金·6三金에 ふんどしの桂가 걸린다. 玉은 서로 멀리 둔다 —
// LegalMoves 는 王手 회피를 따지므로 玉의 자리가 무엇을 재는지까지 바꾼다.
const (
	forkStart = "8k/9/3g1g3/9/9/9/3N5/9/8K b - 1"
	forkMove  = "6g5e"
	// 5四의 後手 歩가 5五로 뛴 桂를 딴다 — 손으로 쓴 1수 읽기가 통과시켰던 그 국면이다.
	hangingForkStart = "8k/9/3g1g3/4p4/9/9/3N5/9/8K b - 1"
)

// forkJudgement 는 「사람 관점으로 before → after」인 판정을 만든다.
//
// 저장 관점이 先手라서 여기서 한 번 옮긴다(senteCp). 그 변환을 테스트가 직접 하는
// 이유는, 게이트가 같은 변환을 반대 방향으로 하기 때문이다(cpFor) — 둘 다 틀리면
// 부호 버그가 상쇄되어 안 잡힌다.
func forkJudgement(before, after int, human shogi.Color) Judgement {
	return Judgement{
		SenteCpBefore: senteCp(before, human),
		SenteCpAfter:  senteCp(after, human),
		HasEvals:      true,
	}
}

// forkPositions 는 그 수의 앞뒤 국면을 함께 만든다. 게이트가 「이 수가 만든 것」만
// 이름 붙이므로 앞 국면이 없으면 아무것도 새것이 아니다.
func forkPositions(t *testing.T, start string, moves ...string) (before, after shogi.Position) {
	t.Helper()
	before, err := positionAfter(start, moves[:len(moves)-1])
	if err != nil {
		t.Fatalf("앞 국면: %v", err)
	}
	after, err = positionAfter(start, moves)
	if err != nil {
		t.Fatalf("뒤 국면: %v", err)
	}
	return before, after
}

func codes(tags []tag.Tag) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, t.Code)
	}
	return out
}

func has(tags []tag.Tag, code string) bool {
	for _, t := range tags {
		if t.Code == code {
			return true
		}
	}
	return false
}

// 이 게이트가 존재하는 이유다. 桂로 金 둘을 노렸지만 그 桂가 歩에 잡히는 자리였던
// 국면을 손으로 쓴 1수 읽기가 통과시켰다(docs/09-tags.md §5). 룰은 이제 형태만 보므로
// (tag.TestTheRuleLayerDoesNotAskWhetherTheForkerSurvives 가 같은 국면을 그쪽에서
// 잰다) 이름을 막는 일은 전부 여기 달려 있다.
func TestForkThatHangsIsNotNamed(t *testing.T) {
	before, after := forkPositions(t, hangingForkStart, forkMove)
	// 전제 — 룰 층은 이 형태에 이름을 붙인다. 이것이 거짓이면 아래가 다른 이유로 통과한다.
	if !has(tag.FindTesuji(after, shogi.Black), "fundoshi_no_kei") {
		t.Fatal("전제가 깨졌다: 룰 층이 ふんどしの桂를 안 낸다")
	}

	// 桂를 그냥 잃는 수라 엔진 평가치가 떨어진다. 떨어지는 폭이 이름을 막는다.
	got := namedTesuji(before, after, shogi.Black, forkMove, forkJudgement(50, -250, shogi.Black))
	if len(got) != 0 {
		t.Errorf("공짜로 잡히는 桂인데 이름이 붙었다: %v", codes(got))
	}
}

// 평가치가 안 떨어지면 이름이 붙는다 — 같은 형태, 다른 엔진 답이다.
func TestForkThatHoldsIsNamed(t *testing.T) {
	before, after := forkPositions(t, forkStart, forkMove)

	got := namedTesuji(before, after, shogi.Black, forkMove, forkJudgement(50, 40, shogi.Black))
	if !has(got, "fundoshi_no_kei") {
		t.Errorf("ふんどしの桂가 붙어야 한다: %v", codes(got))
	}
}

// 모르면 이름을 붙이지 않는다. 엔진이 없거나 판을 못 읽어 평가치가 비면, 룰만으로
// 통과시키는 것은 게이트를 없애는 것과 같다.
func TestNoEvalsMeansNoTesujiName(t *testing.T) {
	before, after := forkPositions(t, forkStart, forkMove)

	if got := namedTesuji(before, after, shogi.Black, forkMove, Judgement{}); len(got) != 0 {
		t.Errorf("평가치가 없는데 이름이 붙었다: %v", codes(got))
	}
}

// 임계치의 양쪽을 못 박는다. 값이 [미확정]이라 언젠가 움직이는데, 비교 방향이
// 뒤집히는 것은 값을 고르는 일과 다른 종류의 버그다.
func TestTesujiGateComparesAgainstTheLossLimit(t *testing.T) {
	before, after := forkPositions(t, forkStart, forkMove)

	for _, tc := range []struct {
		name string
		loss int
		want bool
	}{
		{"한계까지는 手筋이다", TesujiLossCp, true},
		{"한 걸음 넘으면 아니다", TesujiLossCp + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := namedTesuji(before, after, shogi.Black, forkMove, forkJudgement(0, -tc.loss, shogi.Black))
			if has(got, "fundoshi_no_kei") != tc.want {
				t.Errorf("낙폭 %dcp: %v (want named=%v)", tc.loss, codes(got), tc.want)
			}
		})
	}
}

// 後手로 잡은 판에서만 반대로 뜨는 버그를 막는다. 저장 관점이 先手라 게이트는 부호를
// 되돌려야 하는데, 안 되돌려도 「손해」 쪽에서만 답이 갈린다 — 이득 쪽은 부호가 상쇄되어
// 그냥 통과한다. 그래서 이 테스트는 손해 국면으로 잰다.
func TestTesujiGateFlipsForGote(t *testing.T) {
	// 위 국면의 거울상 — 5五에 打った 後手 桂가 4七金·6七金을 노린다.
	before, err := shogi.ParseSFEN("8K/9/9/9/9/9/3G1G3/9/8k w - 1")
	if err != nil {
		t.Fatalf("SFEN: %v", err)
	}
	after, err := shogi.ParseSFEN("8K/9/9/9/4n4/9/3G1G3/9/8k w - 1")
	if err != nil {
		t.Fatalf("SFEN: %v", err)
	}
	if !has(tag.FindTesuji(after, shogi.White), "fundoshi_no_kei") {
		t.Fatal("전제가 깨졌다: 거울상에서 룰이 ふんどしの桂를 안 낸다")
	}

	got := namedTesuji(before, after, shogi.White, "N*5e", forkJudgement(50, -250, shogi.White))
	if len(got) != 0 {
		t.Errorf("後手 관점으로 300cp 손해인데 이름이 붙었다: %v", codes(got))
	}
}

// 이름은 판정을 통과한 그 국면에서만 뜬다.
//
// 게이지와 같은 규약이고(state.mateGen), 여기는 이유가 하나 더 있다 — 이름을 통과시킨
// 것은 그 국면의 평가치다. 국면이 움직인 뒤에도 남겨두면 엔진에게 묻지 않은 형태에
// 이름을 붙이는 것이 되고, 형태는 그대로 서 있으므로 화면에서는 아무 이상이 안 보인다.
func TestTesujiNameDoesNotOutliveItsPosition(t *testing.T) {
	// 상대는 玉을 한 칸 옮긴다 — 両取り는 그대로 서 있다. 형태가 사라지는 수를 두면
	// 이 테스트가 세대가 아니라 기하 때문에 통과한다.
	opp := &scriptedOpponent{moves: []string{"1a1b"}, delay: 150 * time.Millisecond}
	an := &fixedAnalyst{evalBefore: 50, evalAfter: -40} // 사람 관점 +50 → +40
	s := newSession(t, Config{
		StartSFEN: forkStart, Opponent: opp, Analyst: an, HumanColor: shogi.Black,
	})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), forkMove); err != nil {
		t.Fatalf("Play: %v", err)
	}
	named := waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 1 && !s.Judging }, "판정 통과")
	if !has(named.StyleTags, "fundoshi_no_kei") {
		t.Fatalf("판정을 통과한 자리에서 手筋 이름이 안 떴다: %v", codes(named.StyleTags))
	}

	moved := waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "상대 응수")
	if has(moved.StyleTags, "fundoshi_no_kei") {
		t.Errorf("국면이 움직인 뒤에도 이름이 남았다: %v", codes(moved.StyleTags))
	}
	// 형태는 그대로라는 전제. 이것이 거짓이면 위가 기하 때문에 통과한 것이다.
	pos, err := positionAfter(forkStart, []string{forkMove, "1a1b"})
	if err != nil {
		t.Fatalf("국면: %v", err)
	}
	if !has(tag.FindTesuji(pos, shogi.Black), "fundoshi_no_kei") {
		t.Fatal("전제가 깨졌다: 상대의 응수가 両取り를 지웠다")
	}

	// 그리고 물러진 수는 이름을 만들지 않는다. 되물러도 両取り는 서 있는 국면이라,
	// 낡은 이름이 남으면 여기서 다시 뜬다.
	an.verdict = blunder()
	if _, err := s.Play(t.Context(), "1i2i"); err != nil {
		t.Fatalf("두 번째 Play: %v", err)
	}
	back := waitFor(t, ch, func(s Snapshot) bool { return s.Intervention != nil }, "개입")
	if has(back.StyleTags, "fundoshi_no_kei") {
		t.Errorf("물러진 뒤에 이름이 떴다: %v", codes(back.StyleTags))
	}
}

// TestRealEngineGatesTesujiShapes 는 게이트를 엔진에게 맡긴 것이 실제로 갈리는지 잰다.
// 룰 층은 셋 다 이름을 내고, 통과시킬지는 우리 코드가 아니라 水匠5가 읽는다.
//
// 손으로 쓴 1수 읽기를 지운 PR이라 여기가 첫 관문이다 — go test ./... 만으로는
// 이 테스트가 조용히 skip 되고 초록으로 보인다(apps/server/README.md 「테스트」 ③).
//
// 실전 국면으로 잰다. 판을 몇 개만 놓은 국면에서는 평가치를 못 쓴다 — 실제로 桂가
// 살아 있는 両取り를 만들어 재봤더니 엔진의 최선수가 그 수인데도 낙폭이 +620cp로 나왔다
// (journal §34). 그래서 마지막 하나만 인공 국면이고, 그것은 떨어지는 쪽이다.
//
// 낙폭에 붙은 값은 실측이다. 흔들림이 ±150cp쯤 되므로(§34) 한계선(100cp)에서 그만큼
// 떨어진 국면만 골랐다 — 여기가 흔들려서 깨지면 그것 자체가 알아야 할 사실이다.
//
//	SHOWGI_USI_CMD=/opt/yaneuraou/run go test ./internal/game/ -run RealEngineGates -v
func TestRealEngineGatesTesujiShapes(t *testing.T) {
	cmd := os.Getenv("SHOWGI_USI_CMD")
	if cmd == "" {
		t.Skip("SHOWGI_USI_CMD 미설정")
	}
	pool, err := usi.NewPool(1, cmd, map[string]string{
		"USI_Hash": "128", "Threads": "1", "FV_SCALE": "24",
		"BookFile": "no_book", "USI_OwnBook": "false",
	})
	if err != nil {
		t.Fatalf("엔진 풀: %v", err)
	}
	defer pool.Close()

	analyst := NewEngineAnalyst(pool, nil, intervene.Beginner)
	// 실전 기보(games.id=84)의 도중 국면. 27수째 ▲5五角 이 両取り를 걸고, 19수째
	// ▲2二角成 은 같은 이름의 형태를 만들지만 角을 던지는 수다.
	upTo := func(n int) []string { return playtestUpTo103[:n] }

	for _, tc := range []struct {
		name  string
		start string
		moves []string
		code  string
		want  bool
	}{
		{"角の両取り — 낙폭 −16~+48cp", shogi.StartSFEN, upTo(27), "kaku_ryodori", true},
		{"角を捨てて作った同じ形 — +440cp", shogi.StartSFEN, upTo(19), "kaku_ryodori", false},
		{"歩に取られる桂 — +1093cp", hangingForkStart, []string{forkMove}, "fundoshi_no_kei", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pos, err := positionAfter(tc.start, tc.moves[:len(tc.moves)-1])
			if err != nil {
				t.Fatalf("국면: %v", err)
			}
			me := pos.Turn // 판정하는 것은 늘 그 수를 둔 쪽이다

			j, err := analyst.Judge(t.Context(), tc.start, tc.moves, len(tc.moves))
			if err != nil {
				t.Fatalf("판정: %v", err)
			}
			after, err := positionAfter(tc.start, tc.moves)
			if err != nil {
				t.Fatalf("국면: %v", err)
			}

			got := namedTesuji(pos, after, me, tc.moves[len(tc.moves)-1], j)
			loss := cpFor(j.SenteCpBefore, me) - cpFor(j.SenteCpAfter, me)
			t.Logf("%s 최선수=%s 낙폭=%+dcp 이름=%v", tc.moves[len(tc.moves)-1], j.BestUSI, loss, codes(got))

			// 전제 — 룰 층은 셋 다 이름을 낸다. 갈리는 것은 엔진뿐이어야 한다.
			if !has(tag.FindTesuji(after, me), tc.code) {
				t.Fatalf("전제가 깨졌다: 룰 층이 %s 를 안 낸다", tc.code)
			}
			if has(got, tc.code) != tc.want {
				t.Errorf("이름=%v, want %s named=%v (낙폭 %+dcp)", codes(got), tc.code, tc.want, loss)
			}
		})
	}
}

// 打つ 手筋도 같은 게이트를 지난다. 이름을 정하는 사실이 판에 없을 뿐(打った 것인가)
// 이득을 정하는 쪽은 그대로다 — 오히려 이 부류는 게이트가 없으면 못 들어온다. 歩를
// 던지는 것이 내용이라 「잡히지 않는가」로 물으면 정의상 전부 탈락하기 때문이다.
func TestDropTesujiPassesThroughTheSameGate(t *testing.T) {
	// 5三의 後手 金 머리에 歩를 打つ. 持ち駒에 歩 하나를 둔다
	before, after := forkPositions(t, "4k4/9/4g4/9/9/9/9/9/4K4 b P 1", "P*5d")

	got := namedTesuji(before, after, shogi.Black, "P*5d", forkJudgement(0, -TesujiLossCp, shogi.Black))
	if !has(got, "tataki_no_fu") {
		t.Errorf("歩 한 장까지는 叩きの歩다: %v", codes(got))
	}
	// 歩 한 장을 넘게 잃으면 그냥 던진 것이다.
	got = namedTesuji(before, after, shogi.Black, "P*5d", forkJudgement(0, -400, shogi.Black))
	if len(got) != 0 {
		t.Errorf("400cp를 잃는데 이름이 붙었다: %v", codes(got))
	}
}

// 게이트가 껐던 형태가 두 수 뒤에 이름을 받으면 안 된다.
//
// 리뷰가 짚은 구멍이고, 실제로 그렇게 돌고 있었다. 게이트는 「이 수가 손해인가」에
// 답하는데 그 답을 판 위의 형태 전부에 나눠 주면, 낙폭 500cp로 만들어 한 번 꺼진
// 両取り가 그대로 서 있다가 아무 상관 없는 조용한 수에 이름을 받는다.
//
// 화면이 이름을 한 대국에 한 번만 띄우므로(useTagAnnounce) 플레이어가 보는 것은
// 그 틀린 쪽이 된다 — 늦게 온 올바른 판정이 아니라.
func TestARejectedShapeIsNotNamedByALaterQuietMove(t *testing.T) {
	// 상대는 玉만 왔다 갔다 한다 — 両取り는 계속 서 있다.
	opp := &scriptedOpponent{moves: []string{"1a1b", "1b1a"}, delay: 120 * time.Millisecond}
	an := &fixedAnalyst{evalBefore: 10, evalAfter: 490} // 사람 관점 +10 → −490. 낙폭 500cp
	s := newSession(t, Config{
		StartSFEN: forkStart, Opponent: opp, Analyst: an, HumanColor: shogi.Black,
	})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), forkMove); err != nil {
		t.Fatalf("Play: %v", err)
	}
	made := waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 1 && !s.Judging }, "판정 통과")
	if has(made.StyleTags, "fundoshi_no_kei") {
		t.Fatalf("낙폭 500cp인데 이름이 붙었다: %v", codes(made.StyleTags))
	}

	waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "상대 응수")

	// 두 번째 수는 両取り와 아무 상관 없고 낙폭도 0이다.
	an.evalAfter = -10
	if _, err := s.Play(t.Context(), "1i2i"); err != nil {
		t.Fatalf("두 번째 Play: %v", err)
	}
	quiet := waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 3 && !s.Judging }, "조용한 수")
	if has(quiet.StyleTags, "fundoshi_no_kei") {
		t.Errorf("꺼졌던 형태가 조용한 수에 이름을 받았다: %v", codes(quiet.StyleTags))
	}

	// 전제 — 그 両取り는 아직 판 위에 서 있다. 아니면 위가 다른 이유로 통과한다.
	pos, err := positionAfter(forkStart, []string{forkMove, "1a1b", "1i2i"})
	if err != nil {
		t.Fatalf("국면: %v", err)
	}
	if !has(tag.FindTesuji(pos, shogi.Black), "fundoshi_no_kei") {
		t.Fatal("전제가 깨졌다: 両取り가 이미 사라졌다")
	}
}
