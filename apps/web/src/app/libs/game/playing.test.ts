import { afterEach, describe, expect, it, vi } from 'vitest';

import { getPlaying, setPlaying, subscribePlaying } from './playing';

// 모듈 하나가 값을 들고 있으므로 테스트끼리 새어 나간다. 끝날 때마다 되돌린다.
afterEach(() => setPlaying(false));

describe('playing', () => {
  it('구독한 쪽이 바뀔 때만 깨어난다', () => {
    const notify = vi.fn();
    const stop = subscribePlaying(notify);

    setPlaying(true);
    expect(notify).toHaveBeenCalledTimes(1);
    expect(getPlaying()).toBe(true);

    // 같은 값은 안 알린다. 매 手마다 스냅샷이 오는데 그때마다 헤더를 다시 그리면,
    // 사람이 두고 있는 동안 판 밖이 계속 흔들린다.
    setPlaying(true);
    expect(notify).toHaveBeenCalledTimes(1);

    setPlaying(false);
    expect(notify).toHaveBeenCalledTimes(2);

    stop();
  });

  it('끊은 쪽에는 안 간다', () => {
    const notify = vi.fn();
    subscribePlaying(notify)();

    setPlaying(true);
    expect(notify).not.toHaveBeenCalled();
  });

  // `useSyncExternalStore` 가 렌더마다 이 값을 견준다. 매번 새 것을 돌려주면 무한히 다시
  // 그린다 — 원시값이라 저절로 맞지만, 나중에 객체로 바꾸는 순간 여기서 걸려야 한다.
  it('안 바뀌었으면 같은 것을 돌려준다', () => {
    expect(getPlaying()).toBe(getPlaying());
  });
});
