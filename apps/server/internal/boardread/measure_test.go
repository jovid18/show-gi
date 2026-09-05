package boardread

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// 판독이 얼마나 맞는지를 그림 여러 장으로 잰다. 근거와 지금까지의 값은 journal §129.
//
//	SHOWGI_MEASURE=1 SHOWGI_OPENAI_KEY=… go test ./internal/boardread/ -run MeasureBoardRead -v
//
// **그림을 레포에 커밋하지 않는다.** 앱 화면과 방송 캡처는 남의 것이고 이 레포는
// 퍼블릭이다 — floodgate 기보와 같은 규약이고, 경로가 `.gitignore` 에 있다.
//
// **표를 고치지 않는다.** 임계치도 걸지 않는다 — 어긋나면 문장으로 말하고 사람이 저널의
// 표를 옮긴다(handicap 의 TestMeasureBaseline 과 같은 판단). 자동으로 통과선을 두면
// 모델이나 프롬프트가 흔들릴 때 그 선이 조용히 따라 움직인다.
//
// 재는 것이 둘이다.
//
//   - **룰 검산의 사유 수.** 라벨이 없어도 나온다 — 실물 한 판은 언제나 40장이고
//     성립하는 국면이라, 사유가 하나라도 있으면 그 판독은 틀렸다.
//   - **칸 단위 정확도.** 그림 옆에 `<이름>.sfen` 을 두면 칸 81개와 駒台를 맞춰 본다.
//     그 파일은 확인 화면에서 판을 고친 뒤 주소의 `s=` 를 그대로 붙여 만들면 된다 —
//     이 기능 자체가 라벨을 만드는 도구다.

// measureDir 은 그림을 두는 곳이다. 환경변수로 덮을 수 있다.
const measureDir = "testdata/images"

// measureTimeout 은 한 장에 주는 시한이다. Client 의 것보다 넉넉하다 — 여기서 끊기면
// 그 장이 표에서 빠지고, 표본이 조용히 줄어드는 것이 가장 나쁘다.
const measureTimeout = 3 * time.Minute

// boardReadScore 는 그림 한 장의 결과다.
type boardReadScore struct {
	name string
	err  error
	// Faults 는 룰 검산이 짚은 사유 수다. 0이 아니면 그 판독은 틀렸다.
	faults []shogi.PositionFault
	// Short 는 모자란 말의 총합이다. 실물 한 판은 40장이라 이것도 0이어야 한다.
	short int
	// Squares 는 라벨과 맞은 칸 수다. 라벨이 없으면 -1.
	squares int
	// Missed 는 틀린 칸이다. 어느 종류를 어느 종류로 읽는지가 다음에 무엇을 고칠지를
	// 정하므로, 수만 세면 표를 보고도 할 일을 못 고른다.
	missed []string
	// Hands 는 라벨과 駒台가 맞는가다. 라벨이 없으면 이 값을 안 본다.
	hands  bool
	tokens int
}

func TestMeasureBoardRead(t *testing.T) {
	if os.Getenv("SHOWGI_MEASURE") == "" {
		t.Skip("SHOWGI_MEASURE 미설정")
	}
	key := os.Getenv("SHOWGI_OPENAI_KEY")
	if key == "" {
		t.Skip("no SHOWGI_OPENAI_KEY")
	}

	dir := os.Getenv("SHOWGI_BOARD_IMAGES")
	if dir == "" {
		dir = measureDir
	}
	images, err := boardImages(dir)
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	if len(images) == 0 {
		t.Skipf("%s 에 그림이 없다 — 절차는 apps/server/README.md", dir)
	}

	c := New(key, os.Getenv("SHOWGI_BOARDREAD_MODEL"))
	t.Logf("model=%s  images=%d  dir=%s", c.Model(), len(images), dir)

	scores := make([]boardReadScore, 0, len(images))
	for _, path := range images {
		// 한 장씩 순서대로 부른다. 병렬로 부르면 시간당 몫에 그만큼 빨리 닿고
		// (서버의 maxBoardReadsPerHour), 실패가 한 장의 것인지 벽의 것인지 흐려진다.
		scores = append(scores, measureOne(t, c, path))
	}

	reportBoardRead(t, scores)
}

