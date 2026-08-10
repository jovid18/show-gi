import { describe, expect, it } from 'vitest';

import { mobilityOf, type Mark } from './mobility';

const directionsOf = (kind: string) => mobilityOf(kind).map((mark) => mark.direction);
const reachesOf = (kind: string) => new Set(mobilityOf(kind).map((mark) => mark.reach));

describe('mobilityOf', () => {
  // 이 파일이 지키는 것은 하나다 — **桂를 대각선 한 칸으로 그리지 않는다.**
  // 한때 `nne` 를 `ne` 와 같은 격자 자리에 점으로 찍어서 판이 銀의 움직임을 가르쳤다.
  it('桂는 뛴다 — 격자 위의 점이 아니다', () => {
    expect(mobilityOf('N')).toEqual<Mark[]>([
      { reach: 'jump', direction: 'nne' },
      { reach: 'jump', direction: 'nnw' },
    ]);
  });

  it('뛰는 駒는 桂뿐이다', () => {
    const jumpers = Object.keys({ P: 0, L: 0, N: 0, S: 0, G: 0, B: 0, R: 0, K: 0 }).filter((kind) =>
      reachesOf(kind).has('jump'),
    );
    expect(jumpers).toEqual(['N']);
  });

  it('성하면 金이 된다 — 뜀이 남지 않는다', () => {
    for (const kind of ['+P', '+L', '+N', '+S']) {
      expect(directionsOf(kind)).toEqual(directionsOf('G'));
      expect(reachesOf(kind)).toEqual(new Set(['step']));
    }
  });

  it('馬·龍은 쭉 가는 곳과 한 칸 가는 곳을 함께 갖는다', () => {
    expect(reachesOf('+B')).toEqual(new Set(['slide', 'step']));
    expect(reachesOf('+R')).toEqual(new Set(['slide', 'step']));
  });

  it('모르는 종류는 표식이 없다 — 그리다 죽지 않는다', () => {
    expect(mobilityOf('')).toEqual([]);
    expect(mobilityOf('+K')).toEqual([]);
  });
});
