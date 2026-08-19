package book

import (
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// TestOpeningMovesAreLegal 은 수순을 룰 엔진에 검증시킨다.
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

// quietMove 는 판을 흔들지 않는 사람 쪽 한 수다. 결정적으로 고른다 — 무작위면 실패한
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
// journal §30에서 정한 것이고, 빠뜨리면 퍼블릭 레포에서 출처 없는 인용이 된다.
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

// TestOpeningNeverHangsAPiece 는 합법성이 잡지 못하는 종류를 잡는다.
//
// 수순의 모든 수가 합법인데도 순서가 틀리면 駒를 공짜로 준다. 실제로 물렸다 — 矢倉의
// ▲6八銀이 7九를 비우면서 8八의 角을 받치는 것을 없앴고, 그때 대각선이 열려 있으면
// 상대가 그 자리에서 角을 잡는다(journal §48). 룰 엔진은 그 수가 합법이라고만 답한다.
//
// 사람 쪽이 자기 角길을 먼저 연다 — 그것이 이 위험을 만드는 유일한 조건이고,
// 초심자도 첫 몇 수에 그냥 두는 수다.
func TestOpeningNeverHangsAPiece(t *testing.T) {
	for _, o := range All() {
		for _, opp := range []shogi.Color{shogi.Black, shogi.White} {
			t.Run(o.ID+"/"+opp.String(), func(t *testing.T) {
				pos, err := shogi.ParseSFEN(shogi.StartSFEN)
				if err != nil {
					t.Fatal(err)
				}
				human := opp.Other()
				// 사람의 첫 수는 角길을 여는 歩다. 先手면 ▲7六歩, 後手면 △3四歩.
				opener := "7g7f"
				if human == shogi.White {
					opener = "3c3d"
				}
				opened := false

				want := o.Moves(opp)
				for i := 0; i < len(want); {
					if pos.Turn != opp {
						var m shogi.Move
						if !opened {
							if m, err = shogi.ParseUSIMove(opener); err != nil {
								t.Fatal(err)
							}
							if err := pos.ValidateMove(m); err != nil {
								t.Fatalf("角길 여는 수 %s 가 반칙이다: %v", opener, err)
							}
							opened = true
						} else {
							var ok bool
							if m, ok = quietMove(pos); !ok {
								t.Fatalf("%d번째: 사람 쪽에 둘 수가 없다", i)
							}
						}
						pos = pos.Apply(m)
						continue
					}

					m, err := shogi.ParseUSIMove(want[i])
					if err != nil {
						t.Fatal(err)
					}
					if err := pos.ValidateMove(m); err != nil {
						t.Fatalf("%d번째 수 %s 가 반칙이다: %v", i+1, want[i], err)
					}
					pos = pos.Apply(m)
					i++

					if sq, kind := hanging(pos, opp); sq >= 0 {
						t.Fatalf("%d번째 수 %s 뒤에 %s의 駒(type %d)가 공짜다\n국면: %s",
							i, want[i-1], shogi.SquareUSI(sq), int(kind), pos.SFEN())
					}
				}
			})
		}
	}
}

// hanging 은 c 의 駒 중 상대가 노리는데 아무도 받치지 않는 첫 칸이다. 없으면 -1.
//
// 歩는 안 본다 — 서로 마주 본 歩는 초반의 정상 상태이고, 그것까지 세면 어느 수순도 못 지나간다.
// 玉도 안 본다: 王手는 다른 이야기이고 합법성 검사가 이미 막는다.
func hanging(pos shogi.Position, c shogi.Color) (int, shogi.PieceType) {
	for sq := 0; sq < 81; sq++ {
		p := pos.Board[sq]
		if p.Empty() || p.Color() != c {
			continue
		}
		t := p.Type()
		if t.Base() == shogi.Pawn || t == shogi.King {
			continue
		}
		if pos.IsAttacked(sq, c.Other()) && pos.AttackCount(sq, c) == 0 {
			return sq, t
		}
	}
	return -1, 0
}
