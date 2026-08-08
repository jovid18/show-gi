// 기물의 표기. 서버의 `kifu.go`와 같은 글자를 쓴다 — 棋譜와 판이 어긋나면 안 된다.

/** SFEN 한 글자(승격은 `+` 접두). 대문자 = 先手, 소문자 = 後手. */
export type PieceCode = string;

export type Side = 'black' | 'white';

export interface Piece {
  /** 승격을 포함한 종류. `P`, `+P`, `K` … 항상 대문자로 정규화한다 */
  kind: string;
  side: Side;
}

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
  '+L': '成香',
  '+N': '成桂',
  '+S': '成銀',
  '+B': '馬',
  '+R': '龍',
};

/** 持ち駒에 놓이는 순서. 관례대로 飛→角→金→銀→桂→香→歩. */
export const HAND_ORDER = ['R', 'B', 'G', 'S', 'N', 'L', 'P'] as const;

export function kanjiOf(kind: string): string {
  return KANJI[kind] ?? kind;
}

/** 「成香」처럼 두 글자인 기물은 칸 안에서 더 작게 그려야 한다. */
export function isWideKanji(kind: string): boolean {
  return kanjiOf(kind).length > 1;
}
