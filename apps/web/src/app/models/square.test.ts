import { describe, expect, it } from 'vitest';

import { fromIndex, fromUsi, SquareError, toIndex, toUsi } from './square';

describe('USI 표기', () => {
  it('초수 ▲7六歩의 출발·도착 칸을 읽는다', () => {
    // 7g = 7筋 7段(선수 歩의 처음 자리), 7f = 그 한 칸 앞
    expect(fromUsi('7g')).toEqual({ file: 7, rank: 7 });
    expect(fromUsi('7f')).toEqual({ file: 7, rank: 6 });
  });

  it('네 귀퉁이를 왕복해도 같다', () => {
    for (const usi of ['1a', '9a', '1i', '9i']) {
      expect(toUsi(fromUsi(usi))).toBe(usi);
    }
  });

  it('81칸 전부 왕복한다', () => {
    for (let file = 1; file <= 9; file++) {
      for (let rank = 1; rank <= 9; rank++) {
        expect(fromUsi(toUsi({ file, rank }))).toEqual({ file, rank });
      }
    }
  });

  it('범위 밖과 형식 오류를 거른다', () => {
    expect(() => fromUsi('7j')).toThrow(SquareError); // 段은 i까지
    expect(() => fromUsi('0a')).toThrow(SquareError);
    expect(() => fromUsi('7')).toThrow(SquareError);
    expect(() => toUsi({ file: 10, rank: 1 })).toThrow(SquareError);
  });
});

describe('배열 인덱스', () => {
  it('왼쪽 위가 0, 오른쪽 아래가 80이다', () => {
    // 筋이 오른쪽부터 세는 축이라, 화면 왼쪽 위는 9筋 1段이다.
    expect(toIndex({ file: 9, rank: 1 })).toBe(0);
    expect(toIndex({ file: 1, rank: 1 })).toBe(8);
    expect(toIndex({ file: 1, rank: 9 })).toBe(80);
  });

  it('81칸 전부 왕복한다', () => {
    for (let index = 0; index < 81; index++) {
      expect(toIndex(fromIndex(index))).toBe(index);
    }
  });

  it('범위 밖을 거른다', () => {
    expect(() => fromIndex(81)).toThrow(SquareError);
    expect(() => fromIndex(-1)).toThrow(SquareError);
  });
});
