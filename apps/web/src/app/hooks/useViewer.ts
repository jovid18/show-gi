import { useCallback, useEffect, useState } from 'react';

import type { MeResponse } from '@/protocol/auth';

/**
 * 지금 로그인한 사람.
 *
 * **한 번만 묻는다.** 로그인은 페이지를 통째로 떠났다 돌아오는 흐름이라(Google 리디렉션)
 * 돌아온 시점이 곧 새 문서이고, 그 뒤로 이 값이 바뀌는 자리는 로그아웃 하나뿐이다.
 *
 * 실패를 화면에 말하지 않는다 — 여기가 못 읽히면 「로그인 안 함」과 그림이 같고,
 * 대국은 그대로 되기 때문이다. 대국이 로그인에 매여 있지 않은 것이 이 판단의 근거다.
 */
export function useViewer(): { me: MeResponse; signOut: () => void } {
  const [me, setMe] = useState<MeResponse>({ enabled: false, user: null });

  useEffect(() => {
    const controller = new AbortController();
    fetch('/api/me', { signal: controller.signal })
      .then((res) => (res.ok ? (res.json() as Promise<MeResponse>) : null))
      .then((body) => {
        if (body) setMe(body);
      })
      .catch(() => {
        // 끊겼거나 떠난 요청이다. 위 주석 참조 — 화면에 말하지 않는다.
      });
    return () => controller.abort();
  }, []);

  const signOut = useCallback(() => {
    void fetch('/api/auth/logout', { method: 'POST' }).then(() => {
      // **화면 상태만 되돌리고 페이지를 새로 부르지 않는다.** 두는 중인 판이 있으면
      // WebSocket이 끊기고 그 판이 `abandoned`로 닫힌다. 이미 시작한 판은 시작할 때의
      // 주인으로 끝난다(ws.go) — 다음 판부터 익명이다.
      setMe((prev) => ({ ...prev, user: null }));
    });
  }, []);

  return { me, signOut };
}
