package kifu

import (
	"errors"
	"strings"
	"testing"
)

// 将棋ウォーズ의 詰めバト 復習에서 「棋譜をコピー」한 글. 반각 공백으로 持駒를 가르고 「先手番」
// 줄이 있으며, 둬 본 수순이 뒤에 붙는다.
const warsTsume = `場所：将棋ウォーズ
持ち時間：3分切れ負け
手合割：平手
初期局面：詰めバト
後手の持駒：歩四
  ９ ８ ７ ６ ５ ４ ３ ２ １
+---------------------------+
|v香v角 ・ ・ ・ ・ ・v桂v香|一
| ・v金 ・ ・ ・ ・ ・ ・ ・|二
|v玉v銀 龍 桂 ・ ・ ・ ・v歩|三
| 桂 ・v歩 ・ 玉 銀v歩 金 ・|四
| ・v歩 ・ ・v歩 ・ ・v歩 ・|五
| 歩 ・ 歩 歩v馬 ・ ・ ・ ・|六
| ・ 金 ・ ・ ・ ・ 歩 ・ 歩|七
| ・ ・ ・ ・ ・ ・ ・v圭 ・|八
| 香v銀 ・ ・ ・ ・v飛 ・ 香|九
+---------------------------+
先手の持駒：歩三 銀 金
先手番
先手：jovid
先手段級：17級
後手：なし
手数----指手---------消費時間--
1 ８二龍(73)   ( 0:06/00:00:06)
2 ９四玉(93)   ( 0:00/00:00:00)
3 ９五銀打   ( 0:04/00:00:10)
4 投了
`

// ぴよ将棋의 詰将棋 「棋譜出力」. 전각 공백으로 가르고 끝에도 남으며, 手番 줄이 없다.
const piyoTsume = `# ----  ぴよ将棋 棋譜ファイル  ----
棋戦：Mate Problem 2026/10/10 High
戦型：
開始日時：2026/10/10 13:14:46
終了日時：
手合割：平手
後手の持駒：角　銀　桂
  ９ ８ ７ ６ ５ ４ ３ ２ １
+---------------------------+
| ・ ・ ・ ・v歩 ・ ・ ・v銀|一
| ・ ・ と ・ ・v玉 ・v飛v香|二
| ・ ・ ・ ・ ・ 桂v香 ・ ・|三
|v歩 ・v歩 ・ ・ ・v歩 歩v歩|四
| ・ ・ ・ 歩 ・ ・ ・ 香 ・|五
| ・ ・ ・ ・ 歩 ・ ・ ・ ・|六
| 歩 ・ ・vと ・ 歩 銀 銀 歩|七
| ・ ・ ・ ・ ・ ・ ・ ・ 香|八
|vと ・ ・ ・ ・ ・ 金 桂 玉|九
+---------------------------+
先手の持駒：飛　角　金三　桂　歩四
先手：Player
後手：DefSide
手数----指手---------消費時間--
   1 中断       ( 0:01/00:00:01)
まで0手で中断
`

func TestParseBODReadsBothApps(t *testing.T) {
	for _, c := range []struct{ name, text, want string }{
		{"wars", warsTsume, "lb5nl/1g7/ks+RN4p/N1p1KSpG1/1p2p2p1/P1PP+b4/1G4P1P/7+n1/Ls4r1L b GS3P4p 1"},
		{"piyo", piyoTsume, "4p3s/2+P2k1rl/5Nl2/p1p3pPp/3P3L1/4P4/P2+p1PSSP/8L/+p5GNK b RB3GN4Pbsn 1"},
	} {
		got, err := ParseBOD(c.text)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s:\n got  %s\n want %s", c.name, got, c.want)
		}
	}
}

// 「後手番」이 있으면 後手 차례다. 持駒 「なし」는 빈 값이다.
func TestParseBODTurnAndEmptyHands(t *testing.T) {
	text := strings.Replace(warsTsume, "先手番", "後手番", 1)
	text = strings.Replace(text, "先手の持駒：歩三 銀 金", "先手の持駒：なし", 1)
	text = strings.Replace(text, "後手の持駒：歩四", "後手の持駒：歩十八", 1)
	got, err := ParseBOD(text)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, " w 18p 1") {
		t.Errorf("sfen = %s, want 後手番 with 18 pawns in 後手's hand", got)
	}
}

// 칸이 모자란 줄은 몇 段인지를 말한다.
func TestParseBODNamesTheBrokenRank(t *testing.T) {
	text := strings.Replace(warsTsume, "| ・v金 ・ ・ ・ ・ ・ ・ ・|二", "| ・v金 ・ ・ ・ ・ ・ ・|二", 1)
	_, err := ParseBOD(text)
	var be *BODError
	if !errors.As(err, &be) || be.Rank != 2 {
		t.Fatalf("err = %v, want a BODError on rank 2", err)
	}
}

func TestParseBODWithoutADiagram(t *testing.T) {
	if _, err := ParseBOD("1 ７六歩(77)\n2 ３四歩(33)\n"); !errors.Is(err, ErrNoBOD) {
		t.Fatalf("err = %v, want ErrNoBOD", err)
	}
}

// 판 그림이 맞게 옮겨졌다면 그 뒤의 수순이 그 판에서 둬진다. 将棋ウォーズ 글의 세 手다.
func TestParseBODMovesPlayOnTheDiagram(t *testing.T) {
	sfen, err := ParseBOD(warsTsume)
	if err != nil {
		t.Fatal(err)
	}
	g, err := ParseUSI("position sfen " + sfen + " moves 7c8b 9c9d S*9e")
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Moves) != 3 {
		t.Fatalf("moves = %v, want three", g.Moves)
	}
}
