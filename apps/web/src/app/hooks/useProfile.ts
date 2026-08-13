import { useEffect, useState } from 'react';

import type { Profile, ProfileState } from '@/protocol/profile';

/**
 * 마이페이지의 데이터.
 *
 * **열 때마다 새로 부른다.** 방금 끝난 판이 전적에 들어 있어야 하고, 되짚기 목록이 같은
 * 이유로 그렇게 한다(App.tsx).
 *
 * **401을 오류로 그리지 않는다** — 로그인 안 한 것은 실패가 아니라 상태다.
 */
export function useProfile(active: boolean): ProfileState {
  const [state, setState] = useState<ProfileState>({ status: 'loading' });

  useEffect(() => {
    if (!active) return;
    const controller = new AbortController();
    setState({ status: 'loading' });

    fetch('/api/me/profile', { signal: controller.signal })
      .then(async (res) => {
        if (res.status === 401) {
          setState({ status: 'anonymous' });
          return;
        }
        if (!res.ok) {
          setState({ status: 'error' });
          return;
        }
        setState({ status: 'ready', profile: (await res.json()) as Profile });
      })
      .catch(() => {
        // 떠난 요청이면 아무것도 안 그린다 — 화면이 이미 없다.
        if (!controller.signal.aborted) setState({ status: 'error' });
      });

    return () => controller.abort();
  }, [active]);

  return state;
}
