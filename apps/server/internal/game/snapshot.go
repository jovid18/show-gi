package game

// 클라이언트가 보는 타입들이다. **json 태그가 곧 웹과의 계약이다** — 여기서 태그 하나를
// 바꾸면 apps/web 이 그 자리에서 깨진다.
//
// 스냅샷은 언제나 통째로 나간다. 롤백이 있는 이상 부분 갱신을 보내면, 물러진 뒤 화면과
// 서버가 어긋나도 아무도 모른다.

import (
	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/tag"
)

// Status 는 대국이 끝났는지, 끝났다면 왜인지다.
type Status string

const (
	StatusPlaying    Status = "playing"
	StatusCheckmate  Status = "checkmate"  // 詰み
	StatusStalemate  Status = "stalemate"  // 手詰まり — 쇼기에서는 이것도 패배다
	StatusResigned   Status = "resigned"   // 投了
	StatusRepetition Status = "repetition" // 千日手
	// StatusAborted 는 **상대의 수를 못 얻어서** 판을 접은 것이다. 승패가 없다.
	//
	// 投了와 갈라 두는 것이 요점이다. 엔진이 답하지 않은 것을 「相手が投了しました」로
	// 적으면 지고 있던 판이 기록에서 이긴 판이 되고, 그건 이 회차가 반대 방향으로 이미
	// 한 번 겪은 실패다(playtests/2026-08-13-human-1.md). 기록에서는 `abandoned` 로
	// 떨어지므로(server/recorder.go 의 resultOf) **이어하기 목록에 그대로 올라온다** —
	// 다시 붙으면 엔진 풀이 빈 상태에서 같은 국면을 다시 묻는다.
	StatusAborted Status = "aborted"
)

// Side 는 대국자다. 화면 문자열이 아니라 식별자이므로 영어로 둔다.
type Side string

const (
	SideHuman  Side = "human"
	SideEngine Side = "engine"
)

// Move 는 기보 한 수다.
type Move struct {
	USI string `json:"usi"`
	Ja  string `json:"ja"` // 棋譜 표기(▲7六歩). 화면에 그대로 나간다
	By  Side   `json:"by"`
}

