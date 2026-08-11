import { useCallback, useEffect, useState } from 'react';

import type { ApiError, GameDetail, GameListResponse, GameSummary } from './protocol';

/**
 * 불러오는 것 하나의 상태.
 *
 * **실패를 `null`로 뭉개지 않는다.** 「아직 안 왔다」와 「못 읽었다」와 「하나도 없다」는
 * 화면에서 전부 다른 말을 해야 하는데, 하나로 합치면 빈 목록이 오류처럼 보이거나
 * 오류가 빈 목록처럼 보인다.
 */
export type Loaded<T> = { state: 'loading' } | { state: 'ready'; data: T } | { state: 'error'; message: string };

export interface Source<T> {
  loaded: Loaded<T>;
  /** 실패한 뒤 다시 부른다. 성공한 것을 새로 고칠 때도 같은 것을 쓴다. */
  reload: () => void;
}

/** 실패했을 때 화면에 나갈 문구. **서버가 준 일본어를 우선한다.** */
const FALLBACK_ERROR = '対局の記録を読み込めませんでした。';

async function getJSON<T>(path: string, signal: AbortSignal): Promise<T> {
  const res = await fetch(path, { signal });
  if (!res.ok) {
    // 서버가 이유를 일본어로 준다(review.go). 못 읽을 때만 우리 문구를 쓴다.
    const body = (await res.json().catch(() => null)) as ApiError | null;
    throw new Error(body?.message || FALLBACK_ERROR);
  }
  return (await res.json()) as T;
}

/**
 * 요청 하나를 걸고 결과를 상태로 준다.
 *
 * **떠난 요청은 버린다.** 목록에서 판을 빠르게 옮겨 다니면 응답이 순서대로 오지 않고,
 * 그때 늦게 온 것이 화면을 덮으면 **다른 판의 기보를 지금 판이라고 그린다.**
 */
function useFetch<T>(path: string): Source<T> {
  const [loaded, setLoaded] = useState<Loaded<T>>({ state: 'loading' });
  const [attempt, setAttempt] = useState(0);
  const reload = useCallback(() => setAttempt((n) => n + 1), []);

  useEffect(() => {
    const controller = new AbortController();
    setLoaded({ state: 'loading' });

    getJSON<T>(path, controller.signal)
      .then((data) => setLoaded({ state: 'ready', data }))
      .catch((err: unknown) => {
        // 우리가 취소한 것이다. 화면에 오류를 띄우면 사실이 아니다.
        if (controller.signal.aborted) return;
        setLoaded({ state: 'error', message: err instanceof Error ? err.message : FALLBACK_ERROR });
      });

    return () => controller.abort();
  }, [path, attempt]);

  return { loaded, reload };
}

/** 최근 대국 목록. */
export function useGameList(): Source<GameSummary[]> {
  const { loaded, reload } = useFetch<GameListResponse>('/api/games');
  if (loaded.state === 'ready') {
    return { loaded: { state: 'ready', data: loaded.data.games }, reload };
  }
  return { loaded, reload };
}

/** 한 판 전체. */
export function useGameDetail(id: number): Source<GameDetail> {
  return useFetch<GameDetail>(`/api/games/${id}`);
}
