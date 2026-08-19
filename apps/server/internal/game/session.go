// Package game 은 대국 세션의 상태머신이다.
//
// 상태는 세션 goroutine 하나가 소유한다. 입력은 채널로 받고 출력은 스냅샷으로 뿌린다.
// 그래서 잠금이 없고, 세션 밖에서 상태를 읽는 길도 없다 — 스냅샷 요청도 명령이다.
// 롤백이 있는 이상 상태 변경 순서가 곧 제품 정합성이라, mutex 로 나눠 잡으면
// 「물러진 수가 기보에 남는」 버그가 재현되지 않는 형태로 나온다.
package game

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/handicap"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/skill"
	"github.com/jovid18/show-gi/apps/server/internal/tag"
)

// Analyst 는 착수 한 수를 판정한다. 판정과 탐색을 갈라 두려고 인터페이스로 뒀다 —
// 세션은 「이 수가 블런더인가」만 알고 그것을 어떻게 구했는지는 모른다.
type Analyst interface {
	// Judge 는 startSFEN + moves 로 도달한 국면에서 마지막 한 수를 판정한다.
	// 탐색이 오래 걸릴 수 있어 세션 goroutine 밖에서 불린다.
	Judge(ctx context.Context, startSFEN string, moves []string, ply int) (Judgement, error)
}

// Opponent 는 상대(컴퓨터)의 수를 고른다. 구현은 둘 — NewEngineOpponent(엔진 최선수)와
// NewAdaptiveOpponent(밴드 제어, 프로덕션이 쓰는 쪽). 상대의 강함이 바뀌는 자리가
// 여기뿐이라 세션 상태머신은 둘을 구분하지 않는다.
//
// sk 는 세션이 들고 있는 추정치이고, 구조상 언제나 한 수 뒤진다 — applyVerdict 가
// 추정기에 논블로킹으로 던지고 같은 자리에서 이 함수를 부르므로 N수째 응수는 1..N-1 로
// 만든 값으로 고른다. 그래도 한 판에 서너 번은 조절된다(journal §21 ①).
type Opponent interface {
	Choose(ctx context.Context, startSFEN string, moves []string, sk skill.Estimate) (string, error)
}

// SkillAdapter 는 추정치로 강함이 실제로 달라지는 상대다. 안 만족하면 그 상대는 sk 를 버린다.
//
// Snapshot.OpponentStrength 는 추정기가 아니라 이 값을 보고 눈금을 켠다. 추정기만 보면
// sk 를 무시하는 상대(engineOpponent)에도 눈금이 그려진다 — 프로덕션은 adaptive 하나라
// 겉으로 드러나지 않는다.
type SkillAdapter interface {
	// AdaptsToSkill 은 이 상대가 추정치를 보는가다. 값이 아니라 성질이라 인자가 없다.
	AdaptsToSkill() bool
}

// adaptsToSkill 은 상대가 추정치를 보는가다. 인터페이스를 안 만족하면 안 보는 것으로
// 센다 — 새 구현이 눈금을 잠자코 얻는 것보다 잠자코 안 그리는 쪽이 안전하다.
func adaptsToSkill(o Opponent) bool {
	a, ok := o.(SkillAdapter)
	return ok && a.AdaptsToSkill()
}

// BestPlayer 는 조절을 끄고 최선수를 낼 수 있는 상대다. SkillAdapter 와 같은 자리의
// 선택 인터페이스이고, 안 만족하면 세션이 조절된 수를 그대로 쓴다.
//
// 사람이 詰み을 걸고 있는 동안만 불린다(MateChasePlies). 인자에 추정치가 없는 것이
// 곧 「조절하지 않는다」다.
type BestPlayer interface {
	ChooseBest(ctx context.Context, startSFEN string, moves []string) (string, error)
}

// chooseBest 는 조절 없이 최선수를 고른다. 상대가 그것을 못 하면 평소대로 고른다 —
// 詰み 연습이 안 되는 것이 대국이 멈추는 것보다 낫다.
func chooseBest(
	ctx context.Context, o Opponent, startSFEN string, moves []string, sk skill.Estimate,
) (string, error) {
	if b, ok := o.(BestPlayer); ok {
		return b.ChooseBest(ctx, startSFEN, moves)
	}
	return o.Choose(ctx, startSFEN, moves, sk)
}

// Rater 는 사람의 착수를 받아 실력 추정치를 돌려준다. nil이면 밴드가 기준선에 고정된다.
//
// 두 메서드의 방향이 다르다. Observe 는 세션 goroutine 이 부르므로 즉시 돌아와야 하고
// (Recorder 와 같은 규약), Estimates 는 채널이라 읽는 쪽을 세션이 소유한다 — 추정기가
// 공유 변수를 직접 쓰면 상태 소유 규약이 그 자리에서 깨진다.
//
// Recorder 와 같은 이벤트 흐름의 두 번째 소비자다(journal §21 ①).
type Rater interface {
	Observe(m skill.Move)
	Estimates() <-chan skill.Estimate
}

// Config 는 세션 하나의 설정이다.
type Config struct {
	Opponent Opponent
	// Analyst 가 nil이면 개입하지 않는다. 대국은 그대로 된다.
	Analyst Analyst
	// Mate 가 nil이면 詰み 게이지가 꺼진 채로 대국한다.
	//
	// Analyst 와 같은 solver 를 받지만 묻는 국면이 다르다. 저쪽은 방금 둔 수를 판정하려고
	// 착수 전 국면을, 이쪽은 사람 차례인 현재 국면을 묻는다 — 판정 결과를 게이지로
	// 돌려쓸 수 없다.
	Mate MateSearcher
	// TesujiHint 가 nil이면 手筋 제안형 힌트가 꺼진다. 囲い·전법 힌트는 엔진을 안 쓰므로
	// 그대로 뜬다(computeTagHints).
	//
	// Opponent 와 같은 풀을 받지만 묻는 국면이 다르다. 상대는 자기가 둘 수를 고르려고
	// 지금 국면을, 이쪽은 사람이 둘 수 있는 수를 하나씩 둬 본 뒤의 국면을 묻는다.
	TesujiHint MultiSearcher
	// Recorder 가 nil이면 기록하지 않는다. 대국은 그대로 된다.
	Recorder Recorder
	// Rater 가 nil이면 상대의 강함이 대국 내내 기준선 밴드 그대로다.
	//
	// 조절하는 것은 밴드뿐이고 개입 임계치(Level)는 안 건드린다 — 임계치가 대국 중에
	// 흔들리면 같은 국면의 같은 수가 걸리기도 안 걸리기도 한다.
	Rater Rater
	// ObservePlies 는 개입하지 않는 초반 구간이다. 기본값은 0 — 첫 수부터 판정한다.
	// 오프닝의 다양성은 수 번호가 아니라 임계치가 지킨다(01-core.md §2).
	ObservePlies int
	// HumanColor 는 사람이 잡는 쪽. 기본은 先手(Black).
	HumanColor shogi.Color
	// StartSFEN 이 비면 평수 초기 국면.
	StartSFEN string
	// StartMoves 는 StartSFEN 에서 이미 둬진 수순이다. 이어하기가 기록에서 국면을 다시
	// 세울 때만 채운다(server/ws.go, journal §51). 판을 살려 두는 대신 기보로 다시 두므로
	// 세션 수명은 그대로 연결에 매여 있다(§46).
	//
	// 여기 오는 것은 기보라 물러진 수가 없다(docs/01-core.md §5).
	StartMoves []string
	// MoveDeadline·ExtraDeadline 은 대국 중 엔진 탐색에 거는 시한이다. 0이면 기본값이다.
	// 시한이 하는 일은 결과를 버리는 것뿐이고, 넘겼다고 얕게 다시 묻지 않는다 — 그러면
	// 상대의 강함이 서버 사정에 따라 달라진다(01-core.md §4).
	MoveDeadline  time.Duration
	ExtraDeadline time.Duration
	// HintSearch 가 nil이면 부르는 힌트가 꺼진다(CanHint 가 언제나 false).
	//
	// Opponent 와 같은 풀을 받지만 관점이 반대다. 상대는 자기가 둘 수를, 이쪽은 사람이
	// 둘 최선수를 묻는다 — 같은 국면이어도 결과를 돌려쓸 수 없다.
	//
	// k=1로 묻는다(askHint). 판정이 이미 만드는 캐시 행과 모양이 같아야 positions 가
	// 한 행으로 합쳐진다 — 같은 국면·같은 깊이·같은 k.
	HintSearch MultiSearcher
	// HintsUsed·HintStages 는 이 판에서 이미 쓴 힌트다. 이어하는 판만 채운다(server/ws.go).
	// 앞은 예산, 뒤는 「이 국면은 어디까지 봤나」다 — 뒤가 없으면 이어한 판에서 같은
	// 국면의 답을 다시 볼 수 있다(store.HintUse).
	HintsUsed  int
	HintStages map[string]int

	// UndoUsed 는 이 판에서 이미 무른 횟수다. 이어하는 판만 채운다(server/ws.go).
	//
	// 예산이 세션이 아니라 판에 붙어서 여기로 받는다. 세션은 이어할 때마다 새로 서므로
	// 카운터가 그때 0이 되면 UndoMaxPerGame 이 「연결당 3회」가 된다 — 새로고침 한 번에
	// 예산이 다시 찬다.
	UndoUsed int
	// OpponentOpening 은 상대가 따르는 진형의 일본어 이름이다. 스냅샷으로 그대로 나간다.
	// 수순은 상대(book_opponent.go)가 들고 세션은 모른다 — 여기에 수순이 들어오면
	// 판정과 개입이 상대의 계획을 참조할 수 있게 된다.
	OpponentOpening string
}

// ErrClosed 는 끝난 세션에 명령을 보냈을 때 나온다.
var ErrClosed = errors.New("game: session closed")

// ErrNotYourTurn 은 사람 차례가 아닐 때 착수를 보냈을 때 나온다.
var ErrNotYourTurn = errors.New("game: not your turn")

// ErrFinished 는 이미 끝난 대국에 착수를 보냈을 때 나온다.
var ErrFinished = errors.New("game: game already finished")

