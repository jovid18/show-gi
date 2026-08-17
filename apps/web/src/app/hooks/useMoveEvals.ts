import { useEffect, useRef, useState } from 'react';

import { httpSend } from '@/libs/whatif/http';

/**
 * 다시 잰 값 하나.
 *
 * **플레이어 관점이다** — 서버가 `whatifNode.evalCp` 를 그렇게 내보낸다(branch.go 의
 * `playerCp`). 후보(`WhatIfCandidate.evalCp`)는 둔 쪽 관점이라, 한 열에 나란히 세우는
 * 쪽이 뒤집어서 쓴다.
 *
 * **`mateIn` 이 같이 온다.** cp만 받으면 詰み인 자리가 `+29990` 으로 그려지고, 그건
 * 평가치가 아니라 환산값이다(`scoreJa`).
 */
export interface MoveEval {
  cp: number;
  mateIn: number | undefined;
}

/**
 * 「그 국면에서 이 수를 두면 얼마가 되나」를 **수 여러 개에 대해** 받아 온다.
 *
 * 물러진 수는 확정된 수가 아니라 `game_moves` 에 행이 없고, 개입 기록에는 낙폭만 남아 있던
 * 시절의 판이 있다(migrations/005 이전 — 그 값은 되돌릴 수 없다, journal §39 ⑥).
 * 그래서 **그 자리를 다시 재서** 채운다.
 *
 * **`useWhatIf` 를 쓰지 않는다.** 그쪽은 「지금 서 있는 분기」를 들고 있는 장치라, 값을
 * 얻으려고 그것을 건드리면 판이 딴 데로 간다. 여기는 **읽기만** 한다.
 *
 * **한 번에 하나씩 묻는다.** 한 국면에서 다섯 수를 물린 판이 있고(622의 159手), 그걸 동시에
 * 던지면 엔진 풀을 그만큼 잡는다 — 그 풀은 대국과 공유다(docs/01-core.md §4).
 */
export function useMoveEvals(gameId: number, basePly: number, usis: readonly string[]): Map<string, MoveEval> {
  const [evals, setEvals] = useState<Map<string, MoveEval>>(new Map());
  /** 이미 받은 것. 되돌아오면 다시 안 묻는다 — 서버 캐시가 있어도 왕복은 남는다. */
  const seen = useRef(new Map<string, MoveEval>());

  // 판이 바뀌면 들고 있던 것을 버린다. 같은 USI가 다른 판에서 다른 값이다.
  useEffect(() => {
    seen.current = new Map();
    setEvals(new Map());
  }, [gameId]);

  const key = usis.join(' ');
  useEffect(() => {
    if (!usis.length) {
      setEvals(new Map());
      return;
    }

    const controller = new AbortController();
    const send = httpSend(gameId);
    let alive = true;

    void (async () => {
      // 이미 아는 것으로 먼저 그린다 — 되돌아왔을 때 빈칸이 다시 보이지 않는다.
      const known = new Map<string, MoveEval>();
      for (const usi of usis) {
        const hit = seen.current.get(`${basePly}:${usi}`);
        if (hit !== undefined) known.set(usi, hit);
      }
      if (known.size) setEvals(new Map(known));

      for (const usi of usis) {
        if (!alive) return;
        if (known.has(usi)) continue;
        try {
          const node = await send({ ply: basePly, moves: [usi] }, controller.signal);
          if (!alive) return;
          // 끝난 국면(詰み·手詰まり)에는 값이 없다 — 서버가 탐색 없이 돌아온다. 그 줄은
          // 값 없이 남는다: 0으로 채우면 호각으로 읽힌다.
          if (node.evalCp === undefined) continue;
          const at: MoveEval = { cp: node.evalCp, mateIn: node.mateIn };
          seen.current.set(`${basePly}:${usi}`, at);
          known.set(usi, at);
          // **오는 대로 그린다.** 다 모아서 한 번에 띄우면 다섯 수짜리 국면에서 4초를 기다린다.
          setEvals(new Map(known));
        } catch {
          // 한 줄을 못 잰 것으로 목록을 세우지 않는다. 그 줄만 값 없이 남는다.
        }
      }
    })();

    return () => {
      alive = false;
      controller.abort();
    };
    // `usis` 는 매 렌더 새 배열이라 문자열로 비교한다.
  }, [gameId, basePly, key, usis]);

  return evals;
}
