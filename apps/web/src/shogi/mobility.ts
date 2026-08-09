// 駒에 찍히는 움직임 표식.
//
// 초심자용 실물 駒가 하는 것과 같다 — **한 칸 가는 곳은 점, 쭉 가는 곳은 화살표.**
// 「香車가 어떻게 가더라」를 외우게 하지 않는 것이 이 앱의 목적에 곧바로 닿는다.
//
// **규칙 판단이 아니다.** 여기 있는 것은 駒의 *생김새*이지 둘 수 있는 수가 아니다.
// 실제로 어디에 둘 수 있는지는 서버가 `legalMoves` 로 주고, 이 파일은 그걸 모른다 —
// 막힌 길도 화살표는 그대로 그려진다. 실물 駒에 새겨진 그림과 같은 성격이다.

/** 駒에서 본 방향. `n` 이 그 駒가 나아가는 쪽이다(後手는 駒째로 뒤집혀서 같이 돈다). */
export type Direction = 'n' | 'ne' | 'e' | 'se' | 's' | 'sw' | 'w' | 'nw' | 'nne' | 'nnw';

export interface Mark {
  direction: Direction;
  /** 쭉 가는가(화살표). 한 칸이면 점이다. */
  slide: boolean;
}

const step = (...directions: Direction[]): Mark[] => directions.map((direction) => ({ direction, slide: false }));
const slide = (...directions: Direction[]): Mark[] => directions.map((direction) => ({ direction, slide: true }));

/** 金의 움직임. 성한 歩·香·桂·銀이 전부 이것이 된다 — 그래서 한 곳에 둔다. */
const GOLD = step('n', 'ne', 'e', 's', 'w', 'nw');

const MOBILITY: Record<string, Mark[]> = {
  P: step('n'),
  L: slide('n'),
  // 桂만 건너뛴다. 사이 칸을 지나지 않으므로 화살표가 아니라 뛰는 자리에 점을 찍는다.
  N: step('nne', 'nnw'),
  S: step('n', 'ne', 'se', 'sw', 'nw'),
  G: GOLD,
  B: slide('ne', 'se', 'sw', 'nw'),
  R: slide('n', 'e', 's', 'w'),
  K: step('n', 'ne', 'e', 'se', 's', 'sw', 'w', 'nw'),
  '+P': GOLD,
  '+L': GOLD,
  '+N': GOLD,
  '+S': GOLD,
  // 馬 = 角 + 한 칸씩의 상하좌우. 龍 = 飛 + 한 칸씩의 대각.
  '+B': [...slide('ne', 'se', 'sw', 'nw'), ...step('n', 'e', 's', 'w')],
  '+R': [...slide('n', 'e', 's', 'w'), ...step('ne', 'se', 'sw', 'nw')],
};

export function mobilityOf(kind: string): Mark[] {
  return MOBILITY[kind] ?? [];
}

/** 성한 駒인가. 실물 駒가 뒷면을 붉은 먹으로 쓰는 것과 같은 구분이다. */
export function isPromoted(kind: string): boolean {
  return kind.startsWith('+');
}
