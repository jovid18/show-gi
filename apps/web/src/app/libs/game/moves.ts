// 서버가 준 합법수 목록을 판이 쓰기 좋은 모양으로 바꾼다.
//
// **여기서 규칙을 판단하지 않는다.** 목록에 있으면 둘 수 있고 없으면 못 둔다.
// 그래서 二歩도 打ち歩詰め도 클라이언트가 알 필요가 없다 — 애초에 목록에 안 들어온다.

/** 착수 출발점. 반상이면 `7g`, 持ち駒면 `P*` 같은 모양. */
export type Origin = string;

export interface Destination {
  /** 도착 칸. `7f` */
  to: string;
  /** 승격하지 않고 두는 수가 목록에 있는가 */
  plain: boolean;
  /** 승격해서 두는 수가 목록에 있는가 */
  promote: boolean;
}

/**
 * USI 한 수를 풀어 놓은 것.
 *
 * **규칙 판단이 아니라 문자열 해석이다.** 둘 수 있는 수인지는 여기서 알 수 없고 알 필요도
 * 없다 — 합법수는 서버가 목록으로 주고, 물러진 수는 서버가 이미 판정을 끝낸 것이다.
 */
export type ParsedMove =
  | { kind: 'board'; from: string; to: string; promote: boolean }
  | { kind: 'drop'; piece: string; to: string };

const DROP = /^([PLNSGBR])\*([1-9][a-i])$/;
const BOARD = /^([1-9][a-i])([1-9][a-i])(\+?)$/;

/** 읽을 수 없으면 null. 판 전체를 못 그리게 되는 것보다 그 한 수를 버리는 편이 낫다. */
export function parseUsi(usi: string): ParsedMove | null {
  const drop = DROP.exec(usi);
  if (drop?.[1] && drop[2]) {
    return { kind: 'drop', piece: drop[1], to: drop[2] };
  }
  const board = BOARD.exec(usi);
  if (board?.[1] && board[2]) {
    return { kind: 'board', from: board[1], to: board[2], promote: board[3] === '+' };
  }
  return null;
}

/**
 * 합법수를 출발점별로 묶는다.
 *
 * 같은 (출발, 도착)이 성/불성으로 두 번 올 수 있어서 한 항목에 두 갈래를 함께 담는다.
 * 어느 쪽이 가능한지가 곧 「成りますか」를 물을지 말지다.
 */
export function groupByOrigin(legalMoves: readonly string[]): Map<Origin, Destination[]> {
  const grouped = new Map<Origin, Map<string, Destination>>();

  const put = (origin: Origin, to: string, promote: boolean): void => {
    let dests = grouped.get(origin);
    if (!dests) {
      dests = new Map();
      grouped.set(origin, dests);
    }
    const found = dests.get(to) ?? { to, plain: false, promote: false };
    if (promote) found.promote = true;
    else found.plain = true;
    dests.set(to, found);
  };

  for (const usi of legalMoves) {
    const move = parseUsi(usi);
    if (!move) continue;
    if (move.kind === 'drop') put(`${move.piece}*`, move.to, false);
    else put(move.from, move.to, move.promote);
  }

  return new Map([...grouped].map(([origin, dests]) => [origin, [...dests.values()]]));
}

/** 출발점과 도착 칸으로 실제 USI 문자열을 만든다. */
export function toUsiMove(origin: Origin, to: string, promote: boolean): string {
  if (origin.endsWith('*')) return `${origin}${to}`;
  return `${origin}${to}${promote ? '+' : ''}`;
}