// ErrNoUndoLeft 는 그 판의 무르기 예산을 다 썼을 때다(UndoMaxPerGame).
var ErrNoUndoLeft = errors.New("game: no undo left")

// ErrNothingToUndo 는 되돌릴 사람의 수가 아직 없을 때다 — 첫 수 앞이거나, 이 판에서
// 사람이 한 수도 확정하지 않았다.
var ErrNothingToUndo = errors.New("game: nothing to undo")

// UndoMaxPerGame 은 사람이 스스로 무를 수 있는 횟수다.
//
// 개입의 되무르기와 예산이 다르다. 저쪽은 판정이 정하므로 상한이 없고, 이쪽은 사람이
// 정하므로 상한이 곧 기능이다 — 무제한이면 「블런더를 두면 물러진다」가 「아무 때나
// 되돌린다」에 묻힌다(journal §72).
//
// [미확정] 3은 사람이 요청한 값이다. 실측으로 잡은 것이 아니다.
const UndoMaxPerGame = 3

// ErrNoHintLeft 는 그 판의 힌트 예산을 다 썼을 때다(HintMaxPerGame).
var ErrNoHintLeft = errors.New("game: no hint left")

// ErrHintSeen 은 이 국면에서 이미 답까지 봤을 때다. 같은 자리를 세 번째로 물으면
// 여기로 온다 — 더 줄 것이 없다(HintStageMax).
var ErrHintSeen = errors.New("game: hint already given for this position")

// ErrNoHint 는 힌트를 만들 수 없을 때다 — 엔진이 없거나 탐색이 수를 못 냈다.
var ErrNoHint = errors.New("game: no hint available")

// HintMaxPerGame 은 사람이 불러서 받을 수 있는 최선수 힌트의 횟수다.
//
// 갇힘 힌트가 「최선수를 보여주지 않는다」(01-core.md §1)의 예외인 근거는 다섯 번
// 실패해야 열려서 기댈 수 없다는 것이었다. 부르는 힌트는 기댈 수 있으므로 그 자리를
// 예산이 대신 맡는다 — 한 국면의 답을 통째로 보려면 두 번을 쓰므로 한 판에 최대
// 세 수까지다.
//
// [미확정] 6은 사람이 고른 값이다. 근거는 journal §78.
const HintMaxPerGame = 6

// HintStageMax 는 한 국면에서 열리는 마지막 단계다. 1이 「어느 駒인가」, 2가 「어떻게
// 움직이나」이고 3은 없다 — 갇힘 힌트와 같은 그림을 그린다(buildHint).
const HintStageMax = 2

// 대국 중 엔진 탐색에 거는 시한이다. 자를 시간이 아니라 버릴 시점이다 — 깊이 기반이라
// 중간 결과는 depth N 결과가 아니고, 넘기면 통째로 버린다(usi.Engine.SearchDepth).
//
// 값이 둘인 것은 무엇을 먼저 포기하는가다. 넷이 같은 풀을 다투므로(cmd/api/main.go)
// 부가 기능이 오래 붙들면 대국이 굶는다. 숫자의 근거는 journal §56.
const (
	// DefaultMoveDeadline 은 판이 그 자리에서 멈추는 두 경로의 시한이다 — 상대 수와 개입 판정.
	DefaultMoveDeadline = 60 * time.Second

	// DefaultExtraDeadline 은 없어도 판이 도는 두 경로의 시한이다 — 詰み 게이지와 手筋 힌트.
	// 이 둘은 조용히 없어지고, 사람이 알아채는 쪽은 언제나 위 둘이다.
	DefaultExtraDeadline = 20 * time.Second
)

type cmdKind int

const (
	cmdMove cmdKind = iota
	cmdResign
	cmdUndo
	cmdHint
	cmdSnapshot
	cmdSubscribe
	cmdUnsubscribe
)

type command struct {
	kind  cmdKind
	usi   string
	sub   chan Snapshot
	reply chan result
}

type result struct {
	snap Snapshot
	err  error
}

type engineResult struct {
	usi string
	err error
}

type mateResult struct {
	gen   int
	plies int // 詰み 手数. 0이면 못 찾았다
	err   error
}

// tesujiHintResult 는 게이트를 통과한 手筋 후보다. opts 는 계단이 짚을 수를 들고 있고,
// 화면에 나가는 것은 이름뿐이다(tesujiHintTags).
type tesujiHintResult struct {
	gen     int
	opts    []TesujiOption
	dropped int // 상한에 걸려 못 물어본 후보 수
	err     error
}

type judgeResult struct {
	gen       int
	judgement Judgement
	move      Move
	err       error
}

// Session 은 대국 하나다. 모든 메서드는 안전하게 동시 호출할 수 있다 —
// 실제로 하는 일은 세션 goroutine에 명령을 보내고 답을 기다리는 것뿐이다.
type Session struct {
	cmds      chan command
	done      chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
}

// 내부 상태. 세션 goroutine만 만진다.
type state struct {
	cfg     Config
	pos     shogi.Position
	start   string
	moves   []Move
	usis    []string
	prevTo  int
	repeats map[string]int
	status  Status
	winner  Side

	thinking bool
	// searchGen 은 국면이 바뀔 때마다 오른다. 탐색을 띄울 때의 값을 pendingGen 에
	// 적어두고, 결과가 돌아왔을 때 둘이 다르면 버린다 — 그 사이에 국면이 움직였다는 뜻이다.
	// D3에서 롤백이 들어오면 이게 실제로 값을 한다.
	searchGen  int
	pendingGen int

	// 판정도 세션 goroutine 밖에서 돈다(탐색과 같은 이유). 늦게 온 결과는 세대로 버린다.
	judging      bool
	judgeGen     int
	intervention *Intervention

	// stuck 은 같은 국면에서 연속으로 물러진 횟수다. 통과하는 수를 두면 0으로 돌아가고,
	// 되무르기는 국면을 그대로 되돌리므로 「연속」이 곧 「같은 국면」이 된다.
	stuck int
	// hint 는 그 횟수에 열린 안내다. intervention 이 「방금 무엇을 했나」라면
	// 이쪽은 「지금 무엇을 할까」다. 수명은 같다.
	hint *Hint
	// notice 는 우리가 못 해준 것이다. 위 둘과 수명이 같고 뜻이 또 다르다(Notice).
	notice *Notice

	// 詰み 게이지도 세션 goroutine 밖에서 돈다(탐색·판정과 같은 이유).
	//
	// mateHeat 와 mateGen 을 함께 본다. 국면이 움직이면 그 세기는 무효이고, 지우는 대신
	// 스냅샷이 둘을 대조한다 — 지우는 코드를 착수·롤백·종료에 흩어 두면 하나를
	// 빠뜨렸을 때 낡은 불꽃이 새 국면에 남는다.
	gauging  bool
	gaugeGen int
	mateHeat int
	mateGen  int
	// matePlies 는 게이지가 자르기 전의 詰み 手数다. 화면에는 안 나가고(mateHeat 이 그 일을
	// 한다) 상대의 태도를 정하는 데만 쓴다 — MateChasePlies.
	matePlies int
	// chasing 은 사람이 詰み을 걸고 있는 채로 이 수를 뒀는가다. 상대가 밴드를 안 보고
	// 최선으로 버티는 조건이고(MateChasePlies), 사람이 둘 때마다 다시 정해진다.
	//
	// 착수 전에 정해야 한다. 수를 놓은 뒤의 게이지는 그 국면의 것이 아니라, 그때 물으면
	// 「지금 걸고 있는가」가 된다 — 그쪽은 상대 차례라 solver 가 답하지 않는다(maybeGauge).
	chasing bool

	// tesuji 는 엔진 게이트를 통과한 手筋 이름들이고, tesujiGen 은 그것을 구한 국면이다.
	// 게이지와 같은 규약으로 스냅샷이 둘을 대조한다. 이름을 통과시킨 것이 그 국면의
	// 평가치라, 다음 국면까지 들고 가면 묻지도 않은 형태에 이름을 붙이게 된다.
	tesuji    []tag.Tag
	tesujiGen int

	// 부르는 힌트. 예산은 판에 붙고(hints) 단계는 국면마다다(hintStages).
	// 탐색은 세션 goroutine 밖에서 돌고, 게이지와 같은 규약으로 세대(hintGen)로 거른다.
	hints      int
	hintStages map[string]int
	hinting    bool
	hintGen    int
	// hintedKey·hintedUSI 는 지금 국면에서 답을 알려줬는가다. 사람이 그 수를 실제로 뒀는지를
	// 다음 착수에서 채우고(Recorder.HintTaken), 그 수를 실력 추정에서 뺀다 — 알려준 답이
	// 실력으로 기록되면 段級이 부푼다.
	hintedKey string
	hintedUSI string
	// skipRating 은 방금 둔 수를 실력 추정에서 뺄 것인가다. 답을 본 국면에서 착수할 때
	// 켜지고 판정이 끝나면 꺼진다 — 판정이 비동기라 착수 시점의 사실을 그때까지 들고 있는다.
	skipRating bool
	// hintReported 는 이 힌트에 대해 HintTaken 을 이미 적었는가다. 답을 본 국면에서
	// 여러 번 시도할 수 있어서(개입 롤백) 필요하다.
	hintReported bool

	// namedStyle 은 이 판에서 이미 기록한 囲い·전법·戦型 코드다(recordStyleTags).
	// 이어하는 판은 이 기억을 잃으므로(§51) 거르는 자리가 질의에도 하나 더 있다
	// (store.AddStyleTag).
	namedStyle map[string]bool

	// 제안형 힌트. 빈도 상한과 쿨다운을 여기서 잡는다(01-core.md §7.1).
	tagHints       []tag.Tag
	tagHintGen     int
	tagHintCount   int
	tagHintLastPly int

	// 手筋 쪽 제안형 힌트. 예산(카운터)만 따로 센다 — 한 예산이면 囲い가 먼저 다 써서
	// 手筋이 못 뜬다(journal §42). 이름이 아니라 후보를 들고 있는 것은 계단 ②③이 짚을
	// 수가 여기 있어서다. 이름은 언제든 후보에서 편다(tesujiHintTags).
	tesujiOpts        []TesujiOption
	tesujiHintGen     int
	tesujiHinting     bool
	tesujiHintCount   int
	tesujiHintLastPly int
	// tesujiHintAsked 는 한 번이라도 물어봤는가다. tesujiHintLastPly 의 0을 「아직 없다」로
	// 겸하면 0手目의 물음이 쿨다운을 안 걸고, 그 자리가 先手의 첫 차례다.
	tesujiHintAsked bool

	// skill 은 추정기가 마지막으로 올려보낸 값이다. 상대를 고를 때만 쓴다.
	// 세션 goroutine 만 읽고 쓴다 — 추정기는 채널로 올려보낼 뿐이다(Rater).
	skill skill.Estimate

	// 물러질 수 있으므로 착수 직전 국면을 들고 있는다. Position 이 값 타입이라 복사면 끝이다.
	prevPos    shogi.Position
	prevPrevTo int

	// undos 는 사람이 스스로 무른 횟수다. 개입의 되무르기는 안 센다 — 예산이 다르고
	// (UndoMaxPerGame), 개입을 세면 AI가 막을수록 사람의 무르기가 줄어든다.
	// 이어하는 판은 0이 아니라 기록에 남은 값에서 시작한다(Config.UndoUsed).
	undos int

	subs map[chan Snapshot]struct{}
}

