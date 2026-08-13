import { useCallback, useEffect, useRef, useState } from 'react';

import type { ApiError } from '@/protocol/review';
import type { BestAttempt, BestResult, MateAttempt, MateResult, QuizPayload } from '@/protocol/quiz';
import { type Source, useFetch } from './useReview';

/** 문항 하나의 출처. **기다리기를 그만뒀는지**가 더 붙는다 — 아래. */
export interface QuizSource extends Source<QuizPayload> {
  /**
   * 「아직 만드는 중」을 더는 안 기다린다.
   *
   * **「문항이 없다」와 다르다.** 우리가 아는 것은 「정해진 동안 안 왔다」뿐이라, 화면도
   * 딱 그만큼만 말해야 한다.
   */
  gaveUp: boolean;
}

/**
 * 그 판의 문항.
 *
 * **생성이 안 끝났으면 다시 묻는다.** 문항은 판이 끝나는 자리에서 수십 초 동안 만들어지므로
 * (server/ws.go generateQuiz), 판이 끝난 직후에 되짚기를 열면 `ready: false` 가 온다 —
 * 한 번 묻고 「問題はありません」을 그리면 그것이 거짓이 된다.
 */
export function useQuiz(id: number): QuizSource {
  const { loaded, reload } = useFetch<QuizPayload>(`/api/games/${id}/quiz`);
  const [attempts, setAttempts] = useState(0);
  const last = useRef<QuizPayload | null>(null);

  // **판이 바뀌면 처음부터다.** 주소만 갈아 끼우면 이 컴포넌트가 그대로 살아 있으므로
  // (App 이 `id` 만 바꿔 넘긴다) 세는 값도 아래의 직전 답도 앞 판의 것이 남는다.
  const seen = useRef(id);
  if (seen.current !== id) {
    seen.current = id;
    last.current = null;
    if (attempts !== 0) setAttempts(0);
  }

  // **다 만들어지면 멈춘다.** 끝난 판의 문항은 다시 바뀌지 않으므로 계속 물을 이유가 없다.
  //
  // **안 오면 그것도 멈춘다.** 「아직 만드는 중」은 영영 참일 수 있다 — 이 코드 전에 끝난
  // 판, 생성기가 없는 배포, 판이 올라가 옛 행이 죽은 뒤가 전부 그렇다. 계속 물으면 화면이
  // 오지 않을 것을 기다리라고 말하게 된다.
  const waiting = loaded.state === 'ready' && !loaded.data.ready;
  const gaveUp = waiting && attempts >= QUIZ_POLL_MAX;

  useEffect(() => {
    if (!waiting || gaveUp) return;
    const timer = setTimeout(() => {
      setAttempts((n) => n + 1);
      reload();
    }, QUIZ_POLL_MS);
    return () => clearTimeout(timer);
  }, [waiting, gaveUp, reload]);

  // **다시 물을 때 직전 답을 그대로 둔다.** `useFetch` 는 부를 때마다 `loading` 으로
  // 돌아가는데, 그러면 「問題を作っています」가 5초마다 「読み込み中…」으로 번쩍인다 —
  // 화면이 그 두 상태를 통째로 다른 것으로 그리기 때문이다(QuizScreen).
  if (loaded.state === 'ready') {
    last.current = loaded.data;
  }
  if (loaded.state === 'loading' && last.current) {
    return { loaded: { state: 'ready', data: last.current }, reload, gaveUp };
  }

  return { loaded, reload, gaveUp };
}

/**
 * 생성이 끝나기를 기다리는 간격.
 *
 * 詰み 트리가 수십 초 걸리므로(§53) 초 단위로 묻는 것은 낭비다. 5초면 사람이 「멈췄나」
 * 하고 새로고침하기 전에 도착한다.
 */
const QUIZ_POLL_MS = 5000;

/**
 * 몇 번까지 기다리나. 5초 × 12 = 1분이다.
 *
 * 실측으로 6手 판이 1초 안쪽이었고(§53), 오래 걸리는 쪽은 詰み 트리라 그것도 분 단위는
 * 아니다. 1분을 넘겨도 안 왔으면 **오지 않을 쪽**이 훨씬 그럴듯하다 — 옛 판이거나,
 * 생성기가 없는 배포이거나, 문항 판이 올라가 옛 행이 죽은 뒤다.
 */
const QUIZ_POLL_MAX = 12;

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
