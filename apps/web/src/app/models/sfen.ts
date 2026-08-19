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