// New 는 세션을 시작한다. ctx가 끝나면 세션도 끝난다.
func New(ctx context.Context, cfg Config) (*Session, error) {
	sfen := cfg.StartSFEN
	if sfen == "" {
		sfen = shogi.StartSFEN
	}
	pos, err := shogi.ParseSFEN(sfen)
	if err != nil {
		return nil, err
	}
	if cfg.Opponent == nil {
		return nil, errors.New("game: Opponent is required")
	}

	s := &Session{
		cmds:   make(chan command),
		done:   make(chan struct{}),
		closed: make(chan struct{}),
	}
	st := &state{
		cfg:        cfg,
		pos:        pos,
		start:      sfen,
		prevTo:     -1,
		repeats:    map[string]int{},
		status:     StatusPlaying,
		subs:       map[chan Snapshot]struct{}{},
		skill:      skill.Unknown,
		undos:      cfg.UndoUsed,
		hints:      cfg.HintsUsed,
		hintStages: map[string]int{},
	}
	st.repeats[pos.RepetitionKey()]++
	// 넘겨받은 표를 그대로 들지 않는다. 부르는 쪽이 계속 들고 있으면 세션 goroutine 밖에서
	// 그 표가 바뀌고, 그 순간 상태 소유 규약이 깨진다.
	for k, v := range cfg.HintStages {
		st.hintStages[k] = v
	}

	// run 전에 되만든다. 여기까지는 goroutine 이 하나뿐이고 Recorder 도 아직 아무 말을
	// 안 들었으므로, 되만들기가 실패하면 세션이 아예 안 선다 — 반쯤 선 판을 접는 길을
	// 만들지 않는다.
	if err := st.replay(cfg.StartMoves); err != nil {
		return nil, err
	}

	go s.run(ctx, st)
	return s, nil
}

// ErrCannotResume 은 기록에서 판을 다시 세울 수 없을 때다. 부르는 쪽에서 404가 된다.
var ErrCannotResume = errors.New("game: cannot rebuild the position from the record")

// replay 는 기보를 그대로 다시 둬서 끊긴 자리로 판을 되돌린다.
//
// 한 수라도 안 맞으면 통째로 거절한다. 눈감고 이어 두면 한 칸 어긋난 판이 「그때 두던
// 판」의 얼굴로 서고, 그 뒤로 서버도 화면도 조용하다 — 사람만 자기 持ち駒가 다른 것을 본다.
func (st *state) replay(moves []string) error {
	for i, u := range moves {
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			return fmt.Errorf("%w: ply %d (%s): %w", ErrCannotResume, i+1, u, err)
		}
		if err := st.pos.ValidateMove(m); err != nil {
			return fmt.Errorf("%w: ply %d (%s): %w", ErrCannotResume, i+1, u, err)
		}
		// 둔 쪽은 착수 전 수번이다. advance 가 판을 넘긴 뒤에 물으면 상대의 수가 된다.
		by := st.sideOf(st.pos.Turn)
		st.advance(m, by)
	}
	// 되만든 판이 이미 끝나 있으면 이어할 것이 없다. abandoned 로 닫힌 판이라 여기
	// 걸릴 일은 없고, 걸린다면 기록이 그 자리에서 끊긴 것이다.
	if len(moves) > 0 && (st.pos.NoLegalMoves() || st.repeats[st.pos.RepetitionKey()] >= 4) {
		return fmt.Errorf("%w: the game is already over at ply %d", ErrCannotResume, len(moves))
	}
	return nil
}

// moveDeadline·extraDeadline 은 설정된 시한이거나 기본값이다.
func (st *state) moveDeadline() time.Duration {
	if st.cfg.MoveDeadline > 0 {
		return st.cfg.MoveDeadline
	}
	return DefaultMoveDeadline
}

func (st *state) extraDeadline() time.Duration {
	if st.cfg.ExtraDeadline > 0 {
		return st.cfg.ExtraDeadline
	}
	return DefaultExtraDeadline
}

func (s *Session) run(ctx context.Context, st *state) {
	defer close(s.closed)

	engineDone := make(chan engineResult, 1)
	judgeDone := make(chan judgeResult, 1)
	gaugeDone := make(chan mateResult, 1)
	hintDone := make(chan hintResult, 1)
	tesujiDone := make(chan tesujiHintResult, 1)

	// 기록도 세션 goroutine 안에서 시작한다 — 상태를 만지는 순서와 같은 줄에 둔다.
	if st.cfg.Recorder != nil {
		st.cfg.Recorder.Started(st.start, st.cfg.HumanColor)
	}

	// 추정기가 없으면 nil 채널이라 그 case 가 영원히 안 고른다 — 아래 루프에 조건문을
	// 두지 않기 위해서다.
	var rated <-chan skill.Estimate
	if st.cfg.Rater != nil {
		rated = st.cfg.Rater.Estimates()
	}

	// 엔진이 선수면 시작하자마자 생각한다. 사람이 선수면 게이지가 대신 걸린다 —
	// 둘은 정확히 반대 조건이라 언제나 하나만 돈다.
	st.computeTagHints()
	st.maybeTesujiHint(ctx, tesujiDone)
	st.maybeThink(ctx, engineDone)
	st.maybeGauge(ctx, gaugeDone)

	for {
		select {
		case <-ctx.Done():
			st.closeSubs()
			return

		case <-s.done:
			st.closeSubs()
			return

		case c := <-s.cmds:
			st.handle(ctx, c, engineDone, judgeDone, gaugeDone, tesujiDone, hintDone)

		case r := <-engineDone:
			st.applyEngineMove(ctx, r, engineDone, gaugeDone, tesujiDone)

		case r := <-judgeDone:
			st.applyVerdict(ctx, r, engineDone, gaugeDone, tesujiDone)

		case r := <-hintDone:
			st.applyHintResult(r)

		case r := <-gaugeDone:
			st.applyMateHeat(r)

		case r := <-tesujiDone:
			st.applyTesujiHint(r)

		case e := <-rated:
			st.applySkill(e)
		}
	}
}

func (st *state) handle(ctx context.Context, c command, engineDone chan engineResult, judgeDone chan judgeResult, gaugeDone chan mateResult, tesujiDone chan tesujiHintResult, hintDone chan hintResult) {
	switch c.kind {
	case cmdSnapshot:
		c.reply <- result{snap: st.snapshot()}

	case cmdSubscribe:
		st.subs[c.sub] = struct{}{}
		notify(c.sub, st.snapshot())
		c.reply <- result{snap: st.snapshot()}

	case cmdUnsubscribe:
		if _, ok := st.subs[c.sub]; ok {
			delete(st.subs, c.sub)
			close(c.sub)
		}
		c.reply <- result{}

	case cmdResign:
		if st.status != StatusPlaying {
			c.reply <- result{snap: st.snapshot(), err: ErrFinished}
			return
		}
		st.finish(StatusResigned, SideEngine)
		c.reply <- result{snap: st.snapshot()}
		st.broadcast()

	case cmdHint:
		// 성공해도 여기서는 아직 아무것도 안 뜬다. 탐색이 끝나야 단계가 정해지고,
		// 그때 스냅샷이 구독 채널로 간다(applyHintResult).
		err := st.askHint(ctx, hintDone)
		c.reply <- result{snap: st.snapshot(), err: err}
		if err == nil {
			st.broadcast()
		}

	case cmdUndo:
		snap, err := st.undo(ctx, gaugeDone, tesujiDone)
		c.reply <- result{snap: snap, err: err}
		if err == nil {
			st.broadcast()
		}

	case cmdMove:
		snap, err := st.playHuman(ctx, c.usi, engineDone, judgeDone, gaugeDone)
		c.reply <- result{snap: snap, err: err}
		if err == nil {
			st.broadcast()
		}
	}
}

