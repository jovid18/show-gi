package shogi

import (
	"strings"
	"testing"
)

// 성립하는 국면에서는 사유가 하나도 안 나와야 한다. 여기가 새면 확인 화면이 정상인
// 판을 거절하고, 그건 기능이 통째로 안 되는 것과 같다.
func TestFaultsAcceptsRealPositions(t *testing.T) {
	cases := map[string]string{
		"平手 초기 국면": "lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1",
		// 玉 둘과 歩 한 장뿐인 판. 말이 부족한 것은 사유가 아니다.
		"말이 빠진 국면":  "4k4/9/9/9/9/9/4P4/9/4K4 b - 1",
		"持ち駒가 있는 판": "4k4/9/9/9/9/9/9/9/4K4 b RBGSNLPrb 1",
		// と金은 같은 筋에 둘이 있어도 二歩가 아니다.
		"と金과 歩가 같은 筋": "4k4/9/9/9/4+P4/9/4P4/9/4K4 b - 1",
		// 王手를 받고 있는 쪽이 手番이면 정상이다. 飛가 玉과 같은 筋에 서 있다.
		"수번 쪽이 왕수를 받고 있다": "4k4/9/9/9/4R4/9/9/9/3K5 w - 1",
	}
	for name, sfen := range cases {
		t.Run(name, func(t *testing.T) {
			pos, err := ParseSFEN(sfen)
			if err != nil {
				t.Fatalf("ParseSFEN: %v", err)
			}
			if got := pos.Faults(); len(got) != 0 {
				t.Fatalf("Faults() = %v, want none", faultErrors(got))
			}
		})
	}
}

func TestFaultsCatchesIllegalPositions(t *testing.T) {
	cases := []struct {
		name string
		sfen string
		want PositionReason
	}{
		{
			// 歩가 19장이다. 사진에서 と金을 歩로 읽으면 이 모양이 된다.
			name: "말이 한 벌을 넘는다",
			sfen: "lnsgkgsnl/1r5b1/ppppppppp/9/4P4/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1",
			want: PositionPieceExcess,
		},
		{
			name: "玉이 없다",
			sfen: "4k4/9/9/9/9/9/9/9/9 b - 1",
			want: PositionKingCount,
		},
		{
			name: "玉이 둘이다",
			sfen: "4k4/9/9/9/9/9/9/3K1K3/9 b - 1",
			want: PositionKingCount,
		},
		{
			name: "같은 筋에 歩가 둘이다",
			sfen: "4k4/9/9/9/4P4/9/4P4/9/4K4 b - 1",
			want: PositionNifu,
		},
		{
			// 先手의 歩가 段一에 있다. 성했어야 하는 자리다.
			name: "歩가 갈 곳이 없는 단에 있다",
			sfen: "4k3P/9/9/9/9/9/9/9/4K4 b - 1",
			want: PositionDeadPiece,
		},
		{
			// 先手의 桂가 段二에 있다. 뛸 자리가 판 밖이다.
			name: "桂가 갈 곳이 없는 단에 있다",
			sfen: "4k4/8N/9/9/9/9/9/9/4K4 b - 1",
			want: PositionDeadPiece,
		},
		{
			// 後手의 香가 段九에 있다. 부호를 뒤집는 자리라 따로 본다.
			name: "後手의 香가 갈 곳이 없는 단에 있다",
			sfen: "4k4/9/9/9/9/9/9/9/4K3l b - 1",
			want: PositionDeadPiece,
		},
		{
			// 手番이 아닌 쪽이 王手를 받고 있다. 手番을 잘못 고른 자리가 여기로 온다.
			name: "수번이 아닌 쪽이 왕수를 받고 있다",
			sfen: "4k4/9/9/9/4R4/9/9/9/3K5 b - 1",
			want: PositionCheckIgnored,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pos, err := ParseSFEN(tc.sfen)
			if err != nil {
				t.Fatalf("ParseSFEN: %v", err)
			}
			got := pos.Faults()
			if len(got) == 0 {
				t.Fatalf("Faults() = none, want %s", tc.want)
			}
			if !hasReason(got, tc.want) {
				t.Fatalf("Faults() = %v, want %s", faultErrors(got), tc.want)
			}
		})
	}
}