// Snapshot 은 클라이언트가 보는 대국 상태 전부다.
//
// 부분 갱신을 보내지 않는다. 롤백이 있는 이상 "무엇이 바뀌었는지"를 클라이언트가
// 재구성하게 두면, 물러진 뒤 화면과 서버가 어긋나도 아무도 모른다.
type Snapshot struct {
	SFEN     string `json:"sfen"`
	Ply      int    `json:"ply"`
	Turn     string `json:"turn"` // "b" | "w"
	YourTurn bool   `json:"yourTurn"`
	InCheck  bool   `json:"inCheck"`
	Thinking bool   `json:"thinking"`

	// YourColor 는 사람이 잡은 쪽이다("b" | "w"). **판을 어느 쪽에서 보여줄지가 여기 걸려
	// 있다** — 화면이 Turn 하나로 되짚으면 상대 차례에 판이 뒤집힌다.
	YourColor string `json:"yourColor"`

	// OpponentOpening 은 상대가 따르는 진형의 일본어 이름이다. 안 골랐으면 비어 있다.
	//
	// **상대의 형태를 알려주지 않는다는 것(01-core.md §7)과 어긋나지 않는다.** 그쪽이 막는
	// 것은 국면에서 자라난 상대의 계획이고, 이건 시작 화면에서 **사람이 고른 값을 되비추는**
	// 것이다. 골라 놓고 화면에 안 뜨면 기능이 도는지를 사람이 알 수 없다.
	OpponentOpening string `json:"opponentOpening,omitempty"`

	// LegalMoves 는 사람 차례일 때만 채운다.
	//
	// 클라이언트는 여기 있는 수만 고르게 만든다. 그래서 실사용자는 반칙에 도달하지
	// 않고, 서버의 반칙 검사는 API 직접 호출과 국면 어긋남에 대한 방어로만 남는다.
	LegalMoves []string `json:"legalMoves"`

	Moves  []Move `json:"moves"`
	Status Status `json:"status"`
	Winner Side   `json:"winner,omitempty"`

	// Judging 은 방금 둔 수를 판정하는 중인가. 화면이 입력을 잠그는 데 쓴다.
	Judging bool `json:"judging"`
	// Intervention 은 직전 수가 물러졌을 때만 채워진다. 다음 착수에서 지워진다.
	Intervention *Intervention `json:"intervention,omitempty"`
	// Hint 는 같은 국면에서 여러 번 물러졌을 때 열리는 안내다. Intervention 과 수명이
	// 같지만 **뜻이 반대 방향**이라 따로 둔다 — 저쪽은 방금 둔 수를 말하고 이쪽은
	// 지금 둘 수를 말한다. 화면도 다른 색으로 그린다.
	Hint *Hint `json:"hint,omitempty"`

	// Notice 는 **대국은 그대로 도는데 우리가 못 해준 것**이다. 다음 착수에서 지워진다.
	Notice *Notice `json:"notice,omitempty"`

	// StyleTags 는 플레이어의 囲い·전법과 **엔진이 이득으로 본 手筋**의 이름이다.
	// **플레이어 쪽만 채운다** — 상대의 형태는 곧 상대의 계획이다(01-core.md §7).
	// 두 축이 다르게 오는 이유는 styleTags().
	StyleTags []tag.Tag `json:"styleTags,omitempty"`

	// TagHints 는 이 국면에서 플레이어가 둘 수 있는 수 중 **새 이름을 만드는 것이 있을 때**
	// 그 이름이다. 착수 전에 뜨는 제안형 힌트의 데이터이고, 수를 짚지 않는다(01-core.md §7).
	TagHints []tag.Tag `json:"tagHints,omitempty"`

	// MateHeat 는 詰み 게이지의 세기다(1..MateHeatMax). 0이면 꺼져 있다. **상대 玉 쪽
	// 하나뿐이고 手数가 아니라 세기인** 이유는 gauge.go.
	//
	// 사람 차례에서만 구하고, 국면이 움직이면 그 자리에서 무효다(state.mateGen).
	MateHeat int `json:"mateHeat,omitempty"`

	// UndoLeft 는 사람이 **아직 무를 수 있는 횟수**다(UndoMaxPerGame 에서 뺀 것).
	// 0이면 다 썼다. 화면이 남은 횟수를 그대로 그린다.
	//
	// **CanUndo 와 갈라 두는 이유는 뜻이 다르기 때문이다.** 이쪽은 「예산이 얼마 남았나」라
	// 상대 차례에도 참이고, 저쪽은 「지금 이 순간 누를 수 있나」다. 하나로 합치면 상대가
	// 생각하는 동안 남은 횟수가 0으로 보인다.
	UndoLeft int `json:"undoLeft"`
	// CanUndo 는 **지금 무르기를 누를 수 있는가**다. 사람 차례이고, 예산이 남았고,
	// 되돌릴 사람의 수가 기보에 있어야 참이다(state.undo 의 거절 조건과 같은 셋).
	CanUndo bool `json:"canUndo"`

	// HintLeft 는 사람이 **아직 부를 수 있는 힌트 횟수**다(HintMaxPerGame 에서 뺀 것).
	// UndoLeft 와 같은 규약이다 — 예산이라 상대 차례에도 참이다.
	HintLeft int `json:"hintLeft"`
	// CanHint 는 **지금 힌트를 누를 수 있는가**다. 사람 차례이고, 예산이 남았고,
	// 이 국면에서 아직 답까지 안 봤어야 참이다(state.askHint 의 거절 조건과 같다).
	//
	// **국면마다 갈린다는 것이 UndoLeft 와 다른 점이다** — 예산이 남아도 같은 자리를
	// 세 번째로 물으면 false 다.
	CanHint bool `json:"canHint"`

	// OpponentStrength 는 상대가 지금 겨냥하는 강함이다(1~5, 5가 최선수 쪽). **추정기가
	// 꺼져 있으면 안 온다** — 0을 보내면 화면이 「고정된 강함」과 「조절 중이지만 아직
	// 모름」을 구별할 수 없고, 조절하지도 않는 판에 눈금을 그리게 된다.
	//
	// **대국 중에 나가는 실력 관련 값은 이것 하나다.** 같은 추정치를 「너의 실력」으로
	// 매 수 그리면 블런더 하나에 등급이 몇 계단 움직이는 것이 그대로 보인다
	// (skill.RiseRate 가 비대칭이다). 사람에게 붙는 이름은 **판이 끝난 뒤 한 번**이고
	// 총평이 싣는다(06-status.md §62).
	OpponentStrength int `json:"opponentStrength,omitempty"`
}