// measureOne 은 그림 한 장을 읽고 채점한다.
func measureOne(t *testing.T, c *Client, path string) boardReadScore {
	t.Helper()

	score := boardReadScore{name: filepath.Base(path), squares: -1}

	image, err := os.ReadFile(path)
	if err != nil {
		score.err = err
		return score
	}

	ctx, cancel := context.WithTimeout(t.Context(), measureTimeout)
	defer cancel()

	got, err := c.Read(ctx, image)
	if err != nil {
		score.err = err
		return score
	}
	score.tokens = got.Tokens

	pos, err := shogi.ParseSFEN(got.SFEN)
	if err != nil {
		// 이 계층이 만든 SFEN 을 룰 엔진이 못 읽으면 sfenOf 의 버그다.
		score.err = fmt.Errorf("the transcription does not parse: %w", err)
		return score
	}
	score.faults = pos.Faults()
	for _, n := range pos.InventoryShortage() {
		score.short += n
	}

	// 라벨이 있으면 칸까지 맞춰 본다. 없으면 사유 수만으로 본다.
	want, ok := labelFor(t, path)
	if !ok {
		return score
	}
	score.squares, score.hands, score.missed = compare(want, pos)
	return score
}

// labelFor 는 그림 옆의 `<이름>.sfen` 을 읽는다. 없으면 두 번째 값이 거짓이다.
func labelFor(t *testing.T, path string) (shogi.Position, bool) {
	t.Helper()

	label := strings.TrimSuffix(path, filepath.Ext(path)) + ".sfen"
	raw, err := os.ReadFile(label)
	if err != nil {
		return shogi.Position{}, false
	}
	pos, err := shogi.ParseSFEN(strings.TrimSpace(string(raw)))
	if err != nil {
		// 라벨이 깨진 것은 사람이 고칠 일이다. 조용히 넘기면 그 장이 「라벨 없음」으로
		// 세어지고 표가 실제보다 좋아 보인다.
		t.Errorf("%s: %v", filepath.Base(label), err)
		return shogi.Position{}, false
	}
	return pos, true
}

// compare 는 라벨과 판독을 맞춘다. 맞은 칸 수(81까지) · 駒台가 맞는가 · 틀린 칸이다.
//
// 手番을 안 본다. 사진이 말해 주지 않는 값이라 이 계층은 언제나 "b" 를 적고, 고르는
// 것은 사람이다(Result.SFEN).
func compare(want, got shogi.Position) (int, bool, []string) {
	same := 0
	var missed []string
	for sq := range want.Board {
		if want.Board[sq] == got.Board[sq] {
			same++
			continue
		}
		missed = append(missed, fmt.Sprintf("%s %s→%s",
			shogi.SquareJa(sq), pieceJa(want.Board[sq]), pieceJa(got.Board[sq])))
	}
	return same, want.Hands == got.Hands, missed
}

// pieceJa 는 駒 하나를 「▲銀」처럼 적는다. 빈 칸은 「空」이다.
//
// 편을 붙인다. 종류만 적으면 「銀→銀」이 나오는데, 그건 편을 뒤집어 읽은 자리이고
// 이 계층에서 가장 흔한 오독일 수 있다.
func pieceJa(p shogi.Piece) string {
	if p.Empty() {
		return "空"
	}
	mark := "▲"
	if p.Color() == shogi.White {
		mark = "△"
	}
	return mark + shogi.PieceJa(p.Type())
}

