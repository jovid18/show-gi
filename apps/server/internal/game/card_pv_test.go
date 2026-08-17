package game

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 문장과 개입 카드가 **같은 탐색 결과**를 보는지. 사람이 남긴 그림이 여기서 나왔다 —
// 카드 후보 1위는 `△2五玉` 인데 문장은 「相手の最善手は△7七歩」였다(docs/playtests/
// 2026-08-13-human-1.md §6 #8). 어느 쪽도 버그가 아니었고 **묻는 k가 달랐다**(journal §58).

// pvStub 은 k에 따라 다른 답을 준다. 실엔진에서 MultiPV가 1위를 바꾸는 것이 이 항목의
// 전제이므로, 그 성질을 스텁이 그대로 갖고 있어야 테스트가 뜻을 갖는다.
type pvStub struct {
	// single 은 `SearchDepth`(k=1)의 답이다. 판정의 입력이 여기서 나온다.
	single usi.SearchResult
	// multi 는 수순(공백으로 이은 것) → k=OtherBranches 의 답이다. 없는 수순은 에러다.
	multi map[string]usi.SearchResult
	// asked 는 MultiPV로 물어본 수순들. **어느 국면을 물었나**가 여기서 보인다.
	asked [][]string
}

func (s *pvStub) SearchDepth(context.Context, string, []string, int) (usi.SearchResult, error) {
	return s.single, nil
}

func (s *pvStub) SearchMultiPV(_ context.Context, _ string, moves []string, _, _ int) (usi.SearchResult, error) {
	s.asked = append(s.asked, slices.Clone(moves))
	res, ok := s.multi[strings.Join(moves, " ")]
	if !ok {
		return usi.SearchResult{}, errors.New("stub: 그 국면은 안 물어야 한다")
	}
	return res, nil
}

// quietBlunder 는 **카테고리가 `other` 로 떨어지는** 수다. 아무것도 안 따고 王手도 아니고
// 그냥 잡히지도 않아서, `classify` 의 이름 붙은 분기 어디에도 안 걸린다.
var quietBlunder = []string{"1g1f"}

// openedDiagonal 도 `other` 다. 다른 것은 **상대가 딸 것이 생긴다**는 점뿐이다 — 角道가
// 서로 열려 있어서 8八의 角을 그냥 따인다. 판정 대상은 마지막의 端歩이고, 그 수는 딴 것도
// 王手도 아니라 이름 붙은 어느 분기에도 안 걸린다.
var openedDiagonal = []string{"7g7f", "3c3d", "1g1f"}

// judgeOtherBlunder 는 그 수순의 마지막 수를 판정한다. 착수 전 0 → 착수 후 −1600이라
// 개입은 반드시 걸린다.
func judgeOtherBlunder(t *testing.T, stub *pvStub, moves []string) Judgement {
	t.Helper()
	a := &engineAnalyst{search: stub, depth: JudgeDepth, level: intervene.Beginner}

	j, err := a.Judge(t.Context(), shogi.StartSFEN, moves, len(moves))
	if err != nil {
		t.Fatalf("판정: %v", err)
	}
	if j.Verdict.Kind != intervene.KindBlunder || j.Verdict.Category != intervene.CategoryOther {
		t.Fatalf("kind=%s category=%s — other 로 걸려야 이 테스트가 뜻을 갖는다",
			j.Verdict.Kind, j.Verdict.Category)
	}
	return j
}

// afterK1 은 착수 후 국면을 k=1로 읽은 것이다. 이 PV의 첫 수가 **문장에 나가서는 안 되는**
// 그 수다 — 카드는 같은 국면을 k=3으로 묻고 1위가 갈린다.
func afterK1() usi.SearchResult {
	return usi.SearchResult{
		Depth: JudgeDepth, Best: "3c3d", ScoreCp: 1600,
		PV: []string{"3c3d", "2g2f"},
	}
}