// undo 는 사람이 스스로 직전 자기 수를 무른다(待った).
//
// 개입의 롤백과 세 가지가 다르다. 시작하는 쪽이 사람이고, 예산이 있고(UndoMaxPerGame),
// 되돌리는 폭이 두 手다 — 사람의 수 하나를 되돌리려면 이미 확정된 상대의 응수도 같이
// 사라져야 판이 사람 차례로 온다. 롤백은 판정이 상대 수보다 먼저 돌아(playHuman)
// 되돌릴 것이 언제나 하나뿐이고, 그래서 prevPos 한 장으로 끝난다.
//
// 평가는 되돌리지 않는다. 무른 수는 판정을 이미 통과했고 그때 추정기가 그 값을 먹었다
// (applyVerdict 의 observeSkill). 여기서 빼면 「어려운 수를 두고 무르면 실력이 안
// 떨어진다」가 되어 상대가 실제보다 약해진 채로 남는다(journal §72).
//
// engineDone 을 안 받는다. 되감은 국면은 사람 차례라 상대를 생각시킬 일이 없고,
// 인자로 들고 있으면 언젠가 여기서 maybeThink 를 부르게 된다.
func (st *state) undo(ctx context.Context, gaugeDone chan mateResult, tesujiDone chan tesujiHintResult) (Snapshot, error) {
	if st.status != StatusPlaying {
		return st.snapshot(), ErrFinished
	}
	// 판정 중이거나 상대가 생각 중이면 국면이 아직 사람에게 안 돌아왔다. 그 사이에
	// 되감으면 날아오는 결과가 되감기 전 국면의 것이고, 세대로 버려지긴 하지만 사람
	// 눈에는 「눌렀는데 한 수 뒤에 무너졌다」로 보인다.
	if st.judging || st.thinking || st.pos.Turn != st.cfg.HumanColor {
		return st.snapshot(), ErrNotYourTurn
	}
	if st.undos >= UndoMaxPerGame {
		return st.snapshot(), ErrNoUndoLeft
	}
	at := st.lastHumanMove()
	if at < 0 {
		return st.snapshot(), ErrNothingToUndo
	}

	undone := st.moves[at]
	if err := st.rewindTo(at); err != nil {
		// 되감기가 실패하면 판을 안 건드린 채로 거절한다. 반쯤 되감긴 판을 내보내면
		// 그 뒤의 모든 판정이 없던 국면 위에서 돈다(replay 와 같은 판단).
		log.Printf("game: cannot rewind to ply %d: %v", at, err)
		return st.snapshot(), err
	}
	st.undos++

	// 기보에서 지우는 것도 기록 쪽이 한다. 무른 수와 그 뒤 상대의 응수 둘 다이고,
	// 手数를 넘기면 store 가 거기서부터 자른다(store.RecordUndo).
	if st.cfg.Recorder != nil {
		st.cfg.Recorder.Undone(at+1, undone.USI)
	}

	// 개입 카드·힌트·알림은 전부 직전 수에 대한 말이라 그 수가 사라지면 같이 사라진다.
	st.intervention, st.hint, st.notice = nil, nil, nil
	// 갇힘도 푼다. 「같은 국면에서 연속으로 물러졌다」를 세는 값인데(state.stuck), 사람이
	// 스스로 되감은 것은 그 연속이 아니다 — 남겨 두면 다음 한 번에 계단이 열린다.
	st.stuck = 0

	// 되돌아온 국면은 手筋 힌트를 물어봤던 바로 그 국면이다 — rollback 과 같은 근거다.
	if !st.tesujiHinting && st.tesujiHintAsked && st.tesujiHintLastPly == len(st.usis) {
		st.tesujiHintGen = st.searchGen
	}

	st.computeTagHints()
	st.maybeTesujiHint(ctx, tesujiDone)
	st.maybeGauge(ctx, gaugeDone)
	return st.snapshot(), nil
}

// lastHumanMove 는 확정된 기보에서 마지막 사람 수의 자리다. 없으면 -1.
// st.moves 에는 물러진 수가 없으므로(rollback 이 자른다) 판에 남아 있는 수만 나온다.
func (st *state) lastHumanMove() int {
	for i := len(st.moves) - 1; i >= 0; i-- {
		if st.moves[i].By == SideHuman {
			return i
		}
	}
	return -1
}

// rewindTo 는 n手까지 둔 국면으로 되감는다. 그 뒤의 수는 기보에서 사라진다.
//
// 처음부터 다시 둔다. 되돌릴 것이 판 하나가 아니라 千日手 계수·「同」이 보는 도착 칸·
// 표기까지라, 손으로 되감는 코드는 하나를 빠뜨렸을 때 조용하다. 판당 세 번뿐이라
// (UndoMaxPerGame) 다시 두는 비용은 문제가 되지 않는다.
func (st *state) rewindTo(n int) error {
	keep := append([]string(nil), st.usis[:n]...)

	pos, err := shogi.ParseSFEN(st.start)
	if err != nil {
		return err
	}
	st.pos, st.prevTo = pos, -1
	st.moves, st.usis = nil, nil
	st.repeats = map[string]int{}
	st.repeats[pos.RepetitionKey()]++
	// 세대를 여기서 올린다. replay 안의 advance 도 매 수 올리지만, 되감을 것이 0手면
	// 한 번도 안 올라 늦게 온 결과가 살아남는다.
	st.searchGen++

	return st.replay(keep)
}

func (st *state) playHuman(ctx context.Context, usi string, engineDone chan engineResult, judgeDone chan judgeResult, gaugeDone chan mateResult) (Snapshot, error) {
	if st.status != StatusPlaying {
		return st.snapshot(), ErrFinished
	}
	if st.judging {
		return st.snapshot(), ErrNotYourTurn // 판정 중에는 다음 수를 못 둔다
	}
	if st.pos.Turn != st.cfg.HumanColor {
		return st.snapshot(), ErrNotYourTurn
	}
	m, err := shogi.ParseUSIMove(usi)
	if err != nil {
		return st.snapshot(), err
	}
	if err := st.pos.ValidateMove(m); err != nil {
		return st.snapshot(), err
	}

	// 새 수를 두면 직전 개입은 지운다 — 화면에 남아 있으면 방금 둔 수를 가리키는 것처럼 보인다.
	// 힌트도 같이 내린다. 또 물러지면 한 단계 올라간 것이 새로 뜬다.
	// 알림도 같다 — 「앞 수를 못 확인했다」가 남아 있으면 이번 수를 가리키는 말이 된다.
	st.intervention, st.hint, st.notice = nil, nil, nil

	// 물러질 수 있으니 착수 전 국면을 들고 있는다.
	st.prevPos, st.prevPrevTo = st.pos, st.prevTo

	// 이 질문을 할 수 있는 마지막 자리다(state.chasing). 게이지가 아직 안 돌아왔으면
	// (mateGen != searchGen) 모르는 것이고, 모르면 조절을 그대로 둔다.
	st.chasing = st.mateGen == st.searchGen && st.matePlies > 0 && st.matePlies <= MateChasePlies

	// 답을 본 국면에서 두는가. 여기서 둘이 정해진다 — 알려준 수를 실제로 뒀는지를
	// 기록하고(01-core.md §5의 taken), 그 수를 실력 추정에서 뺀다.
	//
	// 착수 전에 봐야 한다. 아래 apply 가 국면을 옮기면 이 국면의 키가 사라진다.
	// 국면이 열쇠라 따로 지울 필요는 없다 — 다른 국면으로 가면 비교가 안 맞고, 되물러
	// 돌아오면 다시 맞는다(그때도 레이팅에서 빠져야 하는 자리다).
	st.skipRating = st.hintedKey != "" && st.hintedKey == shogi.PositionKey(st.pos)
	if st.skipRating && !st.hintReported {
		// 첫 시도만 적는다. 그 뒤의 시도는 물러진 수라 기보에 안 남고, taken 은
		// 「답을 쥔 채 무엇을 뒀나」이지 「몇 번 시도했나」가 아니다.
		st.hintReported = true
		if st.cfg.Recorder != nil {
			st.cfg.Recorder.HintTaken(st.hintedKey, usi == st.hintedUSI)
		}
	}

	st.apply(m, SideHuman)

	// 판정할 것이 있으면 엔진을 부르기 전에 먼저 묻는다. 상대 수를 먼저 두면 롤백이
	// 되돌릴 것이 둘이 된다.
	if st.startJudging(ctx, judgeDone) {
		return st.snapshot(), nil
	}
	// 판정하지 않으면 그 자리에서 확정이다.
	st.recordLastMove()
	st.maybeThink(ctx, engineDone)
	st.maybeGauge(ctx, gaugeDone)
	return st.snapshot(), nil
}

// startJudging 은 방금 둔 사람의 수를 판정시킨다. 판정에 들어갔으면 true.
//
// 탐색과 같은 이유로 세션 goroutine 밖에서 돈다 — 여기서 기다리면 판정하는 동안
// 투료도 스냅샷도 못 받는다.
func (st *state) startJudging(ctx context.Context, judgeDone chan judgeResult) bool {
	if st.cfg.Analyst == nil || st.status != StatusPlaying {
		return false
	}
	if len(st.usis) <= st.cfg.ObservePlies {
		return false
	}
	st.judging = true
	st.judgeGen = st.searchGen

	gen := st.judgeGen
	analyst := st.cfg.Analyst
	start := st.start
	moves := append([]string(nil), st.usis...)
	played := st.moves[len(st.moves)-1]
	ply := len(st.usis)
	deadline := st.moveDeadline()

	go func() {
		jctx, cancel := context.WithTimeout(ctx, deadline)
		j, err := analyst.Judge(jctx, start, moves, ply)
		cancel()

		// 문장은 여기서 안 만든다. explain.Render 는 사실만 보는 순수 함수라 되무르는
		// 자리에서 불러도 판정 결과와 문장이 갈릴 길이 없다.
		select {
		case judgeDone <- judgeResult{gen: gen, judgement: j, move: played, err: err}:
		case <-ctx.Done():
		}
	}()
	return true
}

