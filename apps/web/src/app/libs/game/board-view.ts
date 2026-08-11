// 대국 화면이 판에 넘길 값을 만드는 순수 계산들. **React가 없다** — 상태도 훅도
// 여기 들어오지 않으므로 국면 하나를 넣고 결과를 견주는 테스트가 그대로 된다.
//
// 규칙 판단은 없다. 합법수도 국면도 서버가 주고, 이 파일이 하는 것은 좌표와 표기뿐이다.

import type { LastMove, Ray } from '@/components/Board';
import type { Attack, Player, Snapshot } from '@/protocol/game';
import type { Board as BoardModel } from '@/models/sfen';
import type { Side } from '@/models/piece';
import { fromUsi, toIndex } from '@/models/square';
import { parseUsi } from '@/libs/game/moves';

/**
 * 직전 수가 지나간 두 칸.
 *
 * **도착만 짚으면 「저기 뭔가 있다」까지다.** 초심자가 알아야 하는 것은 무엇이 어디서
 * 왔나이고, 출발 칸이 비었다는 사실이 그 절반이다.
 */
export function lastMoveOf(usi: string): LastMove | null {
  const move = parseUsi(usi);
  if (!move) return null;
  try {
    const to = toIndex(fromUsi(move.to));
    return { from: move.kind === 'drop' ? null : toIndex(fromUsi(move.from)), to };
  } catch {
    return null; // 못 읽는 좌표로 엉뚱한 칸을 칠하느니 안 칠한다
  }
}

/**
 * 회상 한 장면.
 *
 * **화살표는 방금 둔 수가 아니라 다음에 올 수다.** 판은 이미 벌어진 것을 보여주고 있으니
 * 거기에 지나간 궤적을 겹치면 같은 말을 두 번 하는 것이고, 알고 싶은 것은 「그래서 상대가
 * 어떻게 하는가」다. `次へ` 를 누르면 화살표대로 벌어진다.
 *
 * 그래서 채널이 갈린다 — **흰빛 두 칸은 방금 벌어진 것**, **초록 화살표는 다음에 벌어질 것.**
 */
export interface Scene {
  board: BoardModel;
  /** 이 판을 만든 수. 흰빛 두 칸으로 짚는다. */
  played: LastMove;
  /** 다음에 올 수. 마지막 장면에는 없다. */
  ray: Ray | null;
  /** 이 장면에서 玉을 잡으러 오는 말들. 王手가 아니면 비어 있다. */
  checks: Ray[];
  /** 화살표가 가리키는 수가 수순의 몇 번째인가. 마지막 장면이면 -1. */
  next: number;
  /** 다음 수가 打이면 그 駒. 駒台에서 같이 빛나 화살표의 짝이 된다. */
  dropping: { side: Side; kind: string } | null;
}

/** 王手를 거는 줄. 서버가 준 두 칸을 그대로 옮긴다 — 화면은 누가 王手인지 계산하지 않는다. */
export function checkRays(checks: Attack[] | undefined): Ray[] {
  if (!checks?.length) return [];
  const out: Ray[] = [];
  for (const c of checks) {
    try {
      out.push({ from: toIndex(fromUsi(c.from)), to: toIndex(fromUsi(c.to)), by: 'engine', check: true });
    } catch {
      // 못 읽는 좌표로 엉뚱한 선을 긋느니 그 한 줄을 버린다
    }
  }
  return out;
}

export function rayOf(usi: string, by: Player): Ray | null {
  const move = parseUsi(usi);
  if (!move) return null;
  try {
    return { from: move.kind === 'drop' ? null : toIndex(fromUsi(move.from)), to: toIndex(fromUsi(move.to)), by };
  } catch {
    return null; // 못 읽는 좌표로 엉뚱한 화살표를 긋느니 안 긋는다
  }
}

/**
 * `ancestor` 안에서의 자리(px). `offsetParent` 를 타고 올라가며 더한다.
 *
 * **`getBoundingClientRect` 를 안 쓴다.** 그쪽은 변형이 끝난 화면 좌표를 주는데, 화살표가
 * 놓이는 자리는 변형 **전**의 배치 좌표다. 개입 때 판을 기울이던 동안에는 그 차이가 곧
 * 화살표가 어긋나는 것이었고(기울기는 뺐다 — index.css), 지금도 판이 어떤 변형을 받든
 * 이 계산은 그대로 맞는다.
 */
export function offsetWithin(el: HTMLElement, ancestor: HTMLElement): { x: number; y: number } | null {
  let x = 0;
  let y = 0;
  let node: HTMLElement | null = el;
  while (node && node !== ancestor) {
    x += node.offsetLeft;
    y += node.offsetTop;
    node = node.offsetParent as HTMLElement | null;
  }
  return node === ancestor ? { x, y } : null;
}

export function resultText(snapshot: Snapshot): string | null {
  const won = snapshot.winner === 'human';
  switch (snapshot.status) {
    case 'checkmate':
      return won ? '詰み。あなたの勝ちです。' : '詰み。あなたの負けです。';
    case 'stalemate':
      return won ? '手詰まり。あなたの勝ちです。' : '手詰まり。あなたの負けです。';
    case 'resigned':
      return won ? '相手が投了しました。あなたの勝ちです。' : '投了しました。';
    case 'repetition':
      return '千日手。引き分けです。';
    default:
      return null;
  }
}
