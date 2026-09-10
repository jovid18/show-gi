import { useCallback, useEffect, useRef, useState } from 'react';

import type { ApiError } from '@/protocol/review';
import type { BestAttempt, BestResult, MateAttempt, MateResult, QuizPayload } from '@/protocol/quiz';
import { type Source, useFetch } from './useReview';

/** 문항 하나의 출처. 기다리기를 그만뒀는지가 더 붙는다 — 아래. */
export interface QuizSource extends Source<QuizPayload> {
  /**
   * 「아직 만드는 중」을 더는 기다리지 않는다.
   *
   * 「문항이 없다」와 다르다. 우리가 아는 것은 「정해진 동안 오지 않았다」뿐이라, 화면도
   * 딱 그만큼만 말해야 한다.
   */
  gaveUp: boolean;
}

/**
 * 그 판의 문항.
 *
 * 생성이 끝나지 않았으면 다시 묻는다. 문항은 판이 끝나면 줄에 서고 분석 워커가 수십 초 동안
 * 만들므로 (server/quiz_jobs.go), 판이 끝난 직후에 되짚기를 열면 `ready: false` 가 온다 —
 * 한 번 묻고 「問題はありません」을 그리면 그것이 거짓이 된다.
 *
 * 이 값과 판의 `analyzing` 은 다른 것을 기다린다. 저쪽은 평가치이고 이쪽은 문항이라,
 * 그래프가 다 차고 「解析しています」가 꺼진 뒤에도 여기는 아직 기다릴 수 있다.
 */
export function useQuiz(id: number): QuizSource {
  const { loaded, reload } = useFetch<QuizPayload>(`/api/games/${id}/quiz`);
  const [attempts, setAttempts] = useState(0);
  // 기다리기 시작한 시각. 횟수 대신 시간을 잰다 — 아래.
  const since = useRef<number | null>(null);

  // 판이 바뀌면 이 훅 전체가 새로 만들어진다 — App 이 `key` 로 판마다 새로 세운다. 여기서
  // 손으로 되돌리려 하면 안 된다: `id` 가 바뀐 그 렌더에는 `useFetch` 가 아직 앞 판의 답을
  // 갖고 있어서, 지운 자리가 같은 렌더에서 그 값으로 다시 채워진다.

  // 아직 기다리는 중인가.
  //
  // 한 번 실패한 것으로 끝내지 않는다. 요청 하나가 500을 받거나 네트워크가 한 번 끊긴
  // 것으로는 「문항이 오지 않는다」를 정할 수 없다. 다시 묻는 동안 직전 답이 그대로 있으므로
  // (useFetch 의 afterFailure) 이 값은 그 사이에 흔들리지 않는다.
  const waiting = loaded.state === 'ready' && !loaded.data.ready;

  // 다 만들어지면 멈추고, 오지 않으면 그것도 멈춘다. 「아직 만드는 중」은 영영 참일 수 있다 —
  // 이 코드 전에 끝난 판, 생성기가 없는 배포, 문항 판이 올라가 옛 행이 죽은 뒤가 전부 그렇다.
  // 계속 물으면 화면이 오지 않을 것을 기다리라고 말하게 된다.
  //
  // 끊는 기준은 물은 횟수 대신 기다린 시간이다. 세는 쪽은 「효과가 몇 번 다시
  // 도는가」에 매이는데 그것은 재려던 것과 다르고 실제로 어긋났다 — 개발 모드에서 5초
  // 간격이 22초에 9회로 돌았다.
  if (waiting && since.current === null) {
    since.current = Date.now();
  }
  if (!waiting) {
    since.current = null;
  }
  const gaveUp = waiting && since.current !== null && Date.now() - since.current >= QUIZ_WAIT_MS;

  // `attempts` 가 다시 걸어 주는 값이다. 나머지 셋은 폴링 도중에 바뀌지 않는다: `waiting` 은
  // 계속 참이고(다시 묻는 동안 직전 답이 그대로 있다) `gaveUp` 은 거짓이고 `reload` 는 고정이다.
  // 그래서 이것을 빼면 효과가 다시 돌지 않아 타이머가 한 번만 걸린다.
  //
  // 다시 걸어 주는 값은 의도한 것 하나로 고정한다. `waiting` 이 부르는 중에 흔들리는
  // 것에 폴링을 얹으면, 그 흔들림을 없애는 순간 폴링이 같이 멈춘다.
  useEffect(() => {
    if (!waiting || gaveUp) return;
    const timer = setTimeout(() => {
      setAttempts((n) => n + 1);
      reload();
    }, QUIZ_POLL_MS);
    return () => clearTimeout(timer);
  }, [waiting, gaveUp, attempts, reload]);

  // 「もう一度」는 세던 것도 되돌린다. 되돌리지 않으면 눌러도 요청 하나가 나가고 화면은
  // 그만둔 자리에 그대로 멈춰서, 버튼이 아무 일도 하지 않는 것처럼 보인다.
  const retry = useCallback(() => {
    since.current = null;
    setAttempts(0);
    reload();
  }, [reload]);

  return { loaded, reload: retry, gaveUp };
}

/**
 * 생성이 끝나기를 기다리는 간격.
 *
 * 詰み 트리가 수십 초 걸리므로(§53) 초 단위로 묻는 것은 낭비다. 5초면 사람이 「멈췄나」
 * 하고 새로고침하기 전에 도착한다.
 */
const QUIZ_POLL_MS = 5000;

/**
 * 얼마나 기다리나. 10분이다.
 *
 * 서버가 한 판을 자르는 시한이 5분이고(`quizTimeout`), 문항이 줄에 서므로
 * (server/quiz_jobs.go) 그 앞에 기다린 시간이 더 붙는다 — 앞 판 하나를 기다리면 그것만으로
 * 두 배다. 만드는 시한 하나로 잡으면 아직 정직하게 만들고 있는 판에 「오지 않았다」고
 * 말하게 된다.
 *
 * 두 배로 잡은 것이지 잰 값이 아니다 `[미확정]`. 늦게 끊는 쪽으로 기울여 둔 것은 여기
 * 「もう一度」가 있어서다 — 일찍 끊으면 사람이 그 버튼을 눌러야 하고, 늦게 끊으면
 * 기다리기만 하면 된다.
 */
const QUIZ_WAIT_MS = 10 * 60 * 1000;

/** 채점 한 번의 상태. 누른 뒤 답이 오기까지의 자리가 화면에 있어야 한다. */
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
    // 서버가 이유를 일본어로 준다(quiz.go). 읽지 못할 때만 우리 문구를 쓴다.
    const err = (await res.json().catch(() => null)) as ApiError | null;
    throw new Error(err?.message || FALLBACK_ERROR);
  }
  return (await res.json()) as Res;
}

/**
 * 요청 하나를 걸고 마지막 답만 남긴다.
 *
 * 떠난 요청은 버린다. 연타하면 응답이 순서대로 오지 않고, 늦게 온 것이 화면을 덮으면
 * 다른 수의 채점 결과가 지금 수의 것으로 남게 된다(useReview 의 같은 규약).
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
        // 직전 결과를 지우지 않는다. 지우면 판이 문제 국면으로 되돌아가는데 화면은
        // 이미 낸 수를 그대로 갖고 있어서, 다음 한 수가 그 국면에서만 합법인 수로 조합되어
        // 서버에 계속 거절된다 — 「最初から」를 누르기 전까지 문항이 잠긴다.
        setState((prev) => ({
          ...prev,
          pending: false,
          error: err instanceof Error ? err.message : FALLBACK_ERROR,
        }));
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
