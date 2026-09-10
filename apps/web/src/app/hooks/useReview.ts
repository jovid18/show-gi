import { useCallback, useEffect, useRef, useState } from 'react';

import type { GameSummary as PostGameSummary } from '@/protocol/game';
import type { ApiError, GameDetail, GameListResponse, GameSummary } from '@/protocol/review';

/**
 * 불러오는 것 하나의 상태.
 *
 * 실패를 `null`로 뭉개지 않는다. 「아직 오지 않았다」와 「읽지 못했다」와 「하나도 없다」는
 * 화면에서 전부 다른 말을 해야 하는데, 하나로 합치면 빈 목록이 오류처럼 보이거나
 * 오류가 빈 목록처럼 보인다.
 */
export type Loaded<T> = { state: 'loading' } | { state: 'ready'; data: T } | { state: 'error'; message: string };

export interface Source<T> {
  loaded: Loaded<T>;
  /** 실패한 뒤 다시 부른다. 성공한 것을 새로 고칠 때도 같은 것을 쓴다. */
  reload: () => void;
}

/** 실패했을 때 화면에 나갈 문구. 서버가 준 일본어를 우선한다. */
const FALLBACK_ERROR = '対局の記録を読み込めませんでした。';

async function getJSON<T>(path: string, signal: AbortSignal): Promise<T> {
  const res = await fetch(path, { signal });
  if (!res.ok) {
    // 서버가 이유를 일본어로 준다(review.go). 읽지 못할 때만 우리 문구를 쓴다.
    const body = (await res.json().catch(() => null)) as ApiError | null;
    throw new Error(body?.message || FALLBACK_ERROR);
  }
  return (await res.json()) as T;
}

/**
 * 요청 하나를 걸고 결과를 상태로 준다.
 *
 * 떠난 요청은 버린다. 목록에서 판을 빠르게 옮겨 다니면 응답이 순서대로 오지 않고,
 * 그때 늦게 온 것이 화면을 덮으면 다른 판의 기보를 지금 판이라고 그린다.
 *
 * 퀴즈도 이걸 쓴다(useQuiz) — 「아직 오지 않았다 / 읽지 못했다 / 하나도 없다」를 따로 두는 규약이
 * 두 벌이 되면 한쪽에서만 빈 목록이 오류처럼 보인다.
 *
 * `reload` 로 다시 물을 때는 직전 답을 그대로 들고 있는다. 폴링하는 화면이 둘이라
 * (되짚기의 분석 중, 퀴즈의 생성 중) 부르는 쪽마다 그 규약을 다시 짜면 어긋난다.
 * 무엇을 남기고 무엇을 비우는지는 `startingLoad`·`afterFailure` 가 정한다.
 */
export function useFetch<T>(path: string): Source<T> {
  const [loaded, setLoaded] = useState<Loaded<T>>({ state: 'loading' });
  const [attempt, setAttempt] = useState(0);
  const reload = useCallback(() => setAttempt((n) => n + 1), []);
  // 지금 화면에 그려진 것이 어느 주소의 답인가. 같은 주소를 다시 묻는 것과 다른 판으로
  // 옮겨 가는 것을 이 값 하나가 가른다.
  const shown = useRef<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    const samePath = shown.current === path;
    shown.current = path;
    setLoaded((prev) => startingLoad(prev, samePath));

    getJSON<T>(path, controller.signal)
      .then((data) => setLoaded({ state: 'ready', data }))
      .catch((err: unknown) => {
        // 우리가 취소한 것이다. 화면에 오류를 띄우면 사실과 어긋난다.
        if (controller.signal.aborted) return;
        setLoaded((prev) => afterFailure(prev, err instanceof Error ? err.message : FALLBACK_ERROR));
      });

    return () => controller.abort();
  }, [path, attempt]);

  return { loaded, reload };
}

/**
 * 요청을 새로 걸 때 화면에 남길 것.
 *
 * 같은 주소를 다시 묻는 동안에는 직전 답을 그대로 둔다. `loading` 으로 되돌리면 그 자리를
 * 그리던 컴포넌트가 언마운트되고, 되짚기에서는 고른 手数와 가정 수순이 거기서 사라진다
 * (journal §136).
 *
 * 주소가 바뀌면 비운다. 새 판을 받는 동안 앞 판의 기보가 남아 있으면, 화면이 남의 판을
 * 지금 판이라고 말하게 된다.
 */
export function startingLoad<T>(prev: Loaded<T>, samePath: boolean): Loaded<T> {
  if (samePath && prev.state === 'ready') return prev;
  return { state: 'loading' };
}

/**
 * 실패했을 때 화면에 남길 것.
 *
 * 그리던 것이 있으면 남긴다. 폴링 한 번이 끊긴 것으로 판을 지우면, 사람은 자기가 무엇을
 * 잘못 눌렀는지 모른 채 되짚기를 처음부터 다시 연다.
 *
 * 처음부터 아무것도 없었으면 오류다. 그때만 빈 화면보다 이유가 낫다.
 */
export function afterFailure<T>(prev: Loaded<T>, message: string): Loaded<T> {
  if (prev.state === 'ready') return prev;
  return { state: 'error', message };
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

/**
 * 그 판의 총평. 기보와 따로 받는다 — 판을 먼저 그리고 총평을 뒤에 채운다. 대국에서
 * 스냅샷과 총평이 갈라져 오는 것과 같은 자리다(§49).
 *
 * 돌아오는 것은 `protocol/game.ts` 의 `GameSummary` — `protocol/review.ts` 의 같은 이름
 * (목록 한 줄)과 다른 타입이다. 그래서 여기서 이름을 바꿔 받는다.
 */
export function useGameSummary(id: number): Source<PostGameSummary> {
  return useFetch<PostGameSummary>(`/api/games/${id}/summary`);
}

/**
 * 엔진이 살아 있는가.
 *
 * 되짚기는 이 값을 보지 않는다. 기록만 있으면 도는 화면이고, 그것이 리뷰와 대국의 조건이
 * 갈리는 자리다(server.go). 이 값을 묻는 것은 가정 수순 하나 때문이다 — 눌러도
 * 503만 돌아오는 버튼은 「고장 났다」로 읽히므로, 그 자리를 미리 닫고 이유를 적는다.
 *
 * `null`은 아직 모른다이다. 물어보지 못했다고 없는 것으로 치면, /healthz 만 실패한
 * 상황에서 멀쩡한 기능 전체가 사라진다 — 그때는 열어 두고 서버가 답하게 둔다.
 */
export function useEngineReady(): boolean | null {
  const { loaded } = useFetch<{ engine?: boolean }>('/healthz');
  return loaded.state === 'ready' ? (loaded.data.engine ?? false) : null;
}