// boardImages 는 폴더의 그림을 이름 순으로 준다.
//
// 순서를 고정한다. 폴더가 주는 순서에 맡기면 회차마다 표의 줄이 섞이고, 「고쳐서
// 나아진 것」을 두 표를 나란히 놓고 읽을 수가 없다(floodgate 의 seed 와 같은 이유).
func boardImages(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if slices.Contains([]string{".png", ".jpg", ".jpeg", ".webp"}, ext) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

// reportBoardRead 는 표 하나와 한 줄 요약을 찍는다.
//
// 실패해도 t.Fatal 하지 않는다. 이 시험이 답하는 것은 「지금 얼마나 맞나」이고, 그 값을
// 못 읽게 만드는 것이 가장 나쁘다 — 한 장이 죽어도 나머지 표는 나와야 한다.
func reportBoardRead(t *testing.T, scores []boardReadScore) {
	t.Helper()

	var clean, labelled, exact, squares, total int
	t.Log("")
	t.Log("| 그림 | 사유 | 부족 | 맞은 칸 | 駒台 | 토큰 |")
	t.Log("| ---- | ---: | ---: | ------: | ---- | ---: |")
	for _, s := range scores {
		if s.err != nil {
			t.Logf("| %s | — | — | — | — | 실패: %v |", s.name, s.err)
			continue
		}
		total++
		if len(s.faults) == 0 && s.short == 0 {
			clean++
		}
		cells, hands := "—", "—"
		if s.squares >= 0 {
			labelled++
			squares += s.squares
			cells = fmt.Sprintf("%d/81", s.squares)
			hands = "✗"
			if s.hands {
				hands = "✓"
			}
			if s.squares == 81 && s.hands {
				exact++
			}
		}
		t.Logf("| %s | %d | %d | %s | %s | %d |", s.name, len(s.faults), s.short, cells, hands, s.tokens)
	}

	// 틀린 칸을 그림마다 풀어 적는다. 「무엇을 무엇으로 읽는가」가 표의 숫자보다 값지다.
	for _, s := range scores {
		if len(s.missed) == 0 {
			continue
		}
		t.Log("")
		t.Logf("  %s — %d칸", s.name, len(s.missed))
		for _, m := range s.missed {
			t.Logf("    %s", m)
		}
	}

	// 어느 짝으로 헷갈리는지를 모아 센다. 한 그림에서만 나는 것과 여러 그림에서 나는
	// 것을 갈라야, 고칠 값이 있는 자리를 고를 수 있다.
	pairs := map[string]int{}
	for _, s := range scores {
		for _, m := range s.missed {
			if i := strings.Index(m, " "); i >= 0 {
				pairs[m[i+1:]]++
			}
		}
	}
	if len(pairs) > 0 {
		t.Log("")
		t.Log("  헷갈리는 짝 (잦은 순):")
		keys := make([]string, 0, len(pairs))
		for k := range pairs {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if pairs[keys[i]] != pairs[keys[j]] {
				return pairs[keys[i]] > pairs[keys[j]]
			}
			return keys[i] < keys[j]
		})
		for _, k := range keys {
			if pairs[k] > 1 {
				t.Logf("    %s × %d", k, pairs[k])
			}
		}
	}

	// 사유를 한 번 더 풀어 적는다. 어느 종류가 잦은지가 다음에 무엇을 고칠지를 정한다.
	byReason := map[string]int{}
	for _, s := range scores {
		for _, f := range s.faults {
			byReason[f.Reason.String()]++
		}
	}
	if len(byReason) > 0 {
		t.Log("")
		reasons := make([]string, 0, len(byReason))
		for r := range byReason {
			reasons = append(reasons, r)
		}
		sort.Strings(reasons)
		for _, r := range reasons {
			t.Logf("  %s × %d", r, byReason[r])
		}
	}

	t.Log("")
	if total == 0 {
		t.Log("한 장도 못 읽었다.")
		return
	}
	t.Logf("성립하는 판: %d/%d", clean, total)
	if labelled > 0 {
		t.Logf("칸 정확도: %.1f%% (%d/%d) · 완전 일치 %d/%d", 100*float64(squares)/float64(labelled*81),
			squares, labelled*81, exact, labelled)
	} else {
		t.Log("라벨(<이름>.sfen)이 한 장도 없다 — 칸 정확도는 못 잰다")
	}
}
