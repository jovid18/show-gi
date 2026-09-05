// SFEN을 화면에 그릴 수 있는 모양으로 푼다.
//
// 여기서 규칙을 판단하지 않는다. 합법수는 서버가 `legalMoves`로 주고, 이 파일은
// "지금 판에 무엇이 어디 있나"만 읽는다. 클라이언트가 규칙을 따로 구현하기 시작하면
// 서버와 두 벌이 생기고, 어긋났을 때 어느 쪽이 맞는지 아무도 모르게 된다.

import type { Piece, Side } from './piece';

const BOARD_SIZE = 9;

export class SfenError extends Error {}

export interface Board {
  /** 왼쪽 위(9筋 1段)부터 행 우선 81칸. 빈 칸은 null. */
  squares: (Piece | null)[];
  /** 持ち駒. kind(승격 없음) → 개수. */
  hands: Record<Side, Record<string, number>>;
  turn: Side;
}

function pieceOf(letter: string, promoted: boolean): Piece {
  const upper = letter.toUpperCase();
  return {
    kind: promoted ? `+${upper}` : upper,
    side: letter === upper ? 'black' : 'white',
  };
}

export function parseSfen(sfen: string): Board {
  const [boardField, turnField, handField] = sfen.trim().split(/\s+/);
  if (!boardField || !turnField || !handField) {
    throw new SfenError(`SFEN 필드가 부족하다: ${sfen}`);
  }

  const ranks = boardField.split('/');
  if (ranks.length !== BOARD_SIZE) {
    throw new SfenError(`판은 9단이어야 한다: ${boardField}`);
  }

  const squares: (Piece | null)[] = [];
  for (const rank of ranks) {
    let file = 0;
    let promoted = false;

    for (const ch of rank) {
      if (ch >= '1' && ch <= '9') {
        const empty = Number(ch);
        for (let i = 0; i < empty; i += 1) squares.push(null);
        file += empty;
        continue;
      }
      if (ch === '+') {
        promoted = true;
        continue;
      }
      squares.push(pieceOf(ch, promoted));
      promoted = false;
      file += 1;
    }

    if (file !== BOARD_SIZE) {
      throw new SfenError(`한 단은 9칸이어야 한다: ${rank} (${file})`);
    }
  }

  const hands: Record<Side, Record<string, number>> = { black: {}, white: {} };
  if (handField !== '-') {
    let count = 0;
    for (const ch of handField) {
      if (ch >= '0' && ch <= '9') {
        count = count * 10 + Number(ch);
        continue;
      }
      const upper = ch.toUpperCase();
      const side: Side = ch === upper ? 'black' : 'white';
      hands[side][upper] = (hands[side][upper] ?? 0) + (count === 0 ? 1 : count);
      count = 0;
    }
  }

  if (turnField !== 'b' && turnField !== 'w') {
    throw new SfenError(`수번은 b 또는 w다: ${turnField}`);
  }

  return { squares, hands, turn: turnField === 'b' ? 'black' : 'white' };
}

/** 持ち駒를 적는 순서. 관례대로 飛→角→金→銀→桂→香→歩 (`HAND_ORDER` 와 같은 표다). */
const HAND_SFEN_ORDER = ['R', 'B', 'G', 'S', 'N', 'L', 'P'] as const;

/**
 * 판을 SFEN 한 줄로 되돌린다. `parseSfen` 의 역이다.
 *
 * 이 파일에서 유일하게 「글자를 만드는」 자리다. 사진에서 읽어 온 판을 사람이 한 칸씩
 * 고치는 화면이 있어서 필요해졌고(journal §129), 고친 판이 주소가 되어 서버로 간다.
 *
 * **여기서도 규칙을 판단하지 않는다.** 二歩든 玉이 둘이든 그대로 적는다 — 성립하는
 * 판인가는 서버의 룰 엔진이 답하고(`/api/position/check`), 화면은 그 답을 보여 줄 뿐이다.
 * 여기서 걸러 버리면 사람이 고치는 중간 상태를 화면이 그릴 수 없게 된다.
 *
 * 手数를 1로 적는다. 사진 한 장에는 手数가 없고, 검토의 뿌리는 언제나 0手目다.
 */
export function toSfen(board: Board): string {
  const ranks: string[] = [];
  for (let row = 0; row < BOARD_SIZE; row += 1) {
    let rank = '';
    let empty = 0;
    for (let col = 0; col < BOARD_SIZE; col += 1) {
      const piece = board.squares[row * BOARD_SIZE + col] ?? null;
      if (piece === null) {
        empty += 1;
        continue;
      }
      if (empty > 0) {
        rank += String(empty);
        empty = 0;
      }
      // 승격 표시는 `+` 접두이고, 색은 대소문자다 — `parseSfen` 이 읽는 그 규약이다.
      const letter = piece.side === 'black' ? piece.kind : piece.kind.toLowerCase();
      rank += letter;
    }
    if (empty > 0) rank += String(empty);
    ranks.push(rank);
  }

  let hands = '';
  for (const side of ['black', 'white'] as const) {
    for (const kind of HAND_SFEN_ORDER) {
      const n = board.hands[side][kind] ?? 0;
      if (n <= 0) continue;
      // 1장은 개수를 안 적는다. 적으면 왕복이 안 맞고, 서버의 출력도 그 규약이다.
      if (n >= 2) hands += String(n);
      hands += side === 'black' ? kind : kind.toLowerCase();
    }
  }

  const turn = board.turn === 'black' ? 'b' : 'w';
  return `${ranks.join('/')} ${turn} ${hands === '' ? '-' : hands} 1`;
}
