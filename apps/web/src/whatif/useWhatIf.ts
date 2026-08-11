import { useCallback, useEffect, useRef, useState } from 'react';

import type { WhatIfNode, WhatIfRequest } from './protocol';

/**
 * 「そのとき、こう指していたら」 — 가정 수순 한 줄을 들고 있는다.
 *
 * **분기는 화면이 소유한다.** 서버는 매번 통째로 받아 그 국면 하나를 답해 줄 뿐이고,
 * 한 수도 대신 두지 않는다. 되돌릴 상태가 없어서 그럴 수 있다 — 끝난 판의 가정이든
 * 물러진 수 뒤의 가정이든 아무도 안 잃는다.
 *
 * **오가는 길은 두 가지다.** 되짚는 판은 HTTP, 대국 중의 블런더 화면은 그 대국의
 * WebSocket이다. 그 차이를 `send` 하나로 밀어내서 **장치는 한 벌**로 둔다.
 */
export interface WhatIf {
  /** 지금 서 있는 자리. null이면 아직 아무것도 못 받았다. */
  node: WhatIfNode | null;
  pending: boolean;
  error: string | null;
  /** 그 手数의 국면을 묻는다. 수를 함께 주면 그것부터 둔 자리다(물러진 수가 그렇다). */
  at: (ply: number, moves?: string[]) => void;
  /** 지금 자리에서 한 수 둔다. **어느 쪽 차례든** 둘 수 있다. */
  play: (usi: string) => void;
  /** 한 수 물린다. */
  back: () => void;
  /** 분기 전으로 돌아간다 — 갈라져 나온 그 手数다. */
  toRoot: () => void;
  /** 분기에 들어가 있는가. 한 수라도 뒀으면 그렇다. */
  branching: boolean;
  /** 들고 있는 것을 버린다. 화면을 닫을 때 쓴다. */
  clear: () => void;
}

/** 요청 하나를 실어 보내는 길. HTTP든 WebSocket이든 이 모양이면 된다. */
export type Send = (req: WhatIfRequest, signal: AbortSignal) => Promise<WhatIfNode>;

const FALLBACK_ERROR = 'この手順を試せませんでした。';

/**
 * 같은 줄은 같은 자리다.
 *
 * **보낸 것과 받은 것의 열쇠가 같다.** 서버가 응수를 대신 두던 때는 그렇지 않아서
 * (보낸 줄에 한 수가 더 붙어 왔다) 물릴 때마다 캐시가 헛쳤고, 그러면 같은 자리의 후보
 * 평가치가 조금씩 달라졌다 — 같은 국면·같은 깊이가 늘 같은 답을 주지는 않기 때문이다
 * (06-status.md §34 ②). 대신 두지 않기로 하면서 그 버그가 통째로 사라졌다.
 */
function keyOf(req: WhatIfRequest): string {
  return `${req.ply}:${req.moves.join(' ')}`;
}

/**
 * `send` 는 매 렌더마다 새로 만들어져도 된다 — ref로 잡으므로 아래 콜백이 흔들리지 않는다.
 * `resetKey` 가 바뀌면 들고 있던 것을 버린다(다른 판·다른 연결의 분기다).
 */
export function useWhatIf(send: Send, resetKey: unknown): WhatIf {
  const [node, setNode] = useState<WhatIfNode | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const sendRef = useRef(send);
  sendRef.current = send;

  /**
   * 이미 받아 본 자리.
   *
   * **되돌아가면 그때 그 자리가 다시 보여야 한다**(03-frontend.md §3). 다시 물으면 후보의
   * 평가치가 흔들리므로(§34 ②), 물러났다 나아가는 것만으로 숫자가 바뀌면 그건 판의
   * 사실이 아니게 된다. 서버 쪽 `positions` 캐시가 같은 일을 하지만, 여기가 있으면
   * 왕복 자체가 없어서 **누른 즉시** 판이 선다.
   */
  const seen = useRef(new Map<string, WhatIfNode>());
  /** 떠난 요청은 버린다. 빠르게 두면 응답이 순서대로 오지 않는다. */
  const latest = useRef(0);
  const inflight = useRef<AbortController | null>(null);

  useEffect(() => {
    seen.current = new Map();
    setNode(null);
    setError(null);
    setPending(false);
    return () => inflight.current?.abort();
  }, [resetKey]);

  const at = useCallback((ply: number, moves: string[] = []) => {
    const req = { ply, moves };
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

    sendRef
      .current(req, controller.signal)
      .then((data) => {
        if (latest.current !== mine) return;
        seen.current.set(keyOf(req), data);
        setNode(data);
        setPending(false);
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted || latest.current !== mine) return;
        setError(err instanceof Error ? err.message : FALLBACK_ERROR);
        setPending(false);
      });
  }, []);

  const play = useCallback(
    (usi: string) => {
      if (!node) return;
      at(node.basePly, [...node.line.map((m) => m.usi), usi]);
    },
    [node, at],
  );

  const back = useCallback(() => {
    if (!node?.line.length) return;
    at(
      node.basePly,
      node.line.slice(0, -1).map((m) => m.usi),
    );
  }, [node, at]);

  const toRoot = useCallback(() => {
    if (!node) return;
    at(node.basePly);
  }, [node, at]);

  const clear = useCallback(() => {
    latest.current += 1; // 늦게 오는 응답이 닫힌 화면을 다시 열지 않게 한다
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
    at,
    play,
    back,
    toRoot,
    branching: (node?.line.length ?? 0) > 0,
    clear,
  };
}
