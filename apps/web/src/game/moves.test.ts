import { describe, expect, it } from 'vitest';

import { groupByOrigin, toUsiMove } from './moves';

describe('groupByOrigin', () => {
  it('반상의 수를 출발칸별로 묶는다', () => {
    const grouped = groupByOrigin(['7g7f', '2g2f', '7i7h', '7i6h']);

    expect([...grouped.keys()].toSorted()).toEqual(['2g', '7g', '7i']);
    expect(
      grouped
        .get('7i')
        ?.map((d) => d.to)
        .toSorted(),
    ).toEqual(['6h', '7h']);
  });

  it('성·불성이 둘 다 오면 한 항목에 담는다', () => {
    // 「成りますか」를 물을지가 여기서 갈린다
    const grouped = groupByOrigin(['8h2b', '8h2b+']);
    const dest = grouped.get('8h')?.[0];

    expect(dest).toEqual({ to: '2b', plain: true, promote: true });
  });

  it('강제 승격은 성만 남아 묻지 않는다', () => {
    const grouped = groupByOrigin(['5b5a+']);

    expect(grouped.get('5b')?.[0]).toEqual({ to: '5a', plain: false, promote: true });
  });

  it('持ち駒 투입은 `P*` 를 출발점으로 쓴다', () => {
    const grouped = groupByOrigin(['P*5e', 'P*4e', 'G*5b']);

    expect(
      grouped
        .get('P*')
        ?.map((d) => d.to)
        .toSorted(),
    ).toEqual(['4e', '5e']);
    expect(grouped.get('G*')?.[0]).toEqual({ to: '5b', plain: true, promote: false });
  });

  it('읽을 수 없는 표기는 조용히 버린다', () => {
    // 서버가 주는 목록이라 여기 걸릴 일이 없지만, 걸렸을 때 판 전체가 안 그려지면 안 된다
    expect(groupByOrigin(['nonsense', '7g7f']).size).toBe(1);
  });
});

describe('toUsiMove', () => {
  it('반상 이동과 승격', () => {
    expect(toUsiMove('7g', '7f', false)).toBe('7g7f');
    expect(toUsiMove('8h', '2b', true)).toBe('8h2b+');
  });

  it('투입에는 승격이 없다', () => {
    expect(toUsiMove('P*', '5e', false)).toBe('P*5e');
    expect(toUsiMove('P*', '5e', true)).toBe('P*5e');
  });
});
