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
  // 기다리기 시작한 시각. **세는 것이 아니라 재는 것**이다 — 아래.
  const since = useRef<number | null>(null);

  // **판이 바뀌면 이 훅이 통째로 다시 선다** — App 이 `key` 로 판마다 새로 세운다. 여기서
  // 손으로 되돌리려 하면 안 된다: `id` 가 바뀐 그 렌더에는 `useFetch` 가 아직 앞 판의 답을
  // 들고 있어서, 지운 자리가 같은 렌더에서 그 값으로 다시 채워진다.

  // 아직 기다리는 중인가.
  //
  // **한 번 실패한 것으로 끝내지 않는다.** 요청 하나가 500을 받거나 네트워크가 한 번 끊긴
  // 것은 「문항이 안 온다」가 아니다 — 그래서 부르는 중이든 실패했든 직전 답으로 판단한다
  // (아래에서 그 답을 화면에 그대로 세우는 것과 같은 이유다).
  const stillWaiting = last.current != null && !last.current.ready;
  const waiting = loaded.state === 'ready' ? !loaded.data.ready : stillWaiting;

  // **다 만들어지면 멈추고, 안 오면 그것도 멈춘다.** 「아직 만드는 중」은 영영 참일 수 있다 —
  // 이 코드 전에 끝난 판, 생성기가 없는 배포, 문항 판이 올라가 옛 행이 죽은 뒤가 전부 그렇다.
  // 계속 물으면 화면이 **오지 않을 것을 기다리라고** 말하게 된다.
  //
  // 끊는 기준은 **몇 번 물었나가 아니라 얼마나 기다렸나**다. 세는 쪽은 「효과가 몇 번 다시
  // 도는가」에 매이는데 그것은 뜻하는 바가 아니고 실제로 어긋났다 — 개발 모드에서 5초
  // 간격이 22초에 9회로 돌았다. 재는 쪽은 그 횟수가 무엇이든 서버가 스스로 자르는 시각과
  // 같은 자리에서 끊긴다.
  if (waiting && since.current === null) {
    since.current = Date.now();
  }
  if (!waiting) {
    since.current = null;
  }
  const gaveUp = waiting && since.current !== null && Date.now() - since.current >= QUIZ_WAIT_MS;

  // **`attempts` 가 다시 걸어 주는 값이다.** 나머지 셋은 폴링 도중에 안 바뀐다 — `waiting` 은
  // 계속 참이고(위에서 부르는 중에도 참으로 두었다) `gaveUp` 은 거짓이고 `reload` 는 고정이다.
  // 그래서 이것을 빼면 **효과가 다시 안 돌아 타이머가 한 번만 걸린다.**
  //
  // 한때 `waiting` 이 부르는 중에 거짓으로 떨어졌다 돌아오면서 그 일을 대신했는데, 화면이
  // 5초마다 「読み込み中…」으로 번쩍이는 것을 고치면서 그 흔들림이 사라졌다 — 폴링이 거기에
  // 얹혀 있던 것을 그때 못 봤다. 다시 걸어 주는 값을 **의도한 것 하나로** 못박는다.
  useEffect(() => {
    if (!waiting || gaveUp) return;
    const timer = setTimeout(() => {
      setAttempts((n) => n + 1);
      reload();
    }, QUIZ_POLL_MS);
    return () => clearTimeout(timer);
  }, [waiting, gaveUp, attempts, reload]);

  // **「もう一度」는 세던 것도 되돌린다.** 안 되돌리면 눌러도 요청 하나가 나가고 화면은
  // 그만둔 자리에 그대로 서서, 버튼이 아무 일도 안 하는 것처럼 보인다.
  const retry = useCallback(() => {
    since.current = null;
    setAttempts(0);
    reload();
  }, [reload]);

  // **다시 물을 때 직전 답을 그대로 둔다.** `useFetch` 는 부를 때마다 `loading` 으로
  // 돌아가는데, 그러면 「問題を作っています」가 5초마다 「読み込み中…」으로 번쩍인다 —
  // 화면이 그 두 상태를 통째로 다른 것으로 그리기 때문이다(QuizScreen).
  if (loaded.state === 'ready') {
    last.current = loaded.data;
  }
  // 부르는 중이든 한 번 실패했든, 직전 답이 있으면 그것을 그대로 세워 둔다.
  if (loaded.state !== 'ready' && last.current && !gaveUp) {
    return { loaded: { state: 'ready', data: last.current }, reload: retry, gaveUp };
  }

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
 * 얼마나 기다리나. 5분이다.
 *
 * **서버가 스스로 자르는 시한과 같은 값이다**(`quizTimeout`). 그보다 짧게 잡으면 아직
 * 정직하게 만들고 있는 판에 「안 왔다」고 말하게 되고, 길게 잡으면 서버가 이미 포기한
 * 뒤에도 기다린다 — 어느 쪽도 사실이 아니다.
 */
const QUIZ_WAIT_MS = 5 * 60 * 1000;

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
        // **직전 결과를 지우지 않는다.** 지우면 판이 문제 국면으로 되돌아가는데 화면은
        // 이미 낸 수를 그대로 들고 있어서, 다음 한 수가 그 국면에서만 합법인 수로 조합되어
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
