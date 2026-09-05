import { describe, expect, it } from 'vitest';

import { parseSfen, SfenError, toSfen } from './sfen';
import { toIndex } from './square';

const START = 'lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1';

describe('parseSfen', () => {
  it('초기 국면을 푼다', () => {
    const board = parseSfen(START);

    expect(board.squares).toHaveLength(81);
    expect(board.turn).toBe('black');
    // 5一은 後手 玉, 5九는 先手 玉
    expect(board.squares[toIndex({ file: 5, rank: 1 })]).toEqual({ kind: 'K', side: 'white' });
    expect(board.squares[toIndex({ file: 5, rank: 9 })]).toEqual({ kind: 'K', side: 'black' });
    // 8八은 先手 角
    expect(board.squares[toIndex({ file: 8, rank: 8 })]).toEqual({ kind: 'B', side: 'black' });
    expect(board.squares[toIndex({ file: 5, rank: 5 })]).toBeNull();
  });

  it('승격한 말을 푼다', () => {
    const board = parseSfen('k8/4+P4/9/9/9/2+b6/9/9/4K4 w - 1');

    expect(board.squares[toIndex({ file: 5, rank: 2 })]).toEqual({ kind: '+P', side: 'black' });
    expect(board.squares[toIndex({ file: 7, rank: 6 })]).toEqual({ kind: '+B', side: 'white' });
    expect(board.turn).toBe('white');
  });

  it('持ち駒의 개수를 읽는다', () => {
    const board = parseSfen('k8/9/9/9/9/9/9/9/4K4 b R2Pbg3p 1');

    expect(board.hands.black).toEqual({ R: 1, P: 2 });
    expect(board.hands.white).toEqual({ B: 1, G: 1, P: 3 });
  });

  it('持ち駒가 없으면 비어 있다', () => {
    expect(parseSfen(START).hands).toEqual({ black: {}, white: {} });
  });

  it('망가진 SFEN은 던진다', () => {
    // 판을 못 읽으면 그리지 않는다. 틀린 판을 그리는 것이 더 나쁘다.
    expect(() => parseSfen('lnsgkgsnl/9 b - 1')).toThrow(SfenError);
    expect(() => parseSfen('9/9/9/9/9/9/9/9/8 b - 1')).toThrow(SfenError);
    expect(() => parseSfen('too short')).toThrow(SfenError);
    expect(() => parseSfen(START.replace(' b ', ' x '))).toThrow(SfenError);
  });
});

/**
 * 왕복이 맞아야 편집기가 쓸 수 있다. `parseSfen` 은 이미 판을 그리는 데 쓰이고 있었고
 * (`ExploreScreen` 외 셋) `toSfen` 은 사람이 고친 판을 주소에 싣기 위해 생겼다
 * (journal §129) — 한쪽만 틀리면 고친 판과 분석되는 판이 갈린다.
 */
describe('toSfen', () => {
  const cases: Record<string, string> = {
    '평수 초기 국면': 'lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1',
    '持ち駒이 양쪽에 있다': '4k4/9/9/9/9/9/9/9/4K4 b R2G3Pb4l 1',
    '성한 駒가 섞여 있다': '4k4/4+P4/2+r6/9/9/9/6+s2/9/4K4 w - 1',
    '수번이 後手다': 'lnsgkgsnl/1r5b1/ppppppppp/9/9/2P6/PP1PPPPPP/1B5R1/LNSGKGSNL w - 1',
    '판이 비어 있다': '9/9/9/9/9/9/9/9/9 b - 1',
  };

  for (const [name, sfen] of Object.entries(cases)) {
    it(name, () => {
      expect(toSfen(parseSfen(sfen))).toBe(sfen);
    });
  }

  // 1장은 개수를 안 적는다. 적으면 서버가 내는 SFEN 과 글자가 달라지고, 같은 국면에
  // 주소가 두 벌 생긴다.
  it('持ち駒 한 장에는 개수를 안 적는다', () => {
    const board = parseSfen('4k4/9/9/9/9/9/9/9/4K4 b - 1');
    board.hands.black.P = 1;
    board.hands.white.R = 2;
    expect(toSfen(board)).toBe('4k4/9/9/9/9/9/9/9/4K4 b P2r 1');
  });

  // 持ち駒 순서는 관례대로 飛→角→金→銀→桂→香→歩다. 서버의 출력과 같아야 한다.
  it('持ち駒을 관례 순서로 적는다', () => {
    const board = parseSfen('4k4/9/9/9/9/9/9/9/4K4 b - 1');
    board.hands.black = { P: 1, L: 1, N: 1, S: 1, G: 1, B: 1, R: 1 };
    expect(toSfen(board)).toBe('4k4/9/9/9/9/9/9/9/4K4 b RBGSNLP 1');
  });

  // 0장은 안 적는다. 사람이 개수를 내리다 0으로 만든 자리라 반드시 지난다.
  it('0장은 안 적는다', () => {
    const board = parseSfen('4k4/9/9/9/9/9/9/9/4K4 b P 1');
    board.hands.black.P = 0;
    expect(toSfen(board)).toBe('4k4/9/9/9/9/9/9/9/4K4 b - 1');
  });

  /**
   * 여기서 규칙을 판단하지 않는다. 二歩든 玉이 둘이든 그대로 적는다 — 성립하는 판인가는
   * 서버의 룰 엔진이 답하고, 여기서 걸러 버리면 사람이 고쳐 가는 중간 상태를 그릴 수 없다.
   */
  it('성립하지 않는 판도 그대로 적는다', () => {
    const nifu = '4k4/9/9/9/4P4/9/4P4/9/4K4 b - 1';
    expect(toSfen(parseSfen(nifu))).toBe(nifu);
  });

  // 手数를 1로 적는다. 사진 한 장에는 手数가 없고, 검토의 뿌리는 언제나 0手目다.
  it('手数를 언제나 1로 적는다', () => {
    expect(toSfen(parseSfen('4k4/9/9/9/9/9/9/9/4K4 b - 128'))).toMatch(/ 1$/);
  });
});
