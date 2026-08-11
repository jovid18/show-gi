import { describe, expect, it } from 'vitest';

import { parseSfen, SfenError } from './sfen';
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
