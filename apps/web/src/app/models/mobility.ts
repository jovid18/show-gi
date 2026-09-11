// 駒에 찍히는 움직임 표식. 무엇을 어떤 모양으로 그리는지는 journal §22.
//
// 규칙 판단을 하지 않는다. 여기 있는 것은 駒의 생김새뿐이고, 어디에 둘 수 있는지는 서버가
// `legalMoves` 로 준다. 막힌 길도 화살표는 그대로 그려진다.

/** 3×3 격자 위의 방향. `n` 이 그 駒가 나아가는 쪽이다(後手는 駒째로 뒤집혀서 같이 돈다). */
export type GridDirection = 'n' | 'ne' | 'e' | 'se' | 's' | 'sw' | 'w' | 'nw';

/** 桂가 뛰는 두 곳. 두 칸 앞의 좌우라 격자 밖이다. */
export type JumpDirection = 'nne' | 'nnw';

export type Direction = GridDirection | JumpDirection;

/**
 * 표식 하나. 뜀을 따로 가르는 것은 점이 「그 칸」이라 두 칸 앞을 가리킬 수 없고, 화살표는
 * 「지나간다」라서 桂가 하지 않는 일을 가르치기 때문이다.
 */
export type Mark = { reach: 'step' | 'slide'; direction: GridDirection } | { reach: 'jump'; direction: JumpDirection };

const step = (...directions: GridDirection[]): Mark[] => directions.map((direction) => ({ reach: 'step', direction }));
const slide = (...directions: GridDirection[]): Mark[] =>
  directions.map((direction) => ({ reach: 'slide', direction }));
const jump = (...directions: JumpDirection[]): Mark[] => directions.map((direction) => ({ reach: 'jump', direction }));

/** 金의 움직임. 성한 歩·香·桂·銀이 전부 이것이 되어서 한 곳에 둔다. */
const GOLD = step('n', 'ne', 'e', 's', 'w', 'nw');

const MOBILITY: Record<string, Mark[]> = {
  P: step('n'),
  L: slide('n'),
  // 桂만 건너뛴다. 사이 칸을 지나지 않으므로 화살표 대신 뛰는 길을 점선으로 그린다.
  N: jump('nne', 'nnw'),
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