// 잘못 읽은 사진은 여러 자리가 함께 틀린다. 한 번에 하나만 말하면 사람이 고치고 다시
// 누르기를 사유 수만큼 반복한다.
func TestFaultsReportsEveryProblemAtOnce(t *testing.T) {
	// 二歩 두 筋. 각각 둘째 장이 사유가 된다.
	pos, err := ParseSFEN("4k4/9/9/9/2P1P4/9/2P1P4/9/4K4 b - 1")
	if err != nil {
		t.Fatalf("ParseSFEN: %v", err)
	}
	got := pos.Faults()
	if len(got) != 2 {
		t.Fatalf("Faults() = %v, want two nifu faults", faultErrors(got))
	}
	for _, f := range got {
		if f.Reason != PositionNifu {
			t.Fatalf("Faults() = %v, want only nifu", faultErrors(got))
		}
		if f.Square < 0 {
			t.Fatalf("nifu fault has no square: %v", f)
		}
	}
}

// 玉이 없는 판에서 王手 검사가 조용히 통과하면 안 된다 — KingSquare 가 -1을 주고
// InCheck 이 언제나 거짓이라, 그 거짓이 「王手가 없다」로 읽힌다.
func TestFaultsDoesNotAskAboutCheckWithoutAKing(t *testing.T) {
	pos, err := ParseSFEN("9/9/9/9/9/9/9/9/4KR3 b - 1")
	if err != nil {
		t.Fatalf("ParseSFEN: %v", err)
	}
	got := pos.Faults()
	if hasReason(got, PositionCheckIgnored) {
		t.Fatalf("Faults() = %v, want no check fault when a king is missing", faultErrors(got))
	}
	if !hasReason(got, PositionKingCount) {
		t.Fatalf("Faults() = %v, want a king-count fault", faultErrors(got))
	}
}

// 사유가 그대로 화면에 나간다. 한글이 한 글자라도 섞이면 그 자리에서 「번역이 덜 된
// 앱」이 된다(CLAUDE.md).
func TestFaultMessagesAreJapanese(t *testing.T) {
	reasons := []PositionReason{
		PositionUnknown, PositionPieceExcess, PositionKingCount,
		PositionNifu, PositionDeadPiece, PositionCheckIgnored,
	}
	for _, r := range reasons {
		// 玉 수는 개수에 따라 문구가 갈린다. 둘 다 본다.
		for _, count := range []int{0, 2} {
			f := PositionFault{Reason: r, Square: 40, Type: Pawn, Count: count}
			msg := f.Message()
			if msg == "" {
				t.Fatalf("%s: empty message", r)
			}
			if hasHangul(msg) {
				t.Fatalf("%s: message reaches the user in Korean: %q", r, msg)
			}
		}
	}
}

// 실물 한 판은 언제나 40장이다. 사진에서 한 장을 놓친 것을 화면이 경고로 말한다.
func TestInventoryShortage(t *testing.T) {
	full, err := ParseSFEN("lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1")
	if err != nil {
		t.Fatalf("ParseSFEN: %v", err)
	}
	if got := full.InventoryShortage(); len(got) != 0 {
		t.Fatalf("InventoryShortage() = %v, want none for a full set", got)
	}

	// 歩 한 장을 뺀다. 그 한 장이 持ち駒에 있으면 다시 40장이다.
	short, err := ParseSFEN("lnsgkgsnl/1r5b1/1pppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1")
	if err != nil {
		t.Fatalf("ParseSFEN: %v", err)
	}
	if got := short.InventoryShortage()[Pawn]; got != 1 {
		t.Fatalf("InventoryShortage()[Pawn] = %d, want 1", got)
	}
	inHand, err := ParseSFEN("lnsgkgsnl/1r5b1/1pppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b P 1")
	if err != nil {
		t.Fatalf("ParseSFEN: %v", err)
	}
	if got := inHand.InventoryShortage(); len(got) != 0 {
		t.Fatalf("InventoryShortage() = %v, want none once the pawn is in hand", got)
	}
}