// Judgement 는 판정 결과와, 그것을 화면에 그리는 데 쓸 재료다.
//
// **반박 수순이 Verdict 안에 없는 것이 요점이다.** 거기 넣으면 intervene 이 USI 문자열을
// 받게 되어 「입력은 이미 구해진 숫자뿐」이 깨진다 — 카테고리를 스칼라로 받게 만든 것과
// 같은 이유다(06-status.md §15). 반박 수순은 판정의 입력도 출력도 아니고, 판정하면서
// 어차피 손에 들어온 **그리기 재료**다.
type Judgement struct {
	Verdict intervene.Verdict
	// RetractedSFEN 은 물러진 수를 **둔 직후**의 국면이다. 수순을 넘겨 보는 첫 장면이고,
	// 되돌아온 지금 판과는 다르다.
	RetractedSFEN string
	// RetractedChecks 는 물러진 수가 王手였다면 그것을 거는 말들이다.
	RetractedChecks []Attack
	// Refutation 은 「상대는 이렇게 벌한다」 — 착수 후 국면의 최선 수순이다.
	// 개입이 안 걸렸으면 비어 있다.
	Refutation []RefutationMove

	// SenteCpBefore·SenteCpAfter 는 착수 전·후 국면의 평가치다. **先手 관점 cp**이고
	// HasEvals 가 false면 판을 못 읽어 구하지 못한 것이다.
	//
	// **판정에는 안 쓴다** — 기보에 남기기 위한 값이다. cp를 원본으로 남겨두면 승률
	// 상수 K를 바꿔 지난 대국을 다시 채점할 수 있다(01-core.md §2의 K는 아직 실측 전이다).
	// 승률만 남기면 그 길이 닫힌다.
	SenteCpBefore int
	SenteCpAfter  int
	HasEvals      bool

	// Facts 는 설명 계층이 문장으로 바꿀 사실들이다. 개입이 안 걸렸으면 비어 있다.
	//
	// **판정의 출력이지 입력이 아니다.** 무엇을 말해도 되는지가 카테고리에 달려 있어서
	// (explain.Facts.used) 카테고리가 정해진 뒤에야 닫힌다. 그리고 여기 실리는 것은 이미
	// 결정적으로 구해진 사실뿐이라, 설명이 판을 다시 읽는 일이 없다.
	Facts explain.Facts

	// Threshold 는 이 판정에 쓰인 승률 낙폭 임계치다(intervene.Level.Threshold).
	//
	// **DeltaWin 과 함께 나가야 뜻이 정해진다.** 실력 추정은 낙폭을 이 값으로 나눠 0~1로
	// 만드는데(skill.Move), 레벨을 추정기 쪽에서 다시 보면 판정과 다른 값을 쓰는 날이 온다.
	Threshold float64

	// BestUSI 는 착수 **전** 국면의 최선수다. 판정하면서 어차피 손에 들어온 값이라
	// 추가 탐색이 없다.
	//
	// **화면으로 그냥 나가지 않는다** — 갇힘 힌트가 단계에 맞게 잘라 쓴다(buildHint).
	BestUSI string
}