// **문장은 카드가 짚는 수를 말한다.** 판정이 손에 든 k=1 PV가 아니다.
func TestSentenceNamesTheMoveTheCardPoints(t *testing.T) {
	stub := &pvStub{
		single: afterK1(),
		multi: map[string]usi.SearchResult{
			// 카드가 묻는 그 국면. 1위가 k=1과 다르다.
			"1g1f": {Depth: JudgeDepth, Lines: []usi.SearchLine{
				pvLine(1, 1600, "8c8d", "2g2f"),
				pvLine(2, 1500, "3c3d", "2g2f"),
			}},
			// 그 1위 뒤의 국면. 갈래 셋이 여기서 나온다.
			"1g1f 8c8d": {Depth: JudgeDepth, Lines: []usi.SearchLine{
				pvLine(1, -1600, "2g2f", "8d8e"),
			}},
		},
	}

	j := judgeOtherBlunder(t, stub, quietBlunder)

	if j.Facts.OpponentBest != "△8四歩" {
		t.Errorf("상대 최선수 = %q, want △8四歩 — k=1의 △3四歩을 말하면 카드와 어긋난다",
			j.Facts.OpponentBest)
	}
	// **갈래도 그 수 뒤에서 자란다.** 뿌리가 k=1의 수면 문장의 첫 수와 갈래가 다른
	// 국면의 것이 된다.
	if len(j.Facts.Branches) != 1 || j.Facts.Branches[0].PlayerJa != "▲2六歩" {
		t.Errorf("갈래 = %+v", j.Facts.Branches)
	}
	// 물은 국면이 둘이고 순서가 정해져 있다 — 카드 국면 먼저, 그 뒤가 갈래 국면이다.
	want := [][]string{{"1g1f"}, {"1g1f", "8c8d"}}
	if len(stub.asked) != len(want) {
		t.Fatalf("MultiPV로 물은 국면 = %v", stub.asked)
	}
	for i := range want {
		if !slices.Equal(stub.asked[i], want[i]) {
			t.Errorf("[%d] 물은 수순 = %v, want %v", i, stub.asked[i], want[i])
		}
	}
}

// **「무엇을 취할 수 있는가」도 같은 수의 것이어야 한다.** 문장은 상대의 최선수와 그 수로
// 따이는 駒를 한 문장에 적는다(explain.renderBranches) — 출처가 갈리면 그 한 문장 안에서
// 어긋난다.
func TestThreatenedPieceComesFromTheSameMove(t *testing.T) {
	stub := &pvStub{
		single: afterK1(),
		multi: map[string]usi.SearchResult{
			// 1위가 8八角을 그냥 딴다. k=1의 △3四歩은 이미 둔 수라 아무것도 안 딴다.
			"7g7f 3c3d 1g1f": {Depth: JudgeDepth, Lines: []usi.SearchLine{
				pvLine(1, 1600, "2b8h+", "7i8h"),
			}},
			"7g7f 3c3d 1g1f 2b8h+": {Depth: JudgeDepth, Lines: []usi.SearchLine{
				pvLine(1, -1600, "7i8h", "3d3e"),
			}},
		},
	}

	j := judgeOtherBlunder(t, stub, openedDiagonal)

	if j.Facts.OpponentBest != "△8八角成" {
		t.Fatalf("상대 최선수 = %q", j.Facts.OpponentBest)
	}
	if j.Facts.Threatened != "角" {
		t.Errorf("따이는 駒 = %q, want 角 — k=1 PV로 세면 딴 것이 없어 빈 값이 된다",
			j.Facts.Threatened)
	}
}

// 카드 국면을 못 물으면 **판정이 손에 든 PV로 돌아간다.** 그때는 카드의 목록도 같은 이유로
// 안 서므로 화면에 모순이 남지 않고, 설명은 그대로 나간다.
func TestSentenceFallsBackWhenTheCardSearchFails(t *testing.T) {
	stub := &pvStub{
		single: afterK1(),
		multi: map[string]usi.SearchResult{
			// 카드 국면은 없다 — 스텁이 에러를 준다. 갈래 국면만 답한다.
			"1g1f 3c3d": {Depth: JudgeDepth, Lines: []usi.SearchLine{
				pvLine(1, -1600, "2g2f", "3d3e"),
			}},
		},
	}

	j := judgeOtherBlunder(t, stub, quietBlunder)

	if j.Facts.OpponentBest != "△3四歩" {
		t.Errorf("상대 최선수 = %q, want △3四歩", j.Facts.OpponentBest)
	}
}

// 이름이 붙는 카테고리는 **탐색을 더 걸지 않는다.** 문장이 수를 안 적으므로 갈릴 자리가
// 없고(explain.Facts.used), 개입마다 탐색을 하나 더 무는 것은 그만큼 사람을 기다리게 한다.
func TestNamedCategorySkipsTheCardSearch(t *testing.T) {
	stub := &pvStub{single: afterK1(), multi: map[string]usi.SearchResult{}}
	a := &engineAnalyst{search: stub, depth: JudgeDepth, level: intervene.Beginner}

	// 角을 3三에 던진다 — 그냥 잡히므로 `hangs_piece` 다.
	j, err := a.Judge(t.Context(), shogi.StartSFEN, thrownBishopMoves, len(thrownBishopMoves))
	if err != nil {
		t.Fatalf("판정: %v", err)
	}
	if j.Verdict.Category != intervene.CategoryHangsPiece {
		t.Fatalf("카테고리 = %s, want hangs_piece", j.Verdict.Category)
	}
	if len(stub.asked) != 0 {
		t.Errorf("MultiPV를 %d번 걸었다: %v", len(stub.asked), stub.asked)
	}
}
