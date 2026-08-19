// 쇼기 좌표 변환.
//
// 판은 9×9이고 좌표는 두 축의 이름이 다르다.
//   筋(すじ, file) — 세로줄. 오른쪽부터 1~9
//   段(だん, rank) — 가로줄. 위부터 1~9 (사람이 읽을 때는 一~九)
//
// 사람이 읽는 표기는 「7六」이지만 엔진과 주고받는 USI 표기는 `7f`다.
// 段을 a~i로 적는다. 초수 ▲7六歩는 USI로 `7g7f`가 된다.
//
// 화면 배열은 왼쪽 위(9筋 1段)부터 행 우선으로 깐다. 사람이 판을 보는 순서와 같다.

/** 판 위의 한 칸. file·rank 모두 1~9. */
export interface Square {
  /** 筋. 오른쪽부터 1~9 */
  file: number;
  /** 段. 위부터 1~9 */
  rank: number;
}

/**
 * 한 수가 지나간 두 자리(화면 배열 인덱스).
 *
 * 판이 통째로 바뀌면 초심자는 무엇이 변했는지 못 본다(docs/03-frontend.md §3).
 * 도착 칸의 駒를 출발 칸에서 끌어오는 데 쓰는 값이라 장식이 아니라 되짚기의 내용이다.
 *
 * `components/Board` 가 아니라 여기 있는 이유는 칸 인덱스 두 개가 전부이기 때문이다.
 * 판에 있으면 `libs` 가 화면 부품을 참조하게 되고, 그러면 층의 방향이 거꾸로 선다.
 */
export interface Motion {
  /** 도착 칸. */
  to: number;
  /** 출발 칸. 打이면 null — 위에서 떨어진다. */
  from: number | null;
  /**
   * 같은 칸에 두 번 들어와도 다시 나게 하는 열쇠.
   *
   * 되잡기가 정확히 그 경우다. 같은 칸에 연달아 들어오면 요소가 그대로 남아 CSS
   * 애니메이션이 다시 시작하지 않는다 — 이 값이 바뀌면 그 칸만 새로 붙는다.
   */
  id: number;
}

const RANK_LETTERS = 'abcdefghi';
const BOARD_SIZE = 9;

export class SquareError extends Error {}

function assertInRange(value: number, label: string): void {
  if (!Number.isInteger(value) || value < 1 || value > BOARD_SIZE) {
    throw new SquareError(`${label}는 1~9의 정수여야 한다: ${value}`);
  }
}

/** `{file: 7, rank: 6}` → `'7f'` */
export function toUsi(sq: Square): string {
  assertInRange(sq.file, 'file');
  assertInRange(sq.rank, 'rank');
  return `${sq.file}${RANK_LETTERS.charAt(sq.rank - 1)}`;
}

/** `'7f'` → `{file: 7, rank: 6}` */
export function fromUsi(usi: string): Square {
  if (usi.length !== 2) {
    throw new SquareError(`USI 좌표는 두 글자다: ${usi}`);
  }

  const file = Number(usi.charAt(0));
  const rank = RANK_LETTERS.indexOf(usi.charAt(1)) + 1;

  // indexOf가 못 찾으면 0이 되어 아래 검사에 걸린다.
  assertInRange(file, 'file');
  assertInRange(rank, 'rank');

  return { file, rank };
}

/** 화면 배열 인덱스(0~80). 왼쪽 위 = 9筋 1段 = 0 */
export function toIndex(sq: Square): number {
  assertInRange(sq.file, 'file');
  assertInRange(sq.rank, 'rank');
  return (sq.rank - 1) * BOARD_SIZE + (BOARD_SIZE - sq.file);
}

/** toIndex의 역. */
export function fromIndex(index: number): Square {
  if (!Number.isInteger(index) || index < 0 || index >= BOARD_SIZE * BOARD_SIZE) {
    throw new SquareError(`인덱스는 0~80의 정수여야 한다: ${index}`);
  }
  return {
    file: BOARD_SIZE - (index % BOARD_SIZE),
    rank: Math.floor(index / BOARD_SIZE) + 1,
  };
}
