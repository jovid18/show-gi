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
	for _, sh := range castles {
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
	for _, sh := range castles {
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
	for _, sh := range castles {
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
	if got := Detect(pos, nil, shogi.Black); len(got) != 0 {
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
//
// 축이 서로 **다른 입력**에서 나온다는 것도 여기서 못 박힌다 — 囲い는 국면, 전법은 수순.
func TestDetectReturnsOnePerAxisCastleFirst(t *testing.T) {
	pos := place(shogi.Black, shapeByCode(t, "hon_mino").squares...)
	got := Detect(pos, []string{"2h6h"}, shogi.Black) // 飛2八 → 6八

	if want := []string{"hon_mino", "shiken_bisha"}; len(got) != 2 ||
		got[0].Code != want[0] || got[1].Code != want[1] {
		t.Fatalf("%v 를 기대했는데 %v", want, codes(got))
	}
	if got[0].Kind != KindCastle || got[1].Kind != KindFormation {
		t.Errorf("축 순서가 어긋났다: %v", got)
	}
}

// **筋만 본다.** 段을 고정하면 飛를 올린 순간 이름이 꺼지는데, 전법은 그대로다.
func TestFormationSurvivesTheRookAdvancing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		moves []string
	}{
		{"振っただけ", []string{"2h6h"}},
		{"振って上がった", []string{"2h6h", "6h6e"}},
		{"振って下がった", []string{"2h6h", "6h6i"}},
		{"振って何度も動いた", []string{"2h6h", "6h6e", "6e6c"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DetectFormation(tc.moves, shogi.Black)
			if !ok || got.Code != "shiken_bisha" {
				t.Errorf("四間飛車를 기대했는데 %v (ok=%v)", got.Code, ok)
			}
		})
	}
}

// 筋마다 이름이 갈린다. 袖飛車·右四間飛車가 특별 취급 없이 같은 규칙에서 나온다.
func TestEachFileNamesItsFormation(t *testing.T) {
	for _, tc := range []struct {
		usi  string
		want string
	}{
		{"2h3h", "sode_bisha"},
		{"2h4h", "migi_shiken_bisha"},
		{"2h5h", "naka_bisha"},
		{"2h6h", "shiken_bisha"},
		{"2h7h", "sanken_bisha"},
		{"2h8h", "mukai_bisha"},
	} {
		got, ok := DetectFormation([]string{tc.usi}, shogi.Black)
		if !ok || got.Code != tc.want {
			t.Errorf("%s → %v (%s 기대)", tc.usi, got.Code, tc.want)
		}
	}
}

// 飛를 **筋 안에서** 움직인 것은 振ったのではない. 居飛車의 飛先の歩交換이 그렇다.
func TestMovingTheRookWithinItsFileIsNotASwing(t *testing.T) {
	if got, ok := DetectFormation([]string{"2h2f", "2f2d"}, shogi.Black); ok {
		t.Errorf("2八→2六→2四 는 振り飛車가 아닌데 %v 가 떴다", got.Code)
	}
}

// 振り直し는 그 판의 전법을 바꾸지 않는다 — **처음** 振った 筋이 이긴다.
func TestTheFirstSwingWins(t *testing.T) {
	got, ok := DetectFormation([]string{"2h6h", "6h7h"}, shogi.Black)
	if !ok || got.Code != "shiken_bisha" {
		t.Errorf("四間에서 三間으로 옮겼어도 四間飛車여야 하는데 %v", got.Code)
	}
}

// 後手도 같은 규칙에서 나와야 한다. 8二 → 4二 는 先手의 6筋에 해당한다.
func TestFormationMirrorsForGote(t *testing.T) {
	got, ok := DetectFormation([]string{"8b4b"}, shogi.White)
	if !ok || got.Code != "shiken_bisha" {
		t.Errorf("後手 8二→4二 는 四間飛車인데 %v (ok=%v)", got.Code, ok)
	}
	// 先手의 수를 後手로 재면 안 맞아야 한다 — 좇는 시작 칸이 다르다.
	if _, ok := DetectFormation([]string{"2h6h"}, shogi.White); ok {
		t.Error("先手의 수순을 後手 것으로 읽었다")
	}
}

// 居飛車는 **囲った 뒤에만** 뜬다. 振っていない은 初期配置에서도 참이라 그것만으로는
// 아직 아무 선택도 안 드러났다.
//
// 矢倉로 재는 이유가 있다 — 本美濃는 玉이 2八에 서므로 飛가 그 筋에 함께 있을 수 없고,
// 애초에 振り飛車의 囲い다. 「居飛車 + 矢倉」가 실제로 함께 나오는 짝이다.
func TestIbishaNeedsACastleFirst(t *testing.T) {
	rook := square{2, 8, shogi.Rook} // 初形의 飛. 振っていない

	bare := place(shogi.Black, rook) // 囲い이 없다
	if got := Detect(bare, []string{"7g7f"}, shogi.Black); len(got) != 0 {
		t.Errorf("囲い도 없는데 %v 가 떴다", codes(got))
	}

	ss := append(append([]square{}, shapeByCode(t, "kin_yagura").squares...), rook)
	got := Detect(place(shogi.Black, ss...), []string{"7g7f"}, shogi.Black)
	if want := []string{"kin_yagura", "ibisha"}; len(got) != 2 ||
		got[0].Code != want[0] || got[1].Code != want[1] {
		t.Fatalf("%v 를 기대했는데 %v", want, codes(got))
	}
}

// **수순이 없으면 居飛車라고 말하지 않는다.** `StartSFEN` 으로 중간부터 시작한 세션이
// 그렇다 — 振った 기록이 없다는 것이 振っていない는 뜻이 아니다.
//
// 판을 함께 보는 것이 그것을 막는다. 飛가 6筋에 있으면 수순이 비어 있어도 居飛車가 아니다.
func TestIbishaIsNotClaimedWhenTheHistoryIsMissing(t *testing.T) {
	ss := append(append([]square{}, shapeByCode(t, "kin_yagura").squares...),
		square{6, 8, shogi.Rook}) // 이미 振ってある 국면

	if got := Detect(place(shogi.Black, ss...), nil, shogi.Black); len(got) != 1 {
		t.Errorf("囲い 하나만 기대했는데 %v", codes(got))
	}
}

// 打는 좇던 칸과 안 맞는다. 飛가 잡혔다가 6筋에 打たれても 四間飛車가 아니다.
func TestADroppedRookIsNotASwing(t *testing.T) {
	if got, ok := DetectFormation([]string{"R*6h"}, shogi.Black); ok {
		t.Errorf("打으로 전법이 붙었다: %v", got.Code)
	}
}

// **初期配置에서는 아무것도 뜨지 않는다.** 居飛車가 「飛を振っていない」だけ로 성립하면
// 첫 수 전에 뜬다 — 플레이어가 아직 하지 않은 선택에 이름이 붙는다. 囲い을 함께
// 요구하는 것이 그것을 막고, 그 조건이 풀리면 이 테스트가 실패한다.
func TestStartPositionHasNoTags(t *testing.T) {
	pos := shogi.StartPosition()
	for _, c := range []shogi.Color{shogi.Black, shogi.White} {
		if got := Detect(pos, nil, c); len(got) != 0 {
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

	got := Detect(pos, []string{"2h6h"}, shogi.Black)
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
