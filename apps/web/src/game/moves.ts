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

const DROP = /^([PLNSGBR])\*([1-9][a-i])$/;
const BOARD = /^([1-9][a-i])([1-9][a-i])(\+?)$/;

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
    const drop = DROP.exec(usi);
    if (drop) {
      const [, kind, to] = drop;
      if (kind && to) put(`${kind}*`, to, false);
      continue;
    }
    const board = BOARD.exec(usi);
    if (board) {
      const [, from, to, plus] = board;
      if (from && to) put(from, to, plus === '+');
    }
  }

  return new Map([...grouped].map(([origin, dests]) => [origin, [...dests.values()]]));
}

/** 출발점과 도착 칸으로 실제 USI 문자열을 만든다. */
export function toUsiMove(origin: Origin, to: string, promote: boolean): string {
  if (origin.endsWith('*')) return `${origin}${to}`;
  return `${origin}${to}${promote ? '+' : ''}`;
}
