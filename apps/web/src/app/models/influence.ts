// 利き — 칸마다 「누가 몇 매 손을 뻗고 있는가」.
//
// 합법수를 구하지 않는다. 나오는 것은 駒가 닿는 칸의 매수뿐이고 pin 도 二歩도 打ち歩詰め도
// 보지 않는다. 빛나는 칸은 서버의 `legalMoves` 가 정하고, 이 값은 그늘을 드리우는 데만 쓴다.
//
// sfen.ts 의 「규칙을 두 벌로 두지 않는다」와 부딪히지 않는다. 두 벌이 위험한 것은 어긋났을
// 때 어느 쪽이 맞는지 모르기 때문인데, 여기서는 어긋나도 서버가 이긴다.
//
// 서버에서 받아 오지 않는 것은 利き이 지금 판만 있으면 나오기 때문이다. 왕복을 한 번 더 하면
// 판이 바뀔 때마다 그늘이 한 프레임 늦게 따라온다.

import type { Board } from './sfen';
import type { GridDirection, JumpDirection } from './mobility';
import { mobilityOf } from './mobility';
import type { Side } from './piece';

const BOARD_SIZE = 9;
const SQUARES = BOARD_SIZE * BOARD_SIZE;

/**
 * 화면 배열에서의 방향. `n` 이 先手가 나아가는 쪽(위)이다.
 *
 * 後手 駒는 판 위에서 전부 180° 돌아 있으므로 두 성분을 함께 뒤집는다. 한쪽만 뒤집으면
 * 桂가 좌우로 뒤집힌 거울상이 된다.
 */
const STEP: Record<GridDirection, [number, number]> = {
  n: [0, -1],
  ne: [1, -1],
  e: [1, 0],
  se: [1, 1],
  s: [0, 1],
  sw: [-1, 1],
  w: [-1, 0],
  nw: [-1, -1],
};

/** 桂가 뛰는 두 칸. 두 칸 앞의 좌우이고 사이 칸을 밟지 않는다. */
const JUMP: Record<JumpDirection, [number, number]> = {
  nne: [1, -2],
  nnw: [-1, -2],
};

/** 칸마다의 利き 매수. 화면 배열 인덱스(0~80)로 읽는다. */
export interface Influence {
  black: Uint8Array;
  white: Uint8Array;
}

/**
 * 판에 드리운 利き을 센다.
 *
 * 자기 駒가 서 있는 칸도 센다. 그것이 「지켜지고 있다」이고, 상대가 손을 뻗은 칸을 이쪽이
 * 받고 있는지가 그늘의 깊이를 정한다.
 *
 * 쭉 가는 駒는 처음 만난 駒에서 멈추고 그 칸까지 센다. 사이에 낀 駒 너머를 세면(X-ray) 판이
 * 없는 위협을 말한다.
 */
export function influenceOf(board: Board): Influence {
  const black = new Uint8Array(SQUARES);
  const white = new Uint8Array(SQUARES);

  for (let index = 0; index < SQUARES; index += 1) {
    const piece = board.squares[index];
    if (!piece) continue;

    const count = piece.side === 'black' ? black : white;
    const facing = piece.side === 'black' ? 1 : -1;
    const col = index % BOARD_SIZE;
    const row = Math.floor(index / BOARD_SIZE);

    for (const mark of mobilityOf(piece.kind)) {
      if (mark.reach === 'jump') {
        const [dc, dr] = JUMP[mark.direction];
        touch(count, col + dc * facing, row + dr * facing);
        continue;
      }

      const [dc, dr] = STEP[mark.direction];
      let c = col + dc * facing;
      let r = row + dr * facing;

      if (mark.reach === 'step') {
        touch(count, c, r);
        continue;
      }

      while (touch(count, c, r)) {
        if (board.squares[r * BOARD_SIZE + c]) break;
        c += dc * facing;
        r += dr * facing;
      }
    }
  }

  return { black, white };
}

/** 한 칸을 센다. 판 안이면 true. 쭉 가는 駒가 이 값으로 계속 갈지 정한다. */
function touch(count: Uint8Array, col: number, row: number): boolean {
  if (col < 0 || col >= BOARD_SIZE || row < 0 || row >= BOARD_SIZE) return false;
  const at = row * BOARD_SIZE + col;
  // 실제로는 20을 넘을 수 없지만, 넘치면 0으로 감기면서 가장 위험한 칸이 안전해 보인다.
  if ((count[at] ?? 0) < 255) count[at] = (count[at] ?? 0) + 1;
  return true;
}

/**
 * 그늘의 깊이. 상대의 利き에서 이쪽이 받고 있는 매수를 뺀 것이고 음수는 0이다.
 *
 * 駒의 가치는 보지 않는다. 그늘은 「상대가 여기까지 손을 뻗고 있고 이쪽이 받지 않는다」까지고,
 * 「반드시 잡힌다」인지를 정하는 것은 엔진이다.
 */
export function exposure(influence: Influence, me: Side): Uint8Array {
  const mine = me === 'black' ? influence.black : influence.white;
  const theirs = me === 'black' ? influence.white : influence.black;
  const out = new Uint8Array(SQUARES);
  for (let i = 0; i < SQUARES; i += 1) out[i] = Math.max((theirs[i] ?? 0) - (mine[i] ?? 0), 0);
  return out;
}