func hasReason(faults []PositionFault, want PositionReason) bool {
	for _, f := range faults {
		if f.Reason == want {
			return true
		}
	}
	return false
}

func faultErrors(faults []PositionFault) []string {
	out := make([]string, 0, len(faults))
	for _, f := range faults {
		out = append(out, f.Error())
	}
	return out
}

// 持ち駒 수가 Hands 의 int8 을 넘으면 조용히 음수가 된다. 그 판은 예전에 Faults 를
// 통과하면서 movegen 이 打을 만들어 냈고(`== 0` 만 본다), 엔진에는 다시 직렬화한
// 「1장」이 나갔다 — 룰 엔진과 엔진이 다른 판을 보게 된다. 셀프리뷰가 잡았다.
func TestParseSFENRefusesAHandThatCannotFit(t *testing.T) {
	for _, sfen := range []string{
		"9/9/9/9/4k4/9/9/9/4K4 b 200P 1",
		"9/9/9/9/4k4/9/9/9/4K4 b 41P 1",
		"9/9/9/9/4k4/9/9/9/4K4 w 128p 1",
	} {
		if _, err := ParseSFEN(sfen); err == nil {
			t.Errorf("ParseSFEN(%q) = nil error, want a refusal", sfen)
		}
	}
	// 한 벌을 넘는 것은 그대로 받는다. 거절이 아니라 사유로 말한다(InventoryExcess).
	pos, err := ParseSFEN("9/9/9/9/4k4/9/9/9/4K4 b 19P 1")
	if err != nil {
		t.Fatalf("19 pawns in hand should parse: %v", err)
	}
	if !hasReason(pos.Faults(), PositionPieceExcess) {
		t.Errorf("Faults() = %v, want a piece-excess fault", faultErrors(pos.Faults()))
	}
}

// 음수 持ち駒는 InventoryExcess 를 통과한다 — 합이 줄어들 뿐이라 「많다」로 안 걸린다.
// Apply 가 미검증 투입으로 음수를 만들 수 있으므로(그 함수 주석) 여기서 짚어야 한다.
func TestFaultsCatchesANegativeHand(t *testing.T) {
	pos, err := ParseSFEN("9/9/9/9/4k4/9/9/9/4K4 b - 1")
	if err != nil {
		t.Fatalf("ParseSFEN: %v", err)
	}
	pos.Hands[Black][Pawn] = -56

	if !hasReason(pos.Faults(), PositionHandNegative) {
		t.Fatalf("Faults() = %v, want a negative-hand fault", faultErrors(pos.Faults()))
	}
	if hasHangul(PositionFault{Reason: PositionHandNegative, Type: Pawn, Square: -1}.Message()) {
		t.Error("the message reaches the user in Korean")
	}
}

// 말 수 초과는 양쪽을 합쳐 센다. 편을 적으면 後手의 초과를 「先手」로 적는다.
func TestPieceExcessDoesNotClaimASide(t *testing.T) {
	pos, err := ParseSFEN("lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b 3p 1")
	if err != nil {
		t.Fatalf("ParseSFEN: %v", err)
	}
	for _, f := range pos.Faults() {
		if f.Reason != PositionPieceExcess {
			continue
		}
		if f.HasColor {
			t.Errorf("piece excess claims a side: %s", f.Error())
		}
		if strings.Contains(f.Error(), "sente") || strings.Contains(f.Error(), "gote") {
			t.Errorf("the log names a side: %s", f.Error())
		}
		return
	}
	t.Fatal("no piece-excess fault to check")
}
