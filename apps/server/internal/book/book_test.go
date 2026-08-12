package book

import (
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// TestOpeningMovesAreLegal 은 수순을 **룰 엔진에 검증시킨다.**
//
// 진형 수순은 사람이 손으로 적은 것이라 눈으로는 틀린 것이 안 보인다 — 슬라이드가 막히거나
// (▲6八飛는 玉이 들어오기 전이어야 한다) 가려던 칸이 자기 駒로 차 있는 종류다. 그리고 이
// 목록이 늘어날 때 다시 물릴 자리이기도 하다.
//
// 사람 쪽은 「방해하지 않는 수」를 룰 엔진이 뽑은 것에서 고른다(quietMove). 정석대로 받아주는
// 상대를 세우지 않는 이유는 북이 애초에 그것을 전제하지 않기 때문이다(package 주석).
func TestOpeningMovesAreLegal(t *testing.T) {
	for _, o := range All() {
		for _, opp := range []shogi.Color{shogi.Black, shogi.White} {
			t.Run(o.ID+"/"+opp.String(), func(t *testing.T) {
				pos, err := shogi.ParseSFEN(shogi.StartSFEN)
				if err != nil {
					t.Fatalf("start sfen: %v", err)
				}

				want := o.Moves(opp)
				if len(want) == 0 {
					t.Fatal("수순이 비어 있다")
				}

				for i := 0; i < len(want); {
					if pos.Turn != opp {
						m, ok := quietMove(pos)
						if !ok {
							t.Fatalf("%d手目: 사람 쪽에 둘 수가 없다", i)
						}
						pos = pos.Apply(m)
						continue
					}

					m, err := shogi.ParseUSIMove(want[i])
					if err != nil {
						t.Fatalf("%s: USI를 못 읽는다: %v", want[i], err)
					}
					if err := pos.ValidateMove(m); err != nil {
						t.Fatalf("%d번째 수 %s 가 반칙이다: %v\n국면: %s", i+1, want[i], err, pos.SFEN())
					}
					pos = pos.Apply(m)
					i++
				}
			})
		}
	}
}

// quietMove 는 판을 흔들지 않는 사람 쪽 한 수다. **결정적으로 고른다** — 무작위면 실패한
// 회차를 다시 못 만든다.
//
// 자기 진영의 歩를 한 칸 미는 것만 고른다. 잡거나 成 하는 수를 섞으면 북과 무관한 이유로
// 국면이 갈려서, 실패했을 때 수순이 틀린 것인지 이 함수가 이상한 것인지 알 수 없다.
func quietMove(pos shogi.Position) (shogi.Move, bool) {
	legal := pos.LegalMoves()
	for _, m := range legal {
		if m.IsDrop() || m.Promote {
			continue
		}
		if pos.Board[m.From].Type() != shogi.Pawn {
			continue
		}
		if !pos.Board[m.To].Empty() {
			continue // 잡는 수
		}
		return m, true
	}
	// 歩를 못 밀면(막혀 있으면) 아무 합법수나. 그래도 결정적이다.
	if len(legal) > 0 {
		return legal[0], true
	}
	return shogi.Move{}, false
}

// TestMirrorIsInvolution 은 두 번 돌리면 제자리라는 것이다. 이것이 깨지면 後手 수순 전체가
// 조용히 어긋난다 — Moves 가 그 변환 하나에 매여 있다.
func TestMirrorIsInvolution(t *testing.T) {
	for _, o := range All() {
		for _, m := range o.black {
			if got := mirror(mirror(m)); got != m {
				t.Errorf("%s: 두 번 돌렸는데 %s 다", m, got)
			}
			if mirror(m) == m {
				t.Errorf("%s: 돌아가지 않았다", m)
			}
		}
	}
}

func TestMirrorSquare(t *testing.T) {
	cases := map[string]string{"7g": "3c", "1a": "9i", "5e": "5e", "2h": "8b"}
	for in, want := range cases {
		got, ok := mirrorSquare(in)
		if !ok || got != want {
			t.Errorf("mirrorSquare(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "7", "0a", "7j", "abc"} {
		if _, ok := mirrorSquare(bad); ok {
			t.Errorf("mirrorSquare(%q) 가 통과했다", bad)
		}
	}
}

// TestMirrorKeepsSuffix 는 成 표시와 打 표기가 변환에서 살아남는지다. 지금 수순에는 둘 다
// 없지만, 없다는 이유로 안 지키면 뒤에 한 줄 넣는 사람이 물린다.
func TestMirrorKeepsSuffix(t *testing.T) {
	if got := mirror("7g7f+"); got != "3c3d+" {
		t.Errorf("成: %q", got)
	}
	if got := mirror("P*5e"); got != "P*5e" {
		t.Errorf("打: %q", got)
	}
	if got := mirror("B*2b"); got != "B*8h" {
		t.Errorf("打: %q", got)
	}
}

// TestOpeningMetadata 는 화면과 출처 규약을 지키는지다. 항목마다 URL을 박는 것은
// 06-status.md §30에서 정한 것이고, 빠뜨리면 퍼블릭 레포에서 출처 없는 인용이 된다.
func TestOpeningMetadata(t *testing.T) {
	seen := map[string]bool{}
	for _, o := range All() {
		if seen[o.ID] {
			t.Errorf("%s: id가 겹친다", o.ID)
		}
		seen[o.ID] = true
		if o.Name == "" || o.Note == "" {
			t.Errorf("%s: 화면 문구가 비어 있다", o.ID)
		}
		if !strings.HasPrefix(o.Source, "https://") {
			t.Errorf("%s: 출처가 없다", o.ID)
		}
		// 화면에 나가는 것은 일본어다(CLAUDE.md). 한글이 한 글자라도 섞이면 안 된다.
		for _, r := range o.Name + o.Note {
			if r >= 0xAC00 && r <= 0xD7A3 {
				t.Errorf("%s: 화면 문구에 한글이 있다", o.ID)
				break
			}
		}
	}
	if _, ok := Find(""); ok {
		t.Error("빈 id는 없는 것으로 답해야 한다")
	}
	if _, ok := Find("nope"); ok {
		t.Error("없는 id가 통과했다")
	}
}
