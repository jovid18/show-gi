// 기물의 표기.
//
// 판 위의 글자와 棋譜의 글자는 다르다. 棋譜는 서버가 `ja` 로 주고(`kifu.go`), 여기 있는
// 것은 駒에 새겨지는 글자뿐이다. 둘을 같게 두려다 판이 못 읽게 되면 본말이 뒤집힌다.

/** SFEN 한 글자(승격은 `+` 접두). 대문자 = 先手, 소문자 = 後手. */
export type PieceCode = string;

export type Side = 'black' | 'white';

export interface Piece {
  /** 승격을 포함한 종류. `P`, `+P`, `K` … 항상 대문자로 정규화한다 */
  kind: string;
  side: Side;
}

/**
 * 駒 면에 새기는 글자.
 *
 * 성한 銀·桂·香는 실물 駒와 같은 한 글자 약자(全·圭·杏)를 쓴다. 「成銀」처럼 두 글자를
 * 세로로 쌓으면 한 글자가 칸 크기의 절반 아래로 내려가고, 움직임 표식이 들어갈 자리도
 * 없어진다. 약자는 실제 駒에 쓰이는 것이라 원전과도 어긋나지 않는다.
 */
const KANJI: Record<string, string> = {
  P: '歩',
  L: '香',
  N: '桂',
  S: '銀',
  G: '金',
  B: '角',
  R: '飛',
  K: '玉',
  '+P': 'と',
  '+L': '杏',
  '+N': '圭',
  '+S': '全',
  '+B': '馬',
  '+R': '龍',
};

/** 소리내어 읽는 이름. 약자는 눈으로만 통하므로 스크린리더에는 온전한 이름을 준다. */
const NAME: Record<string, string> = {
  '+P': 'と金',
  '+L': '成香',
  '+N': '成桂',
  '+S': '成銀',
};

/** 持ち駒에 놓이는 순서. 관례대로 飛→角→金→銀→桂→香→歩. */
export const HAND_ORDER = ['R', 'B', 'G', 'S', 'N', 'L', 'P'] as const;

export function kanjiOf(kind: string): string {
  return KANJI[kind] ?? kind;
}

export function nameOf(kind: string): string {
  return NAME[kind] ?? kanjiOf(kind);
}
