import { useCallback, useEffect, useRef, useState } from 'react';

import type { WhatIfNode, WhatIfRequest } from '@/protocol/whatif';

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
  /** 분기 전으로 돌아간다 — 갈라져 나온 그 手数, 정확히는 **바닥**이다. */
  toRoot: () => void;
  /**
   * 줄이 그 길이였을 때의 값 — **수마다의 cp**가 여기서 나온다.
   *
   * **다시 묻지 않는다.** 지나온 자리는 이미 받아 뒀으므로 꺼내 오면 되고, 아직 안 가 본
   * 자리는 `null` 이다 — 없는 값을 지어내지 않는다.
   */
  evalOf: (lineLength: number) => { cp: number | undefined; mateIn: number | undefined } | null;
  /** 분기에 들어가 있는가. **바닥 위로** 한 수라도 뒀으면 그렇다. */
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

/** 바닥이 없는 분기. 되짚기가 이쪽이다 — 어느 手数에서든 아무것도 안 깔고 시작한다. */
const NO_FLOOR: readonly string[] = [];

/**
 * `send` 는 매 렌더마다 새로 만들어져도 된다 — ref로 잡으므로 아래 콜백이 흔들리지 않는다.
 * `resetKey` 가 바뀌면 들고 있던 것을 버린다(다른 판·다른 연결의 분기다).
 *
 * `floor` 는 **줄에서 뺄 수 없는 앞머리**다. 대국 중에는 물러진 수 하나가 여기 들어간다 —
 * 그 앞은 곧 **지금 다시 둘 국면**이라, 거기까지 물러나면 이 장치가 최선수 셋으로 「지금
 * 어떻게 두라」를 답하게 된다(01-core.md §7). 서버도 같은 벽을 갖고 있고(ws.go 의
 * `branchRoot`), **두 벌인 것이 맞다** — 화면 쪽은 버튼을 안 그리는 일이고 서버 쪽은
 * 요청을 거절하는 일이라, 하나가 뚫려도 다른 하나가 남는다.
 */
export function useWhatIf(send: Send, resetKey: unknown, floor: readonly string[] = NO_FLOOR): WhatIf {
  const [node, setNode] = useState<WhatIfNode | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const sendRef = useRef(send);
  sendRef.current = send;

  // 아래 콜백들이 매 렌더마다 새로 만들어지지 않게 ref로 잡는다. `send` 와 같은 이유다 —
  // 이 값이 의존성에 들어가면 배열 identity 하나로 「같은 자리를 두 번 묻는」 고리가 산다.
  const floorRef = useRef(floor);
  floorRef.current = floor;

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
  /**
   * 지금 답을 기다리는 요청의 열쇠.
   *
   * **같은 자리를 두 번 묻지 않는다.** 그냥 낭비가 아니라 **깨진다** — 대국 쪽 길은 한
   * 연결에 한 번만 돌리므로(ws.go 의 슬롯) 두 번째 요청이 `busy` 로 튕기고, 그 에러가
   * 먼저 도착해 화면에 뜬 다음 첫 응답은 주인을 잃고 버려진다. 개입 카드가 뜬 순간
   * 실제로 그렇게 됐다(StrictMode가 효과를 두 번 돌린다).
   */
  const asked = useRef<string | null>(null);

  useEffect(() => {
    seen.current = new Map();
    asked.current = null;
    setNode(null);
    setError(null);
    setPending(false);
    return () => inflight.current?.abort();
  }, [resetKey]);

  const at = useCallback((ply: number, moves: string[] = []) => {
    const req = { ply, moves };
    const key = keyOf(req);
    if (asked.current === key) return; // 이미 그 자리를 묻고 있다

    latest.current += 1;
    const mine = latest.current;
    inflight.current?.abort();
    inflight.current = null;
    setError(null);

    const hit = seen.current.get(key);
    if (hit) {
      asked.current = null;
      setPending(false);
      setNode(hit);
      return;
    }

    const controller = new AbortController();
    inflight.current = controller;
    asked.current = key;
    setPending(true);

    sendRef
      .current(req, controller.signal)
      .then((data) => {
        if (asked.current === key) asked.current = null;
        // **캐시는 늦게 온 응답도 받는다.** 버리는 것은 화면에 그리는 일뿐이고, 잰 값을
        // 버릴 이유는 없다 — 다음에 그 자리로 돌아오면 그대로 쓴다.
        seen.current.set(key, data);
        if (latest.current !== mine) return;
        setNode(data);
        setPending(false);
      })
      .catch((err: unknown) => {
        if (asked.current === key) asked.current = null;
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
    // 바닥까지 왔으면 더 물릴 것이 없다. **`length` 만 보면 바닥을 지나쳐 물린다.**
    if (!node || node.line.length <= floorRef.current.length) return;
    at(
      node.basePly,
      node.line.slice(0, -1).map((m) => m.usi),
    );
  }, [node, at]);

  const toRoot = useCallback(() => {
    if (!node) return;
    at(node.basePly, [...floorRef.current]);
  }, [node, at]);

  /**
   * 지나온 자리의 값.
   *
   * **렌더 중에 ref를 읽는다.** 캐시는 늘기만 하고 새 값이 들어올 때마다 `setNode` 가
   * 따라오므로(위) 화면이 뒤처지지 않는다 — 같은 것을 상태로 한 벌 더 들고 있으면
   * 둘이 어긋날 자리만 생긴다.
   */
  const evalOf = useCallback(
    (lineLength: number) => {
      // **지금 줄보다 긴 자리는 모른다.** `slice` 는 넘치면 조용히 짧게 잘라 주므로,
      // 막지 않으면 아직 안 가 본 장면에 **직전 장면의 값**이 붙는다 — 개입 카드에서
      // 물러진 수와 그 다음 수가 같은 숫자로 나왔다(브라우저에서 그 그림을 봤다).
      if (!node || lineLength > node.line.length) return null;
      const line = node.line.slice(0, lineLength).map((m) => m.usi);
      const found = seen.current.get(keyOf({ ply: node.basePly, moves: line }));
      return found ? { cp: found.evalCp, mateIn: found.mateIn } : null;
    },
    [node],
  );

  const clear = useCallback(() => {
    latest.current += 1; // 늦게 오는 응답이 닫힌 화면을 다시 열지 않게 한다
    inflight.current?.abort();
    inflight.current = null;
    asked.current = null;
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
    evalOf,
    branching: (node?.line.length ?? 0) > floor.length,
    clear,
  };
}