// applyVerdict 는 판정 결과를 반영한다. 걸렸으면 되무른다.
func (st *state) applyVerdict(ctx context.Context, r judgeResult, engineDone chan engineResult, gaugeDone chan mateResult, tesujiDone chan tesujiHintResult) {
	if !st.judging || r.gen != st.judgeGen {
		return // 그 사이 국면이 움직였다. 버린다
	}
	st.judging = false

	if r.err != nil {
		// 판정이 실패했다고 대국을 멈추지 않는다. 개입은 부가 기능이고 대국이 본체다.
		// 다만 조용히 넘기지도 않는다 — 개입이 없는 화면은 「이 수는 괜찮았다」와 똑같이
		// 생겼는데 여기서는 확인 자체를 못 한 것이다(Notice).
		log.Printf("game: judging failed, letting the move stand: %v", r.err)
		st.notice = newNotice(NoticeJudgeSkipped)
	}

	// 판정이 성공한 수는 걸렸든 통과했든 실력 신호다. 물러진 수만 세면 표본이 개입에
	// 오염되고, 통과한 수만 세면 제일 큰 실수가 안 들어온다(01-core.md §5).
	if r.err == nil {
		st.observeSkill(r.judgement)
	}

	if r.err == nil && r.judgement.Verdict.Kind == intervene.KindBlunder {
		st.rollback(r)
		// 되물러 사람 차례로 돌아왔다. 힌트와 게이지도 그 국면의 것으로 다시 구한다 —
		// 롤백이 searchGen 을 올리므로 물러지기 전의 것은 이미 무효다.
		st.computeTagHints()
		st.maybeTesujiHint(ctx, tesujiDone)
		st.maybeGauge(ctx, gaugeDone)
		st.broadcast()
		return
	}

	// 판정을 통과했다. 여기가 사람의 수가 확정되는 자리다. 갇힘도 여기서 풀린다.
	st.stuck = 0

	// 手筋의 이름이 여기서 정해진다. 판정이 들고 온 평가치가 「이득인가」에 답하고
	// (tesuji.go), 그 답은 이 국면에서만 유효하므로 세대를 함께 적는다.
	//
	// 앞 국면(prevPos)을 함께 넘기는 것은 이 수가 만든 형태에만 그 답을 주기 위해서다.
	// 물러진 쪽에서는 이 줄에 오지 않는다 — 두지 않은 것으로 된 수가 판의 이름을 정하면
	// 안 된다(movesBy 와 같은 이유).
	st.tesuji = namedTesuji(st.prevPos, st.pos, st.cfg.HumanColor, r.move.USI, r.judgement)
	st.tesujiGen = st.searchGen

	st.recordLastMove()
	st.recordEvals(r.judgement)
	st.maybeThink(ctx, engineDone)
	st.maybeGauge(ctx, gaugeDone)
	st.broadcast()
}

// observeSkill 은 판정 결과를 추정기에 넘긴다. 기다리지 않는다(Rater).
//
// 답을 본 수는 안 넘긴다 — 알려준 최선수를 그대로 둔 것이 실력으로 기록되면 段級이
// 부푼다(journal §62 · §78). 1단계는 그대로 넘긴다. 駒만 짚었으므로 어디로 갈지는
// 여전히 사람이 찾았고, 그때는 hintedUSI 가 비어 있다.
func (st *state) observeSkill(j Judgement) {
	if st.cfg.Rater == nil || st.skipRating {
		return
	}
	st.cfg.Rater.Observe(skill.Move{
		Blunder:   j.Verdict.Kind == intervene.KindBlunder,
		DeltaWin:  j.Verdict.DeltaWin,
		Threshold: j.Threshold,
	})
}

// applySkill 은 올라온 추정치를 갈아 끼운다.
//
// 국면 세대(searchGen)를 안 본다. 게이지·手筋 이름은 특정 국면에 대한 답이라 판이
// 움직이면 거짓이 되지만, 이것은 사람에 대한 값이라 판이 움직여도 그대로 참이다.
//
// 알리는 것은 단계가 바뀔 때뿐이다. 값은 매 수 조금씩 움직이는데 화면에 나가는 것은
// 5단계라, 매번 보내면 같은 그림을 다시 그리는 스냅샷만 늘어난다.
func (st *state) applySkill(e skill.Estimate) {
	before := strengthStep(skillShift(st.skill))
	st.skill = e
	if strengthStep(skillShift(e)) != before {
		st.broadcast()
	}
}

// rollback 은 직전 사람의 수를 물린다. 되돌리는 것은 국면·기보·표기·千日手 계수까지
// 전부다 — 하나라도 남으면 다음 판정이 그 흔적 위에서 돈다.
func (st *state) rollback(r judgeResult) {
	key := st.pos.RepetitionKey()
	if n := st.repeats[key]; n > 0 {
		st.repeats[key] = n - 1
	}

	st.pos, st.prevTo = st.prevPos, st.prevPrevTo
	st.moves = st.moves[:len(st.moves)-1]
	st.usis = st.usis[:len(st.usis)-1]
	st.searchGen++ // 물러진 국면에 대한 늦은 결과를 버리기 위해

	// 되돌아온 국면은 手筋 힌트를 물어봤던 바로 그 국면이다 — 같은 手数·같은 판이라 답이
	// 그대로 참이다. 세대만 새로 붙이면 다시 안 물어도 되고, 안 붙이면 물러질수록 힌트가
	// 사라진다 — 계단이 手筋을 짚어야 하는 자리가 정확히 거기다(pointHintAtTesuji).
	//
	// 아직 도는 중이면 손대지 않는다. 세대를 옮기면 그 결과가 첫 검사에서 버려지고
	// tesujiHinting 이 true 로 남아 그 판의 힌트가 통째로 멈춘다.
	if !st.tesujiHinting && st.tesujiHintAsked && st.tesujiHintLastPly == len(st.usis) {
		st.tesujiHintGen = st.searchGen
	}

	// 물러진 수는 여기서만 남는다. 기보에는 안 들어가므로, 이 한 줄이 없으면 개입에
	// 오염되지 않은 유일한 실력 신호가 사라진다(01-core.md §5).
	if st.cfg.Recorder != nil {
		st.cfg.Recorder.Retracted(len(st.usis)+1, r.move.USI, r.judgement.Verdict)
	}

	st.stuck++
	st.hint = buildHint(st.stuck, r.judgement.BestUSI)

	v := r.judgement.Verdict
	st.intervention = &Intervention{
		Kind:            string(v.Kind),
		Category:        string(v.Category),
		RetractedUSI:    r.move.USI,
		RetractedJa:     r.move.Ja,
		DeltaWin:        v.DeltaWin,
		LostMate:        v.LostMate,
		Message:         explain.Render(r.judgement.Facts),
		RetractedSFEN:   r.judgement.RetractedSFEN,
		RetractedChecks: r.judgement.RetractedChecks,
		Refutation:      r.judgement.Refutation,
	}
}

// advance 는 검증이 끝난 수를 판에 반영한다. 표기는 착수 전 국면에서 만들어야 한다.
//
// 종료 판정은 안 한다. 되만드는 쪽(replay)은 「끝나 있으면 이어할 수 없다」를 답해야
// 하는데, 그 답을 finish 가 내면 아직 서지도 않은 세션이 Recorder 에 종료를 흘린다.
func (st *state) advance(m shogi.Move, by Side) {
	ja := st.pos.MoveJa(m, st.prevTo)
	st.pos = st.pos.Apply(m)
	st.prevTo = int(m.To)
	st.moves = append(st.moves, Move{USI: m.USI(), Ja: ja, By: by})
	st.usis = append(st.usis, m.USI())
	st.searchGen++
	st.repeats[st.pos.RepetitionKey()]++
}

// apply 는 수를 반영하고 그것으로 대국이 끝났는지 본다.
func (st *state) apply(m shogi.Move, by Side) {
	st.advance(m, by)

	key := st.pos.RepetitionKey()
	switch {
	case st.pos.NoLegalMoves():
		// 쇼기에서는 手詰まり도 패배다. 어느 쪽이든 수번 측이 진다.
		status := StatusStalemate
		if st.pos.InCheck(st.pos.Turn) {
			status = StatusCheckmate
		}
		st.finish(status, st.sideOf(st.pos.Turn.Other()))
	case st.repeats[key] >= 4:
		// 千日手. 連続王手の千日手(반칙패)는 아직 구분하지 않는다 — 수순 전체를 보고
		// 매 수 王手였는지 따져야 하고, 초·중반에서는 거의 나오지 않는다.
		st.finish(StatusRepetition, "")
	}
}

func (st *state) finish(status Status, winner Side) {
	st.status = status
	st.winner = winner
	st.thinking = false
	st.judging = false
	st.searchGen++
	if st.cfg.Recorder != nil {
		st.cfg.Recorder.Finished(status, winner)
	}
}

// recordLastMove 는 확정된 직전 수를 기록에 넘긴다.
//
// apply 안이 아니라 확정되는 자리마다 부른다. 착수와 확정이 같은 순간이 아니다 —
// 사람의 수는 판정을 통과해야 확정되고, 물러지면 기보에서 사라진다.
func (st *state) recordLastMove() {
	if st.cfg.Recorder == nil || len(st.moves) == 0 {
		return
	}
	last := st.moves[len(st.moves)-1]
	st.cfg.Recorder.Moved(len(st.moves), last.USI, last.By)
	st.recordStyleTags()
}

// recordStyleTags 는 이 국면에서 사람에게 붙은 이름 중 처음 보는 것을 남긴다.
//
// 확정된 수 뒤에서만 부른다. 물러진 수 위에서 세면 되물러 사라진 형태가 기록에 남고,
// 그건 「짰다」가 아니라 「짤 뻔했다」다.
//
// 화면과 같은 함수(styleTags)를 쓴다 — 갈라 두면 판에 뜬 이름과 마이페이지가 세는
// 이름이 달라진다. 手筋만 뺀다. 그쪽은 엔진 평가치에 매여 있고 이름의 정확도가 아직
// 보류 중이다(Recorder.Named).
func (st *state) recordStyleTags() {
	for _, t := range st.styleTags() {
		if t.Kind == tag.KindTesuji || st.namedStyle[t.Code] {
			continue
		}
		if st.namedStyle == nil {
			st.namedStyle = map[string]bool{}
		}
		st.namedStyle[t.Code] = true
		st.cfg.Recorder.Named(t.Code)
	}
}

// recordEvals 는 판정이 들고 온 평가치 둘을 두 手数에 한 번에 채운다(Recorder.Evaluated).
// 그래서 마지막 수의 평가치는 안 채워진다 — 그 뒤에 사람의 수가 없으면 판정도 없다.
func (st *state) recordEvals(j Judgement) {
	if st.cfg.Recorder == nil || !j.HasEvals {
		return
	}
	ply := len(st.moves)
	st.cfg.Recorder.Evaluated(ply, j.SenteCpAfter)
	if ply >= 2 {
		st.cfg.Recorder.Evaluated(ply-1, j.SenteCpBefore)
	}
}

