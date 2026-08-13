import { useCallback, useEffect, useRef, useState } from 'react';

import type { ApiError } from '@/protocol/review';
import type { BestAttempt, BestResult, MateAttempt, MateResult, QuizPayload } from '@/protocol/quiz';
import { type Source, useFetch } from './useReview';

/**
 * 그 판의 문항.
 *
 * **생성이 안 끝났으면 다시 묻는다.** 문항은 판이 끝나는 자리에서 수십 초 동안 만들어지므로
 * (server/ws.go generateQuiz), 판이 끝난 직후에 되짚기를 열면 `ready: false` 가 온다 —
 * 한 번 묻고 「問題はありません」을 그리면 그것이 거짓이 된다.
 */
export function useQuiz(id: number): Source<QuizPayload> {
  const { loaded, reload } = useFetch<QuizPayload>(`/api/games/${id}/quiz`);

  // **다 만들어지면 멈춘다.** 끝난 판의 문항은 다시 바뀌지 않으므로 계속 물을 이유가 없다.
  const pending = loaded.state === 'ready' && !loaded.data.ready;
  useEffect(() => {
    if (!pending) return;
    const timer = setTimeout(reload, QUIZ_POLL_MS);
    return () => clearTimeout(timer);
  }, [pending, reload]);

  // **다시 물을 때 직전 답을 그대로 둔다.** `useFetch` 는 부를 때마다 `loading` 으로
  // 돌아가는데, 그러면 「問題を作っています」가 5초마다 「読み込み中…」으로 번쩍인다 —
  // 화면이 그 두 상태를 통째로 다른 것으로 그리기 때문이다(QuizScreen).
  const last = useRef<QuizPayload | null>(null);
  if (loaded.state === 'ready') {
    last.current = loaded.data;
  }
  if (loaded.state === 'loading' && last.current) {
    return { loaded: { state: 'ready', data: last.current }, reload };
  }

  return { loaded, reload };
}

/**
 * 생성이 끝나기를 기다리는 간격.
 *
 * 詰み 트리가 수십 초 걸리므로(§53) 초 단위로 묻는 것은 낭비다. 5초면 사람이 「멈췄나」
 * 하고 새로고침하기 전에 도착한다.
 */
const QUIZ_POLL_MS = 5000;

/** 채점 한 번의 상태. **누른 뒤 답이 오기까지의 자리**가 화면에 있어야 한다. */
export interface Grading<T> {
  result: T | null;
  pending: boolean;
  error: string | null;
}

const FALLBACK_ERROR = '採点できませんでした。';

async function postJSON<Req, Res>(path: string, body: Req, signal: AbortSignal): Promise<Res> {
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal,
  });
  if (!res.ok) {
    // 서버가 이유를 일본어로 준다(quiz.go). 못 읽을 때만 우리 문구를 쓴다.
    const err = (await res.json().catch(() => null)) as ApiError | null;
    throw new Error(err?.message || FALLBACK_ERROR);
  }
  return (await res.json()) as Res;
}

/**
 * 요청 하나를 걸고 마지막 답만 남긴다.
 *
 * **떠난 요청은 버린다.** 연타하면 응답이 순서대로 오지 않고, 늦게 온 것이 화면을 덮으면
 * **다른 수의 채점 결과**가 지금 수의 것으로 서게 된다(useReview 의 같은 규약).
 */
function useGrader<Req, Res>(path: string): [Grading<Res>, (body: Req) => Promise<Res | null>, () => void] {
  const [state, setState] = useState<Grading<Res>>({ result: null, pending: false, error: null });
  const inflight = useRef<AbortController | null>(null);

  useEffect(() => () => inflight.current?.abort(), []);

  const send = useCallback(
    async (body: Req): Promise<Res | null> => {
      inflight.current?.abort();
      const controller = new AbortController();
      inflight.current = controller;
      setState((s) => ({ ...s, pending: true, error: null }));

      try {
        const res = await postJSON<Req, Res>(path, body, controller.signal);
        setState({ result: res, pending: false, error: null });
        return res;
      } catch (err: unknown) {
        if (controller.signal.aborted) return null;
        setState({ result: null, pending: false, error: err instanceof Error ? err.message : FALLBACK_ERROR });
        return null;
      }
    },
    [path],
  );

  const clear = useCallback(() => {
    inflight.current?.abort();
    setState({ result: null, pending: false, error: null });
  }, []);

  return [state, send, clear];
}

/** 詰み 문항의 채점. */
export function useMateGrader(
  id: number,
): [Grading<MateResult>, (body: MateAttempt) => Promise<MateResult | null>, () => void] {
  return useGrader<MateAttempt, MateResult>(`/api/games/${id}/quiz/mate`);
}

/** 「최선수는?」 문항의 채점. */
export function useBestGrader(
  id: number,
): [Grading<BestResult>, (body: BestAttempt) => Promise<BestResult | null>, () => void] {
  return useGrader<BestAttempt, BestResult>(`/api/games/${id}/quiz/best`);
}
