import { describe, expect, it } from 'vitest';

import { shouldRing, soundOnFrom } from './useMoveSound';

// 저장된 값이 없는 것과 못 읽는 것이 같은 답이어야 한다. 갈리면 localStorage 를 막아 둔
// 브라우저에서만 소리가 안 나고, 그건 화면에서 버그로 안 보인다.
describe('soundOnFrom', () => {
  it('저장된 것이 없으면 켜짐이다', () => {
    expect(soundOnFrom(null)).toBe(true);
  });

  it('끈 것만 끈 것으로 센다', () => {
    expect(soundOnFrom('off')).toBe(false);
    expect(soundOnFrom('on')).toBe(true);
    expect(soundOnFrom('')).toBe(true);
  });
});

describe('shouldRing', () => {
  it('手数가 늘면 운다', () => {
    expect(shouldRing(4, 5)).toBe(true);
  });

  // 개입이 걸리면 手数가 뒤로 간다. 그때 우는 것은 「두어졌다」는 거짓말이다.
  it('되물러서 手数가 뒤로 가면 안 운다', () => {
    expect(shouldRing(5, 4)).toBe(false);
  });

  it('같은 手数에 다시 그려도 안 운다', () => {
    expect(shouldRing(5, 5)).toBe(false);
  });

  // 이어하기로 들어온 판은 手数가 0이 아닌 채로 시작한다.
  it('첫 렌더에서는 안 운다', () => {
    expect(shouldRing(null, 0)).toBe(false);
    expect(shouldRing(null, 37)).toBe(false);
  });
});