// maybeThink 는 엔진 차례면 탐색을 띄운다.
//
// 탐색은 세션 goroutine 밖에서 돈다. 여기서 기다리면 생각하는 동안 스냅샷 요청도
// 투료도 못 받는다 — 상태를 소유한 goroutine 은 오래 막히면 안 된다.
func (st *state) maybeThink(ctx context.Context, engineDone chan engineResult) {
	if st.status != StatusPlaying || st.thinking || st.pos.Turn == st.cfg.HumanColor {
		return
	}
	st.thinking = true
	st.pendingGen = st.searchGen

	opp := st.cfg.Opponent
	start := st.start
	// 슬라이스를 그대로 넘기면 다음 착수의 append가 같은 배열을 건드릴 수 있다.
	moves := append([]string(nil), st.usis...)
	// 값으로 복사해 넘긴다. goroutine 이 세션 상태를 읽으면 그 순간 소유 규약이 깨진다.
	sk := st.skill
	deadline := st.moveDeadline()
	chasing := st.chasing

	go func() {
		tctx, cancel := context.WithTimeout(ctx, deadline)
		var (
			usi string
			err error
		)
		if chasing {
			usi, err = chooseBest(tctx, opp, start, moves, sk)
		} else {
			usi, err = opp.Choose(tctx, start, moves, sk)
		}
		cancel()
		select {
		case engineDone <- engineResult{usi: usi, err: err}:
		case <-ctx.Done():
		}
	}()
}

// maybeGauge 는 사람 차례면 詰み 게이지를 그 국면의 값으로 다시 구한다.
//
// maybeThink 와 정확히 반대 조건이라 언제나 둘 중 하나만 돈다. 그래서 solver 풀이
// 하나여도 게이지와 개입 판정이 서로 기다리지 않는다.
//
// 탐색·판정처럼 세션 goroutine 밖에서 돈다.
func (st *state) maybeGauge(ctx context.Context, gaugeDone chan mateResult) {
	if st.cfg.Mate == nil || st.status != StatusPlaying {
		return
	}
	// 사람 차례에서만 묻는다. solver 는 수번 측의 詰み을 답하므로 이 자리라야 「내가
	// 상대 玉을 몇 手로 詰ますか」가 나온다 — 상대 차례에 물으면 반대쪽이 나온다(gauge.go).
	if st.pos.Turn != st.cfg.HumanColor {
		return
	}
	if st.gauging && st.gaugeGen == st.searchGen {
		return // 같은 국면을 두 번 묻지 않는다
	}
	st.gauging = true
	st.gaugeGen = st.searchGen

	gen := st.gaugeGen
	mate := st.cfg.Mate
	start := st.start
	deadline := st.extraDeadline()
	// 슬라이스를 그대로 넘기면 다음 착수의 append 가 같은 배열을 건드릴 수 있다.
	moves := append([]string(nil), st.usis...)

	go func() {
		gctx, cancel := context.WithTimeout(ctx, deadline)
		r, err := mate.SearchMate(gctx, start, moves)
		cancel()
		select {
		case gaugeDone <- mateResult{gen: gen, plies: len(r.Moves), err: err}:
		case <-ctx.Done():
		}
	}()
}

// applyMateHeat 는 구해진 게이지 세기를 반영한다.
func (st *state) applyMateHeat(r mateResult) {
	if !st.gauging || r.gen != st.gaugeGen {
		return // 그 사이 게이지를 다시 걸었다. 늦게 온 앞의 결과다
	}
	st.gauging = false

	if r.gen != st.searchGen {
		return // 국면이 움직였다. 스냅샷이 mateGen 으로 걸러내지만 적지도 않는다
	}
	if r.err != nil {
		// 게이지가 없다고 대국을 멈추지 않는다. 개입 판정과 같은 판단이다 —
		// 테두리가 어두운 채로 남고 대국은 그대로 간다.
		log.Printf("game: mate gauge failed, the border stays dark: %v", r.err)
		return
	}

	st.mateHeat, st.mateGen, st.matePlies = mateHeat(r.plies), r.gen, r.plies
	// 세기가 그대로여도 뿌린다. 국면이 바뀌면 스냅샷이 게이지를 껐다가 여기서 다시
	// 켜는데, 「바뀌었을 때만」으로 두면 값이 같은 국면에서 꺼진 채로 남는다.
	st.broadcast()
}

func (st *state) applyEngineMove(ctx context.Context, r engineResult, engineDone chan engineResult, gaugeDone chan mateResult, tesujiDone chan tesujiHintResult) {
	if !st.thinking || st.pendingGen != st.searchGen {
		return // 롤백·투료로 국면이 바뀐 뒤 도착한 결과. 버린다
	}
	st.thinking = false

	// 상대의 수를 못 얻은 것과 상대가 던진 것은 다르다. 아래 resign 은 엔진이 스스로
	// 그렇게 답한 것이라 사람의 승리가 맞지만, 여기는 시한을 넘겼거나 엔진이 고장 난
	// 것이다 — 승패를 지어내지 않고 中断으로 접는다(StatusAborted).
	if r.err != nil {
		log.Printf("game: engine search failed, aborting the game: %v", r.err)
		st.finish(StatusAborted, "")
		st.broadcast()
		return
	}

	switch r.usi {
	case "resign", "win", "none", "":
		st.finish(StatusResigned, SideHuman)
		st.broadcast()
		return
	}

	// 엔진 출력을 그대로 믿지 않는다. 국면을 잘못 보냈거나 엔진이 헷갈린 경우
	// 여기서 잡히고, 안 잡으면 기보가 조용히 깨진 채로 남는다.
	m, err := shogi.ParseUSIMove(r.usi)
	if err == nil {
		err = st.pos.ValidateMove(m)
	}
	if err != nil {
		log.Printf("game: engine returned an unplayable move %q: %v", r.usi, err)
		st.finish(StatusAborted, "")
		st.broadcast()
		return
	}

	st.apply(m, SideEngine)
	st.recordLastMove() // 상대 수는 판정하지 않으므로 두는 즉시 확정이다
	st.computeTagHints()
	st.maybeTesujiHint(ctx, tesujiDone)
	st.maybeThink(ctx, engineDone)
	st.maybeGauge(ctx, gaugeDone)
	st.broadcast()
}

func (st *state) sideOf(c shogi.Color) Side {
	if c == st.cfg.HumanColor {
		return SideHuman
	}
	return SideEngine
}

// movesBy 는 한쪽이 둔 수만 순서대로 낸다. 전법·戦型 판정의 입력이다.
//
// 물러진 수는 들어 있지 않다. st.moves 는 롤백 때 잘리므로 되물러진 수로 전법이
// 정해지는 일이 없다 — 그렇게 되면 개입이 기보를 바꾸는 것이 된다.
func (st *state) movesBy(side Side) []string {
	out := make([]string, 0, len(st.moves))
	for _, m := range st.moves {
		if m.By == side {
			out = append(out, m.USI)
		}
	}
	return out
}

// styleTags 는 화면에 나갈 이름 전부다 — 囲い·전법·戦型은 판에서 매번 다시 세고,
// 手筋은 엔진 게이트를 통과한 것만 실린다.
//
// 두 축이 다르게 오는 것은 비용 때문이 아니다. 囲い는 판만 보면 알 수 있지만 手筋은
// 「이득인가」를 엔진 평가치가 정하고 그 값은 판에서 다시 읽을 수 없다 — 그래서
// 판정이 끝난 자리에서 한 번 구해 세대와 함께 들고 있는다(applyVerdict).
//
// 手筋이 먼저 온다. 화면은 새로 붙은 이름 하나를 골라 잠깐 띄우는데(useTagAnnounce),
// 手筋은 국면이 움직이면 사라지고 囲い는 남아 다음 스냅샷에서도 뜬다 — 뒤에 두면
// 한 스냅샷에 둘이 함께 붙었을 때 사라지는 쪽이 밀려 영영 안 뜬다.
func (st *state) styleTags() []tag.Tag {
	var out []tag.Tag
	if st.tesujiGen == st.searchGen {
		out = append(out, st.tesuji...)
	}
	return append(out, tag.Detect(tag.Input{
		Pos:           st.pos,
		Color:         st.cfg.HumanColor,
		PlayerMoves:   st.movesBy(SideHuman),
		OpponentMoves: st.movesBy(SideEngine),
	})...)
}

const (
	TagHintMaxPerGame = 3
	TagHintCooldown   = 10
)

// hintable 은 그 축의 이름을 착수 前에 권해도 되는가다. 이름을 붙이는 쪽(styleTags)은
// 셋 다 그대로 내므로, 여기서 거르는 것은 제안뿐이다.
//
// 빼는 이유가 축마다 다르다:
//
//	囲い   짓다 만 형태에 이름이 없어서, 「이 수를 두면 이름이 생긴다」가 구현한
//	       종류 수에 달린 임의의 한 수가 된다 — §44
//	전법   飛를 어느 筋으로 振るか는 그 사람이 고르는 것이다. 첫 수 앞의
//	       「中飛車になります」는 힌트가 아니라 지시다 — §71
//
// 남는 것은 戦型이다. 角換わり처럼 판 전체가 이미 그렇게 되어 있는가를 말하는 축이라
// 「무엇을 골라라」가 아니다.
func hintable(t tag.Tag) bool {
	return t.Kind == tag.KindOpening
}

