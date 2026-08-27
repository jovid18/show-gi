import { useCallback, useEffect, useRef, useState } from 'react';

import { leaveQueue, pollQueue, type QueueStatus } from '@/protocol/queue';

/** 큐에 서 있는 동안 다시 물어보는 주기. 서버가 걷어가는 시한보다 넉넉히 짧아야 한다. */
const POLL_MS = 2000;

export interface QueueState {
  /** 큐에 서 있는가. 눌러야 서고, 짝이 잡히거나 그만두면 내려간다. */
  searching: boolean;
  /** 지금 큐에 서 있는 사람 수(자기 포함). 아직 모르면 0. */
  waiting: number;
  /** 큐에 선 뒤로 흐른 시간(ms). 서버가 준 값이다. */
  waitedMs: number;
  /** 서버가 말한 실패. 로그인이 필요한 것도 여기로 온다. */
  error: string | null;
  start: () => void;
  stop: () => void;
}

/**
 * 대기열에 서서 짝이 잡히기를 기다린다.
 *
 * 폴링이다. WebSocket 이 아닌 이유는 기다리는 동안 서버가 할 말이 없기 때문이다 —
 * 짝은 상대가 큐에 서는 순간 생기고, 그것을 아는 것은 그 사람의 요청이다.
 *
 * 같은 호출 하나가 큐에 서기·heartbeat·짝짓기를 다 한다(`pollQueue`). 그래서 이 훅이
 * 하는 일은 그것을 주기적으로 부르고, 짝이 잡히면 `onMatched` 를 부르는 것뿐이다.
 *
 * 멈추는 것을 서버에 알린다. 안 알려도 만료가 걷어가지만, 그동안 상대에게는 내가 큐에
 * 있는 것으로 보이고 그 사이 잡힌 짝은 아무도 안 오는 방이 된다.
 */
export function useQueue(onMatched: (roomId: string) => void): QueueState {
  const [searching, setSearching] = useState(false);
  const [waiting, setWaiting] = useState(0);
  const [waitedMs, setWaitedMs] = useState(0);
  const [error, setError] = useState<string | null>(null);

  /**
   * 지금 화면이 짝을 받을 자리인가. 콜백을 ref 로 들고 있는 이유는 폴링을 그것 때문에
   * 다시 걸지 않기 위해서다 — 부모가 매번 새 함수를 넘기면 큐에서 나갔다 다시 서게 된다.
   */
  const matched = useRef(onMatched);
  matched.current = onMatched;

  useEffect(() => {
    if (!searching) return;

    const ac = new AbortController();
    let timer = 0;
    /** 이 효과가 아직 지금 것인가. 취소 뒤에 도착한 답이 상태를 덮는 것을 막는다. */
    let current = true;

    const ask = (): void => {
      void pollQueue(ac.signal)
        .then((status: QueueStatus) => {
          if (!current) return;
          if (status.status === 'matched') {
            // 큐에서 내려온다. 서버는 이 자리를 이미 지웠다(TakeQueueSeat) —
            // 여기서 `leaveQueue` 를 부르면 방금 잡힌 상대의 자리까지 흔든다.
            setSearching(false);
            matched.current(status.roomId);
            return;
          }
          setWaiting(status.waiting);
          setWaitedMs(status.waitedMs);
          timer = window.setTimeout(ask, POLL_MS);
        })
        .catch((e: Error) => {
          if (!current || ac.signal.aborted) return;
          // 재시도를 멈춘다. 로그인이 필요한 실패가 여기로 오는데, 그때 계속 부르면
          // 2초마다 401이 쌓인다 — 사람이 다시 누르는 것이 재시도다.
          setSearching(false);
          setError(e.message);
        });
    };
    ask();

    return () => {
      current = false;
      ac.abort();
      window.clearTimeout(timer);
    };
  }, [searching]);

  // 탭을 닫는 것도 큐에서 나가는 것이다. 언마운트 정리로는 안 잡힌다 — 창을 닫으면
  // React 가 정리를 안 부른다.
  useEffect(() => {
    if (!searching) return;
    const bye = (): void => void leaveQueue();
    window.addEventListener('pagehide', bye);
    return () => window.removeEventListener('pagehide', bye);
  }, [searching]);

  /**
   * 화면이 사라지는 것도 큐에서 나가는 것이다. 대국이 시작되면 이 화면이 내려가는데
   * (`GameScreen`), 그때 큐에 남아 있으면 만료까지의 몇 초 사이에 짝이 잡힐 수 있다 —
   * 그 방은 아무도 안 들어가고 상대는 60초를 기다린다.
   *
   * 값을 ref 로 본다. 의존성에 `searching` 을 넣으면 이 정리가 큐에 설 때마다 돌아서
   * 방금 큐에 선 것을 그 자리에서 취소한다.
   */
  const active = useRef(false);
  active.current = searching;
  useEffect(
    () => () => {
      if (active.current) void leaveQueue();
    },
    [],
  );

  const start = useCallback(() => {
    setError(null);
    setWaiting(0);
    setWaitedMs(0);
    setSearching(true);
  }, []);

  const stop = useCallback(() => {
    setSearching(false);
    void leaveQueue();
  }, []);

  return { searching, waiting, waitedMs, error, start, stop };
}
