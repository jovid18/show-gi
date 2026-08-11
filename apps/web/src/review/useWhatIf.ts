import { useCallback, useEffect, useRef, useState } from 'react';

import type { ApiError, WhatIfNode, WhatIfRequest } from './protocol';

/**
 * 「そのとき、こう指していたら」 — 가정 수순 한 줄을 들고 있는다.
 *
 * **분기는 화면이 소유한다.** 서버는 매번 통째로 받아 한 걸음 진행시켜 줄 뿐이다
 * (whatif.go). 진행 중인 대국을 세션 goroutine이 소유하는 것과 반대편이고, 이유는
 * 여기에 되돌릴 상태가 없기 때문이다 — 끝난 판의 가정이라 아무도 안 잃는다.
 */
export interface WhatIf {
  /** 지금 서 있는 자리. null이면 분기에 들어가 있지 않다. */
  node: WhatIfNode | null;
  /** 서버가 답하는 중. 판은 직전 자리를 그대로 그린다 — 빈 판을 스치게 하지 않는다. */
  pending: boolean;
  error: string | null;
  /** 그 手数에서 분기를 연다. 수를 함께 주면 그 수부터 두고 시작한다(물러진 수가 그것이다). */
  enter: (ply: number, moves?: string[]) => void;
  /** 지금 자리에서 한 수 둔다. */
  play: (usi: string) => void;
  /** 한 수 물린다 — **사람의 수와 그에 대한 응수를 함께** 지운다. */
  back: () => void;
  /** 물릴 수가 남아 있는가. 뿌리에서 상대가 먼저 둔 자리는 물릴 것이 없다. */
  canBack: boolean;
  /** 실제로 둔 판으로 돌아간다. */
  close: () => void;
}

const FALLBACK_ERROR = 'この手順を試せませんでした。';

/** 같은 줄은 같은 자리다. 되돌아갔다 다시 와도 **그때 그 수순**이 서야 한다. */
function keyOf(req: WhatIfRequest): string {
  return `${req.ply}:${req.moves.join(' ')}`;
}

/**
 * 받은 자리 자신의 열쇠.
 *
 * **보낸 것과 받은 것이 다르다.** 상대 차례면 서버가 응수를 하나 더 붙여서 주므로,
 * 「사람의 수까지」로 물어본 자리는 「응수까지」로 돌아온다. 물리는 쪽은 받은 줄에서
 * 수를 빼서 묻기 때문에, 보낸 열쇠만 기억하면 **되돌아갈 때마다 반드시 헛친다.**
 *
 * 브라우저에서 실제로 그랬다 — 一手戻る 를 누를 때마다 후보의 평가치가 조금씩 달라졌고
 * (같은 국면·같은 깊이가 늘 같은 답을 주지는 않는다, 06-status.md §34 ②) 그건 판이
 * 말하는 사실이 아니라 두 번 물어본 흔적이다.
 */
function keyOfNode(node: WhatIfNode): string {
  return `${node.basePly}:${node.line.map((m) => m.usi).join(' ')}`;
}

async function postJSON<T>(path: string, body: unknown, signal: AbortSignal): Promise<T> {
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal,
  });
  if (!res.ok) {
    // 서버가 이유를 일본어로 준다(whatif.go). 못 읽을 때만 우리 문구를 쓴다.
    const err = (await res.json().catch(() => null)) as ApiError | null;
    throw new Error(err?.message || FALLBACK_ERROR);
  }
  return (await res.json()) as T;
}

export function useWhatIf(gameId: number): WhatIf {
  const [node, setNode] = useState<WhatIfNode | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  /**
   * 이미 받아 본 자리.
   *
   * **되돌아가면 분기가 다시 보여야 한다**(03-frontend.md §3). 그런데 같은 국면·같은
   * 깊이가 늘 같은 답을 주지는 않아서(06-status.md §34 ②) 다시 물으면 후보의 평가치가
   * 흔들린다 — 물러났다 나아가는 것만으로 숫자가 바뀌면 그건 판의 사실이 아니게 된다.
   */
  const seen = useRef(new Map<string, WhatIfNode>());
  /** 떠난 요청은 버린다. 빠르게 두면 응답이 순서대로 오지 않는다. */
  const latest = useRef(0);
  const inflight = useRef<AbortController | null>(null);

  // 다른 판으로 옮기면 이 분기는 남의 판의 것이다.
  useEffect(() => {
    seen.current = new Map();
    setNode(null);
    setError(null);
    setPending(false);
    return () => inflight.current?.abort();
  }, [gameId]);

  const request = useCallback(
    (req: WhatIfRequest) => {
      latest.current += 1;
      const mine = latest.current;
      inflight.current?.abort();
      inflight.current = null;
      setError(null);

      const hit = seen.current.get(keyOf(req));
      if (hit) {
        setPending(false);
        setNode(hit);
        return;
      }

      const controller = new AbortController();
      inflight.current = controller;
      setPending(true);

      postJSON<WhatIfNode>(`/api/games/${gameId}/whatif`, req, controller.signal)
        .then((data) => {
          if (latest.current !== mine) return;
          // 물어본 열쇠와 **받은 자리의 열쇠** 둘 다 기억한다. 둘은 같지 않다(keyOfNode).
          seen.current.set(keyOf(req), data);
          seen.current.set(keyOfNode(data), data);
          setNode(data);
          setPending(false);
        })
        .catch((err: unknown) => {
          if (controller.signal.aborted || latest.current !== mine) return;
          setError(err instanceof Error ? err.message : FALLBACK_ERROR);
          setPending(false);
        });
    },
    [gameId],
  );

  const enter = useCallback((ply: number, moves: string[] = []) => request({ ply, moves }), [request]);

  const play = useCallback(
    (usi: string) => {
      if (!node) return;
      request({ ply: node.basePly, moves: [...node.line.map((m) => m.usi), usi] });
    },
    [node, request],
  );

  /**
   * **「한 수」는 사람 기준이다.** 상대의 응수만 지우면 그 자리에서 다시 답을 받게 되고,
   * 그러면 물리는 것이 아니라 상대에게 한 번 더 묻는 것이 된다.
   */
  const back = useCallback(() => {
    if (!node) return;
    const line = node.line.map((m) => m.usi);
    if (node.line.at(-1)?.by === 'engine') line.pop();
    line.pop();
    request({ ply: node.basePly, moves: line });
  }, [node, request]);

  const close = useCallback(() => {
    latest.current += 1; // 늦게 오는 응답이 닫힌 분기를 다시 열지 않게 한다
    inflight.current?.abort();
    inflight.current = null;
    setNode(null);
    setError(null);
    setPending(false);
  }, []);

  return {
    node,
    pending,
    error,
    enter,
    play,
    back,
    canBack: node?.line.some((m) => m.by === 'human') ?? false,
    close,
  };
}