// computeTagHints 는 플레이어의 합법수 중 새 이름을 만드는 것을 찾는다. 무엇을 권할지는
// hintable 이 정한다.
//
// 엔진을 안 부른다 — 戦型은 판과 수순만으로 정해지므로 합법수마다 둬 보면 끝이다.
// 手筋은 평가치가 있어야 해서 비동기로 따로 구한다(maybeTesujiHint).
func (st *state) computeTagHints() {
	st.tagHintGen = st.searchGen

	if st.status != StatusPlaying || st.pos.Turn != st.cfg.HumanColor {
		st.tagHints = nil
		return
	}
	if st.tagHintCount >= TagHintMaxPerGame {
		st.tagHints = nil
		return
	}
	ply := len(st.moves)
	if st.tagHintLastPly > 0 && ply > st.tagHintLastPly && ply-st.tagHintLastPly < TagHintCooldown {
		st.tagHints = nil
		return
	}

	playerMoves := st.movesBy(SideHuman)
	oppMoves := st.movesBy(SideEngine)

	have := map[string]bool{}
	for _, t := range tag.Detect(tag.Input{
		Pos: st.pos, Color: st.cfg.HumanColor,
		PlayerMoves: playerMoves, OpponentMoves: oppMoves,
	}) {
		have[t.Code] = true
	}

	seen := map[string]bool{}
	var hints []tag.Tag
	for _, m := range st.pos.LegalMoves() {
		after := st.pos.Apply(m)
		afterMoves := append(append([]string(nil), playerMoves...), m.USI())
		for _, t := range tag.Detect(tag.Input{
			Pos: after, Color: st.cfg.HumanColor,
			PlayerMoves: afterMoves, OpponentMoves: oppMoves,
		}) {
			if !hintable(t) {
				continue
			}
			if !have[t.Code] && !seen[t.Code] {
				seen[t.Code] = true
				hints = append(hints, t)
			}
		}
	}
	st.tagHints = hints
	if len(hints) > 0 && st.tagHintLastPly != ply {
		st.tagHintCount++
		st.tagHintLastPly = ply
	}
}

// maybeTesujiHint 는 사람 차례면 「지금 두면 새 手筋 이름이 생기는 수」를 찾아 그것이
// 엔진에게도 이득인지 묻고, 통과한 것만 남긴다. 빈도 상한과 쿨다운도 여기서 본다.
//
// computeTagHints 와 갈리는 이유는 하나다 — 手筋은 「그래서 得인가」를 엔진이 답해야
// 이름이 붙는다(tesuji.go). 그래서 게이지와 같은 모양이다: goroutine 으로 던지고
// 세대로 걸러 받는다(maybeGauge).
//
// 엔진을 걸기 전에 룰로 거르지만 그것이 걸러 주는 양은 적고(journal §56) 필터 자체도
// 싸지 않다 — 그래서 필터까지 세션 goroutine 밖에서 돈다(tesujiOptions).
func (st *state) maybeTesujiHint(ctx context.Context, done chan tesujiHintResult) {
	if st.cfg.TesujiHint == nil || st.status != StatusPlaying {
		return
	}
	// 사람 차례에서만 묻는다. 상대 차례의 手筋은 알릴 것이 아니라 당할 것이다.
	if st.pos.Turn != st.cfg.HumanColor {
		return
	}
	if st.tesujiHinting && st.tesujiHintGen == st.searchGen {
		return // 같은 국면을 두 번 묻지 않는다
	}
	if st.tesujiHintCount >= TagHintMaxPerGame {
		return
	}
	// 쿨다운은 띄운 자리가 아니라 물어본 자리에서 잰다. 뜬 자리에서만 재면 게이트가 한
	// 번도 안 열리는 판에서 이 탐색이 사람 차례마다 돈다(journal §56 · §74). 탐색이 후보
	// 수와 무관하게 한 번이 된 뒤에도(gateTesujiOptions) 그대로인 것은, 아끼는 것이 탐색
	// 횟수만이 아니라 룰 필터이기 때문이다. 상한(tesujiHintCount)은 뜬 횟수를 센다.
	ply := len(st.moves)
	if st.tesujiHintAsked && ply-st.tesujiHintLastPly < TagHintCooldown {
		return
	}

	st.tesujiHinting = true
	st.tesujiHintGen = st.searchGen
	st.tesujiHintLastPly, st.tesujiHintAsked = ply, true
	// 앞 국면의 후보를 여기서 버린다. 세대를 이 국면에 붙인 순간 스냅샷이 그것을 실어
	// 보내는데(snapshot), 아직 이 국면에 대해 아는 것이 없다. 룰 필터가 goroutine 으로
	// 들어가면서 그 사이가 밀리초에서 초 단위가 됐다(§56).
	st.tesujiOpts = nil

	gen := st.tesujiHintGen
	search := st.cfg.TesujiHint
	start := st.start
	color := st.cfg.HumanColor
	deadline := st.extraDeadline()
	// 판도 값으로 복사해 넘긴다. 룰로 거르는 동안에도 세션은 다음 수를 받는다.
	// shogi.Position 이 값 타입이라 복사면 끝이다.
	pos := st.pos
	// 슬라이스를 그대로 넘기면 다음 착수의 append 가 같은 배열을 건드릴 수 있다.
	moves := append([]string(nil), st.usis...)

	go func() {
		hctx, cancel := context.WithTimeout(ctx, deadline)
		defer cancel()

		// 룰 필터도 여기서 돈다. 세션 goroutine 안에 있었을 때 종반에 초 단위로 막았다
		// (비용은 tesujiOptions, 실측은 journal §56).
		//
		// 시한 안에 두는 것은 예산을 정직하게 세기 위해서다. 이 함수 자체는 ctx 를 안 보므로
		// 중간에 끊기지 않고, 오래 걸린 만큼 아래 게이트의 몫이 줄어든다.
		opts := tesujiOptions(pos, color)
		var (
			kept    []TesujiOption
			dropped int
			err     error
		)
		if len(opts) > 0 {
			kept, dropped, err = gateTesujiOptions(hctx, search, JudgeDepth, TesujiHintRootK, start, moves, opts, color)
		}
		select {
		case done <- tesujiHintResult{gen: gen, opts: kept, dropped: dropped, err: err}:
		case <-ctx.Done():
		}
	}()
}

// applyTesujiHint 는 게이트를 통과한 후보를 반영한다.
func (st *state) applyTesujiHint(r tesujiHintResult) {
	if !st.tesujiHinting || r.gen != st.tesujiHintGen {
		return // 그 사이 다시 걸었다. 늦게 온 앞의 결과다
	}
	st.tesujiHinting = false

	if r.gen != st.searchGen {
		return // 국면이 움직였다. 낡은 평가치로 이름을 붙이는 것이 게이트를 없애는 것과 같다
	}
	// 에러보다 먼저 센다. 시한을 넘기면 남은 후보가 통째로 여기로 오는데(gateTesujiOptions),
	// 에러 뒤에 두면 그 판에서 제일 많이 잘린 회차만 로그에 안 남는다.
	if r.dropped > 0 {
		// 잘린 것을 안 세면 「手筋이 없었다」와 「못 봤다」가 같은 화면이 된다.
		log.Printf("game: tesuji hint skipped %d candidate(s)", r.dropped)
	}
	if r.err != nil {
		// 힌트가 없다고 대국을 멈추지 않는다. 게이지·개입 판정과 같은 판단이다.
		log.Printf("game: tesuji hint search failed, the hint stays quiet: %v", r.err)
		return
	}

	st.tesujiOpts = r.opts
	if len(r.opts) > 0 {
		st.tesujiHintCount++
		st.pointHintAtTesuji()
	}
	st.broadcast()
}

// pointHintAtTesuji 는 이미 열린 계단이 최선수 대신 手筋을 짚게 바꾼다.
//
// 계단을 새로 만들지 않는다. 둘 다 「네가 무엇을 두면 되는가」이고 발동 조건도 같아서,
// 따로 두면 같은 파랑이 두 뜻이 된다(journal §41).
//
// 바꿔도 되는 근거는 게이트다 — 후보는 전부 TesujiLossCp 안이고 대신 이름이 있다
// (01-core.md §7.1). 여럿이면 첫 번째를 쓴다. LegalMoves 순서라 결정적이고, 전부 같은
// 게이트를 지났으므로 여기서 새로 고를 근거가 없다.
func (st *state) pointHintAtTesuji() {
	if st.hint == nil || len(st.tesujiOpts) == 0 {
		return
	}
	if h := buildHint(st.stuck, st.tesujiOpts[0].USI); h != nil {
		st.hint = h
	}
}

func (st *state) snapshot() Snapshot {
	turn := "b"
	if st.pos.Turn == shogi.White {
		turn = "w"
	}
	yours := st.status == StatusPlaying && !st.judging && st.pos.Turn == st.cfg.HumanColor

	yourColor := "b"
	if st.cfg.HumanColor == shogi.White {
		yourColor = "w"
	}

	snap := Snapshot{
		SFEN:            st.pos.SFEN(),
		Ply:             len(st.moves),
		Turn:            turn,
		YourTurn:        yours,
		YourColor:       yourColor,
		InCheck:         st.pos.InCheck(st.pos.Turn),
		Thinking:        st.thinking,
		Moves:           append([]Move(nil), st.moves...),
		Status:          st.status,
		Winner:          st.winner,
		Judging:         st.judging,
		Intervention:    st.intervention,
		Hint:            st.hint,
		Notice:          st.notice,
		StyleTags:       st.styleTags(),
		OpponentOpening: st.cfg.OpponentOpening,
		HandicapJa:      handicap.NameOf(st.cfg.StartSFEN),
		// 사람 관점이다 — 화면이 후보를 그 관점으로 칠한다(Snapshot.BaselineCp).
		BaselineCp: handicap.BaselineCpFor(st.cfg.StartSFEN, st.cfg.HumanColor),
		UndoLeft:   max(UndoMaxPerGame-st.undos, 0),
		HintLeft:   max(HintMaxPerGame-st.hints, 0),
		// 화면이 조건을 다시 짓지 않게 여기서 답한다. yourTurn && undoLeft > 0 으로
		// 흉내내면 「사람이 아직 한 수도 안 뒀다」가 빠지고, 그 자리에서 누른 버튼이
		// 거절로 돌아온다.
		CanUndo: yours && st.undos < UndoMaxPerGame && st.lastHumanMove() >= 0,
		// askHint 의 거절 조건과 같다 — 엔진이 있고, 사람 차례이고, 예산이 남았고,
		// 이 국면에서 아직 답까지 안 봤다.
		CanHint: st.cfg.HintSearch != nil && yours && !st.judging && !st.thinking &&
			st.hints < HintMaxPerGame && st.hintStages[shogi.PositionKey(st.pos)] < HintStageMax,
	}
	// 국면이 움직였으면 게이지는 그 자리에서 무효다(state.mateGen).
	if st.mateGen == st.searchGen {
		snap.MateHeat = st.mateHeat
	}
	// 추정기가 있고 상대가 그 값을 볼 때만 강함을 말한다(Snapshot.OpponentStrength).
	if st.cfg.Rater != nil && adaptsToSkill(st.cfg.Opponent) {
		snap.OpponentStrength = strengthStep(skillShift(st.skill))
	}
	// 囲い·전법과 手筋이 같은 칸으로 나간다(§41). 세대를 따로 보는 것은 手筋이 엔진을
	// 기다리느라 몇 초 늦게 합류하기 때문이다.
	//
	// 새 슬라이스에 담는다. st.tagHints 에 그대로 덧붙이면 이미 뿌린 스냅샷과 배열을
	// 공유하게 되고, 구독자가 그것을 읽는 동안 세션이 다음 append 를 쓴다.
	var hints []tag.Tag
	if st.tagHintGen == st.searchGen {
		hints = append(hints, st.tagHints...)
	}
	if st.tesujiHintGen == st.searchGen {
		hints = append(hints, tesujiHintTags(st.tesujiOpts)...)
	}
	snap.TagHints = hints
	if yours {
		for _, m := range st.pos.LegalMoves() {
			snap.LegalMoves = append(snap.LegalMoves, m.USI())
		}
	}
	return snap
}

