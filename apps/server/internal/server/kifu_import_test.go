package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/kifu"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

const sampleKIF = `先手：わたし
後手：あいて
手合割：平手
   1 ７六歩(77)
   2 ３四歩(33)
   3 ２六歩(27)
   4 ８四歩(83)
   5 投了
`

// games.result 는 주인 관점이다. 안 뒤집으면 後手로 둔 판의 승패가 통째로 반대가 된다.
func TestImportedResultIsFromTheOwnersSide(t *testing.T) {
	for _, tc := range []struct {
		name   string
		kifu   kifu.GameResult
		color  string
		chosen string
		want   store.GameResult
		ok     bool
	}{
		{"sente won, I was sente", kifu.ResultSenteWin, "b", "", store.ResultWin, true},
		{"sente won, I was gote", kifu.ResultSenteWin, "w", "", store.ResultLoss, true},
		{"gote won, I was gote", kifu.ResultGoteWin, "w", "", store.ResultWin, true},
		{"gote won, I was sente", kifu.ResultGoteWin, "b", "", store.ResultLoss, true},
		{"a draw is a draw", kifu.ResultDraw, "w", "", store.ResultDraw, true},
		// 기보가 말하면 그쪽이 이긴다 — 사람이 자기 승패를 잘못 골라도 기록이 맞다.
		{"the record wins over the choice", kifu.ResultSenteWin, "b", "loss", store.ResultWin, true},
		{"unknown falls back to the choice", kifu.ResultUnknown, "b", "loss", store.ResultLoss, true},
		{"unknown with no choice is refused", kifu.ResultUnknown, "b", "", "", false},
		{"a bogus choice is refused", kifu.ResultUnknown, "b", "abandoned", "", false},
	} {
		got, ok := importedResultOf(tc.kifu, tc.color, tc.chosen)
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

// 미리보기는 앞뒤를 같이 보여 준다. 앞만 보여 주면 「뒤가 잘렸는가」를 사람이 알 수 없고,
// 그것이 취해 오기에서 가장 흔한 오류다.
func TestPreviewShowsBothEnds(t *testing.T) {
	g, notation, err := kifu.Read(sampleKIF)
	if err != nil {
		t.Fatal(err)
	}
	got := previewOf(g, notation)
	if got.Plies != 4 {
		t.Errorf("Plies = %d, want 4", got.Plies)
	}
	if got.Sente != "わたし" || got.Gote != "あいて" {
		t.Errorf("names = %q / %q", got.Sente, got.Gote)
	}
	// 4手 뒤는 先手 차례다. 投了는 던지는 쪽의 手番에 적히므로 이긴 쪽은 後手다.
	if got.Result != "gote" {
		t.Errorf("Result = %q, want gote (投了 on sente's turn)", got.Result)
	}
	if got.Transcribed {
		t.Error("Transcribed = true for a kifu a deterministic parser read")
	}
	// 4手뿐이라 앞뒤로 안 자른다.
	if len(got.Head) != 4 || len(got.Tail) != 0 {
		t.Fatalf("head %v tail %v", got.Head, got.Tail)
	}
	if !strings.HasPrefix(got.Head[0], "▲") {
		t.Errorf("head[0] = %q, want 棋譜 notation", got.Head[0])
	}
}

func TestPreviewCutsALongGame(t *testing.T) {
	var b strings.Builder
	b.WriteString("手合割：平手\n")
	for i, m := range []string{
		"７六歩(77)", "３四歩(33)", "２六歩(27)", "８四歩(83)", "２五歩(26)", "８五歩(84)",
		"７八金(69)", "３二金(41)", "２四歩(25)", "同　歩(23)", "同　飛(28)", "２三歩打",
	} {
		fmt.Fprintf(&b, "%4d %s\n", i+1, m)
	}
	g, notation, err := kifu.Read(b.String())
	if err != nil {
		t.Fatal(err)
	}
	got := previewOf(g, notation)
	if len(got.Head) != importPreviewHead || len(got.Tail) != importPreviewTail {
		t.Fatalf("head %d tail %d, want %d and %d", len(got.Head), len(got.Tail), importPreviewHead, importPreviewTail)
	}
}

// 익명은 401 이다. 익명끼리는 구별할 수단이 없어서 「누구의 기보인가」에 답할 수가 없다.
func TestImportNeedsALogin(t *testing.T) {
	h := &kifuHandler{auth: &authHandler{}}
	for _, path := range []string{"/api/kifu/parse", "/api/kifu/import"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"text":"x"}`))
		if path == "/api/kifu/parse" {
			h.parse(w, r)
		} else {
			h.create(w, r)
		}
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", path, w.Code)
		}
	}
}

// 읽은 手数를 말한다. 「読み取れませんでした」만으로는 사람이 어디를 고쳐야 하는지 모른다.
func TestUnreadableMoveSaysWhichPly(t *testing.T) {
	w := httptest.NewRecorder()
	_, _, err := kifu.Read("▲7六歩 △3四歩 ▲9九玉")
	writeImportError(w, err)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var body struct {
		Error   string `json:"error"`
		Ply     int    `json:"ply"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error != "move" || body.Ply != 3 {
		t.Errorf("body = %+v", body)
	}
	if !strings.Contains(body.Message, "3手目") {
		t.Errorf("message = %q, want it to name the ply", body.Message)
	}
}

// 취해 온 판이 사후 분석을 지나면 평가치와 悪手 줄이 같이 채워지고, 段級에 쌓인다.
// 사람이 정한 것이 「전부 합친다」다(journal §126).
//
//	SHOWGI_TEST_DATABASE_URL=postgres://showgi:showgi@localhost:5432/showgi go test ./internal/server/
func TestImportedGameGetsEvalsAndBlunders(t *testing.T) {
	st := testStore(t)
	userID, err := st.UpsertUser(t.Context(), "test", "import-"+time.Now().Format("150405.000000000"), "テスト")
	if err != nil {
		t.Fatal(err)
	}

	// 段級의 창이 21~60手라(skill.AnchorFromPly) 짧은 판으로는 프로파일이 안 움직인다.
	g, notation, err := kifu.Read(shuffleGameUSI(32))
	if err != nil {
		t.Fatal(err)
	}
	h := &kifuHandler{store: st}
	gameID, err := h.save(t.Context(), userID, "b", string(notation), g, store.ResultWin)
	if err != nil {
		t.Fatal(err)
	}

	// 홀수 手(사람의 수)만 낙폭이 크게 나오도록 갈라 둔다.
	a := analyzerFor(st, func() game.Analyst {
		return stubAnalyst{lossOdd: 0.9, lossEven: 0, blunder: true}
	})
	a.level = intervene.Beginner

	if got := a.analyze(t.Context(), importKey(gameID), a.seatsOf(t.Context(), importKey(gameID))); got != "done" {
		t.Fatalf("analyze = %q, want done", got)
	}

	rec, err := st.GameRecordAnyOwner(t.Context(), gameID)
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Imported {
		t.Error("Imported = false for a game that was imported")
	}
	for _, m := range rec.Moves {
		if m.EvalCp == nil {
			t.Errorf("ply %d has no eval", m.Ply)
		}
	}
	if len(rec.Interventions) == 0 {
		t.Fatal("no blunder was recorded for a game full of them")
	}
	for _, iv := range rec.Interventions {
		// 홀수 手가 사람의 수다(先手로 둔 판).
		if iv.Ply%2 == 0 {
			t.Errorf("ply %d is the opponent's move; it must not count as this player's blunder", iv.Ply)
		}
		if iv.Kind != string(intervene.KindBlunder) {
			t.Errorf("ply %d kind = %q", iv.Ply, iv.Kind)
		}
		// 아무도 안 막았다. 그 칸을 채우면 없던 일을 있었다고 말하는 것이다.
		if iv.RetractedUSI != "" {
			t.Errorf("ply %d has a retracted move: %q", iv.Ply, iv.RetractedUSI)
		}
	}

	if _, ok, err := st.SkillProfile(t.Context(), userID); err != nil {
		t.Fatal(err)
	} else if !ok {
		t.Error("nothing was added to the skill profile; imported games are meant to count")
	}
}

// 되짚기가 「解析しています」를 그리려면 취해 온 판도 「분석 중」으로 보여야 한다.
// games.match_id 로 조인하는 쪽에는 안 걸린다.
//
//	SHOWGI_TEST_DATABASE_URL=postgres://showgi:showgi@localhost:5432/showgi go test ./internal/server/
func TestImportedGameShowsAsAnalyzing(t *testing.T) {
	st := testStore(t)
	userID, err := st.UpsertUser(t.Context(), "test", "analyzing-"+time.Now().Format("150405.000000000"), "テスト")
	if err != nil {
		t.Fatal(err)
	}
	gameID, err := st.CreateImportedGame(t.Context(), userID, "b", "", string(kifu.NotationKIF))
	if err != nil {
		t.Fatal(err)
	}

	a := analyzerFor(st, func() game.Analyst { return stubAnalyst{} })
	if a.analyzing(t.Context(), gameID) {
		t.Fatal("said it was being analyzed before it was queued")
	}
	if err := a.enqueueImport(t.Context(), gameID, "", []string{"7g7f", "3c3d"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		a.dropJob(t.Context(), importKey(gameID))
		a.discard(t.Context(), importKey(gameID))
	})
	if !a.analyzing(t.Context(), gameID) {
		t.Error("a queued import does not show as being analyzed")
	}

	// 手가 하나씩 다 서야 워커들이 병렬로 잰다.
	if n, err := st.CountMeasuredAnalysisPlies(t.Context(), importKey(gameID)); err != nil {
		t.Fatal(err)
	} else if n != 0 {
		t.Errorf("measured = %d before anything ran", n)
	}
}

// shuffleGameUSI 는 합법이면서 길기만 한 수순이다. 飛車를 좌우로 옮기는 것뿐이라
// 국면이 거의 안 바뀌고, 그래서 段級의 창(21~60手)을 넘기는 데만 쓴다.
func shuffleGameUSI(plies int) string {
	moves := []string{"7g7f", "3c3d"}
	shuffle := []string{"2h3h", "8b7b", "3h2h", "7b8b"}
	for i := 0; len(moves) < plies; i++ {
		moves = append(moves, shuffle[i%len(shuffle)])
	}
	return "position startpos moves " + strings.Join(moves, " ")
}

// 하루 몫은 판이 만들어지는 것을 센다. 옮겨 적는 일은 그 전에 일어나므로, 읽기만
// 반복하는 사람은 그 벽에 영영 안 닿으면서 토큰을 계속 쓴다(journal §126).
func TestTranscribeBudgetCapsTheHour(t *testing.T) {
	now := time.Now()
	b := newTranscribeBudget()
	b.now = func() time.Time { return now }

	for i := range maxTranscribesPerHour {
		if !b.take(1) {
			t.Fatalf("call %d was refused inside the budget", i+1)
		}
	}
	if b.take(1) {
		t.Error("the call past the budget went through")
	}
	// 사람마다 따로 센다. 한 사람이 다 쓰면 다른 사람이 못 읽는 것은 벽이 아니라 고장이다.
	if !b.take(2) {
		t.Error("another person was refused because of somebody else's calls")
	}

	// 창이 미끄러진다. 정시 초기화면 창이 바뀌는 순간에 두 배가 지나간다.
	now = now.Add(time.Hour + time.Second)
	if !b.take(1) {
		t.Error("the budget never came back after the window passed")
	}
	// 창 밖으로 나간 사람은 표에서 지운다 — 안 지우면 이 맵이 로그인한 사람 수만큼 자란다.
	if _, still := b.hits[2]; still {
		t.Error("a person whose calls all fell out of the window is still held")
	}
}

// nil 예산은 막지 않는다. 분석기 없이 만든 테스트용 핸들러가 그 모양이다.
func TestNilBudgetLetsEverythingThrough(t *testing.T) {
	var b *transcribeBudget
	for range maxTranscribesPerHour + 5 {
		if !b.take(1) {
			t.Fatal("a nil budget refused a call")
		}
	}
}

// 넘쳐서 끊긴 몸통은 「못 읽었다」가 아니다. 같은 문장을 주면 사람이 형식을 고치려 든다.
func TestAnOversizedBodySaysItIsTooLarge(t *testing.T) {
	w := httptest.NewRecorder()
	body := `{"text":"` + strings.Repeat("x", importBodyMax+1<<10) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/api/kifu/parse", strings.NewReader(body))
	if _, ok := decodeImport(w, r); ok {
		t.Fatal("accepted a body over the limit")
	}
	var got struct{ Error string }
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Error != "too_large" {
		t.Errorf("error = %q, want too_large", got.Error)
	}
}

// 千日手는 shogi.ValidateMove 가 안 막는다. 합법 수순만으로 몇 천 手를 적을 수 있고,
// 그 판이 手数만큼의 엔진 판정을 줄에 세운다 — 정규화 계층의 벽은 그 길을 안 막는다.
func TestADeterministicallyReadKifuIsStillCapped(t *testing.T) {
	h := &kifuHandler{}
	long := shuffleGameUSI(maxImportPlies + 2)

	// 먼저 그 수순이 실제로 읽히는지 본다. 안 읽히면 이 시험이 벽이 아니라 파서를 재게 된다.
	g, _, err := kifu.Read(long)
	if err != nil {
		t.Fatalf("the sample does not parse, so this proves nothing: %v", err)
	}
	if len(g.Moves) <= maxImportPlies {
		t.Fatalf("the sample is %d plies, not over the cap", len(g.Moves))
	}

	if _, _, err := h.read(t.Context(), 1, long); !errors.Is(err, errTooManyPlies) {
		t.Fatalf("err = %v, want errTooManyPlies", err)
	}

	w := httptest.NewRecorder()
	writeImportError(w, errTooManyPlies)
	var got struct{ Error string }
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Error != "too_many_plies" {
		t.Errorf("error = %q, want too_many_plies", got.Error)
	}
}
