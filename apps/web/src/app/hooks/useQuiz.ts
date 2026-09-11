import { useCallback, useEffect, useRef, useState } from 'react';

import type { ApiError } from '@/protocol/review';
import type { BestAttempt, BestResult, MateAttempt, MateResult, QuizPayload } from '@/protocol/quiz';
import { type Source, useFetch } from './useReview';

/** 문항 하나의 출처. 기다리기를 그만뒀는지가 더 붙는다. */
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
 * 생성이 끝나지 않았으면 다시 묻는다. 문항은 판이 끝나면 큐에 들어가고 분석 워커가 수십 초
 * 동안 만들므로(server/quiz_jobs.go), 한 번 묻고 「問題はありません」을 그리면 거짓이 된다.
 *
 * 이 값과 판의 `analyzing` 은 다른 것을 기다린다. 저쪽은 평가치이고 이쪽은 문항이라,
 * 그래프가 다 차고 「解析しています」가 꺼진 뒤에도 여기는 아직 기다릴 수 있다.
 */
export function useQuiz(id: number): QuizSource {
  const { loaded, reload } = useFetch<QuizPayload>(`/api/games/${id}/quiz`);
  const [attempts, setAttempts] = useState(0);
  // 기다리기 시작한 시각. 횟수 대신 시간을 잰다.
  const since = useRef<number | null>(null);
  // 「줄에 없다」를 처음 들은 시각. 위와 따로 잰다.
  const denied = useRef<number | null>(null);

  // 판이 바뀌면 이 훅 전체가 새로 만들어진다(App 이 `key` 로 판마다 새로 세운다). 여기서
  // 손으로 되돌리면 안 된다: `id` 가 바뀐 그 렌더에는 `useFetch` 가 아직 앞 판의 답을 갖고
  // 있어서, 지운 자리가 같은 렌더에서 그 값으로 다시 채워진다.

  // 아직 기다리는 중인가. 한 번 실패한 것으로 끝내지 않는다. 다시 묻는 동안 직전 답이 그대로
  // 있으므로(useFetch 의 afterFailure) 이 값은 그 사이에 흔들리지 않는다.
  //
  // 「아직 만드는 중」은 영영 참일 수 있다. 이 코드 전에 끝난 판과, 문항 판이 올라가 옛 행이
  // 죽은 뒤가 그렇다. 그것을 서버가 말한다(`queued`).
  const pending = loaded.state === 'ready' && !loaded.data.ready;
  // 없는 것은 거짓이 아니다. 배포가 도는 동안 옛 태스크가 이 칸 없이 답하고, 그때는
  // 물어볼 것이 없으므로 시간으로만 끊는다.
  const said = pending ? loaded.data.queued : undefined;

  // 끊는 기준은 물은 횟수 대신 기다린 시간이다. 세는 쪽은 「효과가 몇 번 다시 도는가」에
  // 매인다. 개발 모드에서 5초 간격이 22초에 9회로 돌았다.
  if (pending && since.current === null) {
    since.current = Date.now();
  }
  if (!pending) {
    since.current = null;
  }
  const waited = since.current === null ? 0 : Date.now() - since.current;

  // 한 번의 「줄에 없다」로 그만두지 않는다(QUIZ_MIN_WAIT_MS). 그 시각을 따로 잰다. 전체
  // 기다린 시간으로 재면 몇 분 기다린 뒤의 첫 거짓이 곧바로 끊는다.
  if (said === false && denied.current === null) {
    denied.current = Date.now();
  }
  if (said !== false) {
    denied.current = null;
  }
  const deniedFor = denied.current === null ? 0 : Date.now() - denied.current;

  const waiting = pending && waited < QUIZ_WAIT_MS && !(said === false && deniedFor >= QUIZ_MIN_WAIT_MS);
  const gaveUp = pending && !waiting;

  // `attempts` 가 다시 걸어 주는 값이다. 나머지 셋은 폴링 도중에 바뀌지 않는다: `waiting` 은
  // 계속 참이고 `gaveUp` 은 거짓이고 `reload` 는 고정이다. 이것을 빼면 효과가 다시 돌지 않아
  // 타이머가 한 번만 걸린다.
  useEffect(() => {
    if (!waiting || gaveUp) return;
    const timer = setTimeout(() => {
      setAttempts((n) => n + 1);
      reload();
    }, pollDelay(waited));
    return () => clearTimeout(timer);
    // waited 는 다시 걸어 주는 값에 넣지 않는다. 매 렌더에 바뀌는 값이라 넣으면 타이머가
    // 계속 다시 걸려 아무것도 끝나지 않는다. 다음 간격은 다음 폴링이 도착할 때 `attempts` 가
    // 바뀌면서 그 렌더의 값으로 다시 정해진다.
  }, [waiting, gaveUp, attempts, reload]);

  // 「もう一度」는 세던 것도 되돌린다. 되돌리지 않으면 눌러도 화면이 그만둔 자리에 그대로
  // 멈춰서 버튼이 아무 일도 하지 않는 것처럼 보인다.
  const retry = useCallback(() => {
    since.current = null;
    denied.current = null;
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
 * 다음에 물어보기까지 얼마나 둘 것인가.
 *
 * 오래 기다릴수록 뜸해진다. 상한이 30분인데(QUIZ_WAIT_MS) 5초로만 물으면 한 사람이 판
 * 하나에 360번을 묻고, 그 요청 하나가 기보 전체를 읽는다(reviewHandler.record).
 *
 * 앞은 촘촘하다. 문항은 대개 수십 초 안에 오고, 그때 사람이 화면 앞에 있다.
 */
function pollDelay(waited: number): number {
  if (waited < 60 * 1000) return QUIZ_POLL_MS;
  if (waited < 5 * 60 * 1000) return 3 * QUIZ_POLL_MS;
  return 6 * QUIZ_POLL_MS;
}

/**
 * 「온다」를 들으면서 이만큼 지나면 그만 묻는다.
 *
 * 끊는 것은 `queued` 가 먼저 하고, 이 값은 그 뒤에 남는 마지막 자물쇠다. 상한까지 실패한
 * 판도 청소가 지울 때까지 큐에 남으므로(server/quiz_jobs.go) 그 말만 믿으면 몇 시간을 묻는다.
 *
 * 만드는 시한(5분)만으로 잡을 수 없다. `queued` 는 가져온 판을 재는 동안에도 참이고
 * (server/quiz.go), 판이 길면 그 재기가 분 단위로 간다.
 *
 * 잰 값이 아니다 `[미확정]`. 늦게 끊는 쪽으로 기울인 것은 여기 「もう一度」가 있어서다.
 */
const QUIZ_WAIT_MS = 30 * 60 * 1000;

/**
 * 줄에 없다고 할 때 그래도 기다리는 시간.
 *
 * `queued` 가 잠깐 거짓일 수 있다. 대국이 끝나면 총평이 먼저 가고 큐에 세우는 것이 그
 * 뒤이고(server/ws.go), 세우기가 실패한 판은 줄 없이 그 자리에서 만들어진다. 한 번의
 * 거짓으로 그만두면 그 두 자리에서 화면이 오는 것을 오지 않았다고 말한다.
 *
 * 잰 값이 아니라 「사람이 새로고침하기 전」과 「없는 것을 기다리게 하지 않는다」 사이에서
 * 고른 것이다 `[미확정]`.
 */
const QUIZ_MIN_WAIT_MS = 60 * 1000;

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
 * 떠난 요청은 버린다. 연타하면 응답이 순서대로 오지 않고, 늦게 온 것이 화면을 덮으면 다른
 * 수의 채점 결과가 지금 수의 것으로 남는다(useReview 의 같은 규약).
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
        // 직전 결과를 지우지 않는다. 지우면 판이 문제 국면으로 되돌아가는데 화면은 이미 낸
        // 수를 그대로 갖고 있어서, 다음 한 수가 그 국면에서만 합법인 수로 조합되어 서버에
        // 계속 거절된다. 「最初から」를 누르기 전까지 문항이 잠긴다.
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