// RefutationMove 는 반박 수순의 한 수다.
//
// 기보의 Move 와 달리 **그 수를 둔 뒤의 국면을 함께 싣는다.** 화면이 수순을 한 수씩
// 넘겨 보여주는데, 클라이언트가 스스로 수를 두면 규칙 엔진을 한 벌 더 갖는 것이라
// D2에서 「클라이언트는 규칙을 모른다」로 정해둔 것과 어긋난다. 판은 서버가 만든다.
//
// 持ち駒도 SFEN에 들어 있으므로 駒台까지 이 한 줄로 맞는다.
type RefutationMove struct {
	USI  string `json:"usi"`
	Ja   string `json:"ja"`
	By   Side   `json:"by"`
	SFEN string `json:"sfen"`
	// Checks 는 그 수 뒤에 **玉을 잡으러 오는 말들**이다. 王手가 아니면 비어 있다.
	//
	// 「王手다」까지는 국면만 봐도 알지만 **어느 말이 걸고 있는지**는 규칙을 알아야 하고,
	// 그건 클라이언트가 갖지 않기로 한 것이다(D2). 両王手가 여기서 둘로 나오고,
	// 그 둘이 곧 「먹어서 풀 수 없다」의 이유다.
	Checks []Attack `json:"checks,omitempty"`
}

// Attack 은 판 위에 그을 한 줄이다. 칸은 USI 좌표(`4i`).
type Attack struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Intervention 은 제지형 개입 하나다. 스냅샷에 실려 화면으로 간다.
type Intervention struct {
	Kind string `json:"kind"` // "blunder"
	// Category 는 **왜** 나쁜가다(intervene.Category). 화면은 이걸로 문장을 짓지 않고
	// Message 를 그대로 그린다 — 표기가 두 벌이 되지 않게. 나중에 DB의
	// interventions.category 와 약점 프로파일이 이 값을 쓴다.
	Category string `json:"category"`
	// RetractedUSI 는 물러진 수. **개입 없는 순수 실력 신호**라 나중에 DB로 간다
	// (interventions.retracted_usi — 기보에는 안 들어간다).
	RetractedUSI string `json:"retractedUsi"`
	// RetractedJa 는 그 수의 棋譜 표기. 화면에 그대로 나간다.
	RetractedJa string `json:"retractedJa"`
	// DeltaWin 은 승률 낙폭(0~1).
	DeltaWin float64 `json:"deltaWin"`
	// LostMate 는 詰み을 놓쳐서 걸렸는가. 문구가 갈린다.
	LostMate bool `json:"lostMate"`
	// Message 는 화면에 그대로 나가는 일본어 문구다.
	Message string `json:"message"`
	// RetractedSFEN 은 물러진 수를 **둔 직후**의 국면이다. 화면이 수순을 넘겨 볼 때의
	// 첫 장면이고, 되돌아온 지금 판(`Snapshot.SFEN`)과는 다르다.
	RetractedSFEN string `json:"retractedSfen"`
	// RetractedChecks 는 물러진 수가 王手였다면 그것을 거는 말들이다.
	RetractedChecks []Attack `json:"retractedChecks,omitempty"`
	// Refutation 은 「상대는 이렇게 벌한다」. 물러진 수를 그대로 뒀을 때의 최선 수순이고,
	// 첫 수가 상대의 수다. 못 구했으면 비어 있다 — 화면은 그때 넘기기를 안 띄운다.
	//
	// **이것은 최선수가 아니다.** 이 수순이 시작하는 국면은 되물러서 이미 사라졌으므로,
	// 여기 있는 어느 수도 「지금 이렇게 두라」가 되지 않는다. 금지된 것은 플레이어가
	// 뒀어야 할 수이고 이쪽은 **왜 나쁜가**에 속한다(01-core.md §1).
	Refutation []RefutationMove `json:"refutation,omitempty"`
}

