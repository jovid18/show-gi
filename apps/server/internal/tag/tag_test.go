package tag

import (
	"strings"
	"testing"
	"unicode"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// place 는 빈 판에 先手 좌표로 駒를 놓는다. 色을 넘기면 그 색의 진영으로 옮겨 놓는다.
//
// 판을 비운 채로 재는 이유는 **주변 駒가 판정에 끼어들지 않아야** 하기 때문이다.
// 실전 국면으로만 재면 어느 칸이 실제 조건인지 테스트가 말해주지 않는다.
func place(c shogi.Color, ss ...square) shogi.Position {
	var pos shogi.Position
	for _, s := range ss {
		pos.Board[squareFor(s, c)] = shogi.MakePiece(s.pt, c)
	}
	return pos
}

func shapeByCode(t *testing.T, code string) shape {
	t.Helper()
	for _, sh := range append(append([]shape{}, castles...), formations...) {
		if sh.tag.Code == code {
			return sh
		}
	}
	t.Fatalf("정의에 없는 코드: %s", code)
	return shape{}
}

func codes(tags []Tag) []string {
	out := make([]string, 0, len(tags))
	for _, tg := range tags {
		out = append(out, tg.Code)
	}
	return out
}

// 각 정의가 자기 좌표에서 실제로 뜨는지. 정의를 추가하면 여기가 자동으로 늘어난다.
func TestEveryShapeMatchesItsOwnSquares(t *testing.T) {
	for _, sh := range append(append([]shape{}, castles...), formations...) {
		pos := place(shogi.Black, sh.squares...)
		if !sh.matches(pos, shogi.Black) {
			t.Errorf("%s: 자기 좌표에서 안 뜬다", sh.tag.Code)
		}
	}
}

// **後手 미러.** 이 테스트가 없으면 後手 국면에서 태그가 조용히 안 뜬다 — 에러가
// 나지 않는 종류의 버그라 기계로만 잡힌다.
//
// 미러가 맞는지를 값으로도 못 박는다: 先手 玉2八의 거울은 後手 玉8二다.
func TestShapesMirrorForGote(t *testing.T) {
	for _, sh := range append(append([]shape{}, castles...), formations...) {
		pos := place(shogi.White, sh.squares...)
		if !sh.matches(pos, shogi.White) {
			t.Errorf("%s: 後手 진영에서 안 뜬다", sh.tag.Code)
		}
		// 先手 좌표 그대로 놓으면 後手 것으로 읽혀서는 안 된다.
		if sh.matches(place(shogi.Black, sh.squares...), shogi.White) {
			t.Errorf("%s: 先手 배치를 後手 것으로 읽는다", sh.tag.Code)
		}
	}

	if got, want := squareFor(square{2, 8, shogi.King}, shogi.White), shogi.SquareOf(8, 2); got != want {
		t.Errorf("玉2八의 거울 = %d, 8二 = %d", got, want)
	}
}

// **음성 테스트.** 필수 칸 하나를 비우면 안 떠야 한다. 판정이 느슨해지는 것을
// 이것만이 잡는다 — 느슨한 판정은 화면에 틀린 이름을 내보낸다.
func TestOneMissingSquareIsNotTheCastle(t *testing.T) {
	for _, sh := range castles {
		for i := range sh.squares {
			partial := append(append([]square{}, sh.squares[:i]...), sh.squares[i+1:]...)
			if sh.matches(place(shogi.Black, partial...), shogi.Black) {
				t.Errorf("%s: %d번째 칸이 비어도 뜬다", sh.tag.Code, i)
			}
		}
	}
}

// 같은 칸에 상대 駒가 있으면 내 囲い가 아니다.
func TestOpponentPiecesDoNotFormMyCastle(t *testing.T) {
	sh := shapeByCode(t, "hon_mino")
	var pos shogi.Position
	for _, s := range sh.squares {
		// 先手 좌표에 後手 駒를 놓는다.
		pos.Board[squareFor(s, shogi.Black)] = shogi.MakePiece(s.pt, shogi.White)
	}
	if got := Detect(pos, shogi.Black); len(got) != 0 {
		t.Errorf("상대 駒로 짜인 형태가 내 것으로 뜬다: %v", codes(got))
	}
}

// **구체성.** 本美濃가 성립하는 국면에서 片美濃라고 말하면 안 된다. 本美濃의 칸이
// 片美濃를 전부 포함하므로 둘 다 맞는데, 화면에는 더 구체적인 쪽이 나가야 한다.
func TestMoreSpecificCastleWins(t *testing.T) {
	hon := shapeByCode(t, "hon_mino")
	kata := shapeByCode(t, "kata_mino")

	pos := place(shogi.Black, hon.squares...)
	if !kata.matches(pos, shogi.Black) {
		t.Fatal("전제가 깨졌다: 本美濃 국면에서 片美濃가 안 맞는다")
	}
	got, ok := pick(castles, pos, shogi.Black)
	if !ok || got.Code != "hon_mino" {
		t.Errorf("本美濃 국면인데 %v 로 골랐다", got.Code)
	}
}

// 축마다 하나씩, 囲い가 먼저. 「四間飛車 + 本美濃囲い」는 한 국면의 정상 상태다.
func TestDetectReturnsOnePerAxisCastleFirst(t *testing.T) {
	ss := append(append([]square{}, shapeByCode(t, "hon_mino").squares...),
		shapeByCode(t, "shiken_bisha").squares...)
	got := Detect(place(shogi.Black, ss...), shogi.Black)

	if want := []string{"hon_mino", "shiken_bisha"}; len(got) != 2 ||
		got[0].Code != want[0] || got[1].Code != want[1] {
		t.Fatalf("%v 를 기대했는데 %v", want, codes(got))
	}
	if got[0].Kind != KindCastle || got[1].Kind != KindFormation {
		t.Errorf("축 순서가 어긋났다: %v", got)
	}
}

// **初期配置에서는 전법이 뜨지 않는다.** 居飛車를 정의에 넣으면 첫 수 전에 뜬다 —
// 플레이어가 아직 하지 않은 선택에 이름이 붙는다. 그래서 표에서 뺐고, 그 결정이
// 되돌려지면 이 테스트가 실패한다.
func TestStartPositionHasNoTags(t *testing.T) {
	pos := shogi.StartPosition()
	for _, c := range []shogi.Color{shogi.Black, shogi.White} {
		if got := Detect(pos, c); len(got) != 0 {
			t.Errorf("%v: 初期配置인데 %v 가 떴다", c, codes(got))
		}
	}
}

// **SFEN에서 끝까지.** 위 테스트들은 전부 squareFor 로 판을 만들어 재므로, 그 함수가
// 통째로 틀려도 자기 일관성만으로 다 통과한다. 손으로 적은 SFEN 하나가 그 구멍을 막는다
// — (筋, 段) 읽기가 룰 엔진의 좌표계와 어긋나면 여기만 실패한다.
//
// 8段: 9八~7八 빈칸, 6八飛, 5八金, 4八金, 3八銀, 2八玉, 1八 빈칸 = 본美濃 + 四間飛車.
func TestDetectsFromAHandWrittenSFEN(t *testing.T) {
	const sfen = "4k4/9/9/9/9/9/9/3RGGSK1/9 b - 1"

	pos, err := shogi.ParseSFEN(sfen)
	if err != nil {
		t.Fatalf("SFEN 파싱 실패: %v", err)
	}
	if got, want := pos.Board[shogi.SquareOf(2, 8)], shogi.MakePiece(shogi.King, shogi.Black); got != want {
		t.Fatalf("2八에 玉이 없다 — 좌표 읽기가 어긋났다 (got %v)", got)
	}

	got := Detect(pos, shogi.Black)
	if want := []string{"hon_mino", "shiken_bisha"}; len(got) != 2 ||
		got[0].Code != want[0] || got[1].Code != want[1] {
		t.Fatalf("%v 를 기대했는데 %v", want, codes(got))
	}
}

// 화면에 나가는 문자열에 한글이 없는지. 사람 눈으로 지키면 결국 샌다(CLAUDE.md).
func TestNoHangulInNames(t *testing.T) {
	for _, tg := range All() {
		for _, r := range tg.NameJa {
			if unicode.Is(unicode.Hangul, r) {
				t.Errorf("%s: 화면 문자열에 한글이 있다: %q", tg.Code, tg.NameJa)
				break
			}
		}
	}
}

// 코드는 검색 키다 — 겹치면 코퍼스 항목이 조용히 다른 태그에 붙는다.
func TestCodesAreUniqueAndNamesFilled(t *testing.T) {
	seen := map[string]bool{}
	for _, tg := range All() {
		if seen[tg.Code] {
			t.Errorf("코드가 겹친다: %s", tg.Code)
		}
		seen[tg.Code] = true

		if tg.Code == "" || tg.NameJa == "" {
			t.Errorf("빈 칸이 있다: %+v", tg)
		}
		if strings.ContainsAny(tg.Code, " -") || strings.ToLower(tg.Code) != tg.Code {
			t.Errorf("%s: 코드는 소문자 snake_case 여야 한다", tg.Code)
		}
	}
}

// 囲い 좌표는 옮겨온 것이므로 출처가 있어야 한다. 없으면 지어낸 것과 구별되지 않는다.
func TestCastlesCarryTheirSource(t *testing.T) {
	for _, sh := range castles {
		if !strings.HasPrefix(sh.source, "https://") {
			t.Errorf("%s: 좌표 출처가 없다", sh.tag.Code)
		}
		if SourceOf(sh.tag.Code) != sh.source {
			t.Errorf("%s: SourceOf 가 다른 값을 준다", sh.tag.Code)
		}
	}
}