func (st *state) broadcast() {
	snap := st.snapshot()
	for ch := range st.subs {
		notify(ch, snap)
	}
}

func (st *state) closeSubs() {
	for ch := range st.subs {
		close(ch)
	}
	st.subs = nil
}

// notify 는 막히지 않고 최신 스냅샷을 넣는다.
//
// 느린 클라이언트 하나가 세션 goroutine 을 멈추면 그 대국이 통째로 선다. 스냅샷은
// 언제나 전체 상태라 중간 것을 버려도 손실이 없다.
func notify(ch chan Snapshot, snap Snapshot) {
	for range 2 {
		select {
		case ch <- snap:
			return
		default:
		}
		select {
		case <-ch: // 낡은 것을 버리고 다시 시도
		default:
		}
	}
}

func (s *Session) send(ctx context.Context, c command) (Snapshot, error) {
	c.reply = make(chan result, 1)
	select {
	case s.cmds <- c:
	case <-s.closed:
		return Snapshot{}, ErrClosed
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
	select {
	case r := <-c.reply:
		return r.snap, r.err
	case <-s.closed:
		return Snapshot{}, ErrClosed
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
}

// Play 는 사람의 수를 둔다. 불법수면 *shogi.IllegalMoveError 가 돌아온다.
func (s *Session) Play(ctx context.Context, usi string) (Snapshot, error) {
	return s.send(ctx, command{kind: cmdMove, usi: usi})
}

// Resign 은 사람이 投了한다.
func (s *Session) Resign(ctx context.Context) (Snapshot, error) {
	return s.send(ctx, command{kind: cmdResign})
}

// Undo 는 사람이 직전 자기 수를 무른다(待った).
//
// 예산을 다 썼으면 ErrNoUndoLeft, 되돌릴 수가 없으면 ErrNothingToUndo,
// 사람 차례가 아니면 ErrNotYourTurn 이다.
func (s *Session) Undo(ctx context.Context) (Snapshot, error) {
	return s.send(ctx, command{kind: cmdUndo})
}

// Hint 는 지금 국면의 최선수 힌트를 부른다.
//
// 돌아온 스냅샷에는 아직 힌트가 없다. 탐색이 끝나야 단계가 정해지므로 구독 채널로
// 한 번 더 온다(착수·판정과 같은 규약). 예산이 없거나 이미 답까지 본 국면이면 그
// 자리에서 거절한다(askHint).
func (s *Session) Hint(ctx context.Context) (Snapshot, error) {
	return s.send(ctx, command{kind: cmdHint})
}

// Snapshot 은 지금 상태를 돌려준다.
func (s *Session) Snapshot(ctx context.Context) (Snapshot, error) {
	return s.send(ctx, command{kind: cmdSnapshot})
}

// Subscribe 는 상태가 바뀔 때마다 스냅샷을 받는 채널을 준다.
// 반환된 취소 함수를 반드시 부른다. 세션이 끝나면 채널이 닫힌다.
func (s *Session) Subscribe(ctx context.Context) (<-chan Snapshot, func(), error) {
	ch := make(chan Snapshot, 1)
	if _, err := s.send(ctx, command{kind: cmdSubscribe, sub: ch}); err != nil {
		return nil, nil, err
	}
	cancel := func() {
		_, _ = s.send(context.WithoutCancel(ctx), command{kind: cmdUnsubscribe, sub: ch})
	}
	return ch, cancel, nil
}

// Close 는 세션을 끝내고 goroutine이 정리될 때까지 기다린다. 두 번 불러도 안전하다.
func (s *Session) Close() {
	s.closeOnce.Do(func() { close(s.done) })
	<-s.closed
}

// ─── 부르는 힌트 ─────────────────────────────────────────────
// 사람이 눌러서 받는 최선수 힌트. 근거와 예산은 HintMaxPerGame · journal §78.

// hintResult 는 힌트 탐색 하나의 결과다. 게이지·판정과 같은 규약으로 세대를 들고 온다.
type hintResult struct {
	gen  int
	key  string
	best string
	err  error
}

// askHint 는 지금 국면의 최선수를 물어 힌트를 만든다.
//
// 거절은 여기서 전부 끝낸다. 탐색을 걸기 전에 예산·단계·차례를 보므로, 답이 오는
// 쪽(applyHintResult)은 「늦게 온 앞의 결과인가」만 본다.
func (st *state) askHint(ctx context.Context, done chan hintResult) error {
	if st.cfg.HintSearch == nil {
		return ErrNoHint
	}
	if st.status != StatusPlaying {
		return ErrFinished
	}
	// 판정 중이거나 상대가 생각 중이면 국면이 아직 사람에게 안 왔다 — 그때의 최선수는
	// 사람이 둘 자리의 것이 아니다. 무르기와 같은 자리, 같은 이유다(undo).
	if st.judging || st.thinking || st.pos.Turn != st.cfg.HumanColor {
		return ErrNotYourTurn
	}
	if st.hints >= HintMaxPerGame {
		return ErrNoHintLeft
	}
	key := shogi.PositionKey(st.pos)
	if st.hintStages[key] >= HintStageMax {
		return ErrHintSeen
	}
	if st.hinting {
		return nil // 이미 묻고 있다. 두 번 세지 않는다
	}

	st.hinting = true
	st.hintGen = st.searchGen

	gen, start := st.hintGen, st.start
	search := st.cfg.HintSearch
	depth := JudgeDepth
	// 슬라이스를 그대로 넘기면 다음 착수의 append 가 같은 배열을 건드린다.
	moves := append([]string(nil), st.usis...)
	deadline := st.extraDeadline()

	go func() {
		hctx, cancel := context.WithTimeout(ctx, deadline)
		res, err := search.SearchMultiPV(hctx, start, moves, depth, 1)
		cancel()
		select {
		case done <- hintResult{gen: gen, key: key, best: res.Best, err: err}:
		case <-ctx.Done():
		}
	}()
	return nil
}

// applyHintResult 는 구해진 최선수를 그 국면의 다음 단계로 그린다.
//
// 예산은 물어본 자리가 아니라 답이 온 여기서 센다. 그래야 탐색이 실패한 회차가
// 예산을 먹지 않는다.
func (st *state) applyHintResult(r hintResult) {
	if !st.hinting || r.gen != st.hintGen {
		return // 그 사이 다시 걸었다. 늦게 온 앞의 결과다
	}
	st.hinting = false

	if r.gen != st.searchGen {
		return // 국면이 움직였다. 다른 국면의 최선수를 지금 판에 짚으면 거짓이다
	}
	if r.err != nil {
		// 힌트가 없다고 대국을 멈추지 않는다. 게이지·판정과 같은 판단이다.
		log.Printf("game: hint search failed, nothing is shown: %v", r.err)
		st.notice = newNotice(NoticeHintFailed)
		st.broadcast()
		return
	}
	if r.best == "" || r.best == "resign" || r.best == "win" {
		// 둘 수가 없거나 판이 끝난 국면이다. 예산을 안 쓴다.
		st.notice = newNotice(NoticeHintFailed)
		st.broadcast()
		return
	}

	stage := st.hintStages[r.key] + 1
	h := buildHint(hintStuck(stage), r.best)
	if h == nil {
		st.notice = newNotice(NoticeHintFailed)
		st.broadcast()
		return
	}

	st.hintStages[r.key] = stage
	st.hints++
	st.hint = h
	// 답까지 봤을 때만 기억한다. 1단계는 駒만 짚으므로 그 수를 뒀다고 「알려준 답을
	// 그대로 뒀다」가 되지 않는다 — 어디로 갈지는 여전히 사람이 찾았다.
	if stage >= HintStageMax {
		st.hintedKey, st.hintedUSI, st.hintReported = r.key, r.best, false
	}

	if st.cfg.Recorder != nil {
		st.cfg.Recorder.Hinted(len(st.moves), r.key, stage, r.best)
	}
	st.broadcast()
}

// hintStuck 은 힌트 단계를 buildHint 가 아는 자(연속 되무르기 횟수)로 옮긴다.
//
// 두 문이 같은 그림을 그린다 — 1단계는 그 駒에 테, 2단계는 그 수에 화살표다. 갇힘
// 힌트와 다른 것은 문을 여는 방식뿐이라(실패 횟수 vs 예산) 그리는 쪽은 한 벌이고,
// 여기가 두 자 사이의 유일한 번역이다.
func hintStuck(stage int) int {
	if stage >= HintStageMax {
		return HintMoveAfter
	}
	return HintPieceAfter
}