// Notice 는 대국을 멈추지 않은 실패 하나다.
//
// **개입(Intervention)과 갈라 둔다.** 저쪽은 판에 대한 판단이고 이쪽은 **우리 쪽 사정**이다.
// 섞으면 「시한을 넘겨 못 확인했다」가 화면에서 「이 수는 괜찮았다」로 읽히는데, 학습 앱에서
// 그 둘은 정반대다 — 초심자는 아무 말이 없으면 통과한 것으로 읽는다.
type Notice struct {
	// Code 는 기계용이다. 화면은 이걸로 문장을 짓지 않는다 — 표기가 두 벌이 되면
	// 어긋났을 때 어느 쪽이 맞는지 알 수 없다(Intervention.Category 와 같은 자리).
	Code string `json:"code"`
	// Message 는 화면에 그대로 나가는 일본어다.
	Message string `json:"message"`
}

// 알림 문구. **여기가 유일한 목록이다** — 화면은 Message 를 그대로 그린다.
const (
	// NoticeJudgeSkipped 는 방금 둔 수를 판정하지 못했다는 것이다. 수는 그대로 선다.
	NoticeJudgeSkipped = "judge_skipped"

	// NoticeHintFailed 는 부른 힌트를 못 만들었다는 것이다. **예산은 안 줄었다** —
	// 사람이 아무것도 못 본 채로 한 번을 잃으면 그 기능은 신뢰를 잃는다(applyHintResult).
	NoticeHintFailed = "hint_failed"
)

var noticeMessages = map[string]string{
	NoticeJudgeSkipped: "今の手は確かめられませんでした。そのまま進みます。",
	NoticeHintFailed:   "ヒントを用意できませんでした。回数は減っていません。",
}

// newNotice 는 코드로 알림 하나를 만든다. 모르는 코드면 nil이다 — 빈 문구를 화면에
// 띄우느니 아무 말도 안 하는 쪽이 낫다.
func newNotice(code string) *Notice {
	msg, ok := noticeMessages[code]
	if !ok {
		return nil
	}
	return &Notice{Code: code, Message: msg}
}

// 갇힘 힌트가 열리는 지점. **같은 국면에서 연속으로 물러진 횟수**다 — 통과하는 수를
// 두면 0으로 돌아간다. 한 판 누적으로 세지 않는 이유는 06-status.md §23.
//
// **[미확정]** 3과 5는 초기값이다. 재채점에서 2와 4로 내렸다가 되돌렸다 — 표본이
// 전부 에이전트라 사람이 갇히는 모양이 아니었다(§39).
const (
	HintPieceAfter = 3
	HintMoveAfter  = 5
)

// Hint 는 갇혔을 때 열리는 계단식 안내다.
//
// **자르는 일은 서버가 한다.** 최선수를 통째로 내려보내고 화면이 출발 칸만 그리면
// 계단이 화면에만 있고 답은 devtools에 그대로 남는다.
//
// 「최선수를 보여주지 않는다」(01-core.md §1)와 어긋나지 않는 근거는 06-status.md §23.
type Hint struct {
	// Square 는 움직일 駒가 있는 칸(`5d`). 打면 비어 있다.
	Square string `json:"square,omitempty"`
	// Drop 은 駒台에서 집을 駒(`B`). 판 위의 수면 비어 있다.
	Drop string `json:"drop,omitempty"`
	// USI 는 그 수 전체. **마지막 단계에서만 채워진다.**
	USI string `json:"usi,omitempty"`
}

// buildHint 는 연속 되무르기 횟수와 최선수로 그 단계의 힌트를 만든다. 아직이면 nil.
func buildHint(stuck int, bestUSI string) *Hint {
	if stuck < HintPieceAfter || bestUSI == "" {
		return nil
	}
	m, err := shogi.ParseUSIMove(bestUSI)
	if err != nil {
		return nil
	}

	// 打의 駒 글자는 USI 문자열의 첫 글자(`B*4a`)다. 여기서 떼는 것은 shogi 에 역방향
	// 표를 새로 만들지 않기 위해서이고, 위에서 파싱이 이미 형식을 보증한다.
	var h Hint
	if m.IsDrop() {
		h.Drop = bestUSI[:1]
	} else {
		h.Square = shogi.SquareUSI(int(m.From))
	}
	if stuck >= HintMoveAfter {
		h.USI = bestUSI
	}
	return &h
}
