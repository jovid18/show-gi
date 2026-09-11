import { describe, expect, it } from 'vitest';

import { afterFailure, startingLoad, type Loaded } from './useReview';

/** 화면에 이미 그려진 답. 무엇이 들었는지는 이 검사와 무관하다. */
const ready: Loaded<{ n: number }> = { state: 'ready', data: { n: 1 } };

// 폴링하는 화면 둘이 이 함수에 걸려 있다. 되짚기는 분석이 끝날 때까지 5초마다,
// 퀴즈는 생성이 끝날 때까지 5초마다 같은 주소를 다시 묻는다(journal §136).
describe('startingLoad', () => {
  it('같은 주소를 다시 물을 때는 그리던 것을 남긴다', () => {
    expect(startingLoad(ready, true)).toBe(ready);
  });

  // 남기면 새 판을 받는 동안 앞 판의 기보가 그대로 보인다.
  it('주소가 바뀌면 비운다', () => {
    expect(startingLoad(ready, false)).toEqual({ state: 'loading' });
  });

  // 「もう一度」를 누른 자리다. 그리던 것이 없으므로 남길 것도 없다.
  it('오류에서 다시 물으면 비운다', () => {
    expect(startingLoad({ state: 'error', message: 'だめ' }, true)).toEqual({ state: 'loading' });
  });

  it('첫 요청은 비운 채로 시작한다', () => {
    expect(startingLoad({ state: 'loading' }, false)).toEqual({ state: 'loading' });
  });
});

describe('afterFailure', () => {
  it('그리던 것이 있으면 실패해도 남긴다', () => {
    expect(afterFailure(ready, 'だめ')).toBe(ready);
  });

  it('처음부터 아무것도 없었으면 오류를 말한다', () => {
    expect(afterFailure({ state: 'loading' }, 'だめ')).toEqual({ state: 'error', message: 'だめ' });
  });

  it('오류가 이어지면 마지막 이유로 바꾼다', () => {
    expect(afterFailure({ state: 'error', message: '前' }, '後')).toEqual({ state: 'error', message: '後' });
  });
});
