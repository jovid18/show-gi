import { useCallback, useEffect, useRef, useState } from 'react';

import type { ClientMessage, ServerMessage, Snapshot } from '@/protocol/game';
import type { WhatIfNode, WhatIfRequest } from '@/protocol/whatif';
import type { Send } from '@/hooks/useWhatIf';

export type Connection = 'connecting' | 'open' | 'closed';

export interface GameState {
  connection: Connection;
  snapshot: Snapshot | null;
  /** 서버가 착수를 거절한 이유. 일본어 문구가 그대로 온다. */
  rejection: string | null;
  /**
   * 개입 **회차**. 개입이 실려 온 스냅샷마다 하나씩 오른다.
   *
   * `snapshot.intervention` 이 있는지만 보면 안 된다 — 서버는 다음 착수까지 그걸 들고
   * 있으므로, 같은 자리에서 같은 수로 또 걸렸을 때 화면이 "아까 그거"로 착각한다.
   * 회차로 세면 연출을 다시 돌릴지가 명확해진다.
   */
  interventionEpisode: number;
  play: (usi: string) => void;
  resign: () => void;
  dismissRejection: () => void;
  /** 새 대국을 시작한다. 끊긴 연결을 다시 붙일 때도 같은 것을 쓴다. */
  restart: () => void;
  /**
   * 가정 수순 한 자리를 **이 대국의 연결로** 묻는다(`useWhatIf` 의 `Send`).
   *
   * **되짚기와 길이 갈리는 이유는 뿌리다.** 저쪽은 DB 기록에서 만들지만, 두는 중인 판은
   * 기록이 비동기로 쌓여서 개입 직후에는 마지막 수가 아직 없을 수 있다 — 하필 제일
   * 누르고 싶은 순간에 흔들린다. 세션이 방금 보낸 스냅샷이 그 자리의 정본이다.
   */
  whatif: Send;
}

function socketUrl(): string {
  const scheme = window.location.protocol === 'https:' ? 'wss' : 'ws';
  return `${scheme}://${window.location.host}/ws/game`;
}

/**
 * `/ws/game`에 붙어 대국 하나를 연다.
 *
 * 스냅샷은 **항상 전체 상태**라 여기서 이전 것과 합치지 않는다. 받은 것으로 통째로 바꾼다 —
 * 부분 갱신을 재구성하기 시작하면 D3의 롤백 뒤에 화면과 서버가 어긋나도 알 방법이 없다.
 */
export function useGame(): GameState {
  const [connection, setConnection] = useState<Connection>('connecting');
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [rejection, setRejection] = useState<string | null>(null);
  const [interventionEpisode, setInterventionEpisode] = useState(0);
  // 새 대국은 새 연결이다. 서버가 연결 하나에 대국 하나를 여니, 다시 붙는 것이 곧 새 판이다.
  const [generation, setGeneration] = useState(0);
  const socketRef = useRef<WebSocket | null>(null);
  // 직전 스냅샷에 개입이 실려 있었는가. 회차를 세는 데만 쓴다.
  const hadIntervention = useRef(false);

  /**
   * 답을 기다리는 가정 수순 요청.
   *
   * **하나뿐이다.** 서버가 한 연결에 한 번만 돌리므로(ws.go 의 슬롯) 여기도 하나면 되고,
   * 새 요청이 오면 앞의 것은 버려진다 — 그쪽은 이미 `useWhatIf` 가 abort로 접은 것이다.
   */
  const asking = useRef<{ resolve: (n: WhatIfNode) => void; reject: (e: Error) => void } | null>(null);
  const settle = useCallback((fn: (p: NonNullable<typeof asking.current>) => void) => {
    const pending = asking.current;
    if (!pending) return;
    asking.current = null;
    fn(pending);
  }, []);

  useEffect(() => {
    const socket = new WebSocket(socketUrl());
    socketRef.current = socket;
    hadIntervention.current = false;

    socket.addEventListener('open', () => setConnection('open'));
    socket.addEventListener('close', () => setConnection('closed'));
    socket.addEventListener('error', () => setConnection('closed'));

    socket.addEventListener('message', (event) => {
      let msg: ServerMessage;
      try {
        msg = JSON.parse(String(event.data)) as ServerMessage;
      } catch {
        return; // 우리가 못 읽는 것은 무시한다. 판을 지우는 것보다 낫다
      }
      if (msg.type === 'snapshot') {
        // 서버는 착수 하나에 개입 하나를 싣고 다음 착수까지 들고 있는다. 그래서 "있다"가
        // 아니라 **없다가 생긴 순간**이 새 개입이다.
        const has = Boolean(msg.snapshot.intervention);
        if (has && !hadIntervention.current) setInterventionEpisode((n) => n + 1);
        hadIntervention.current = has;

        setSnapshot(msg.snapshot);
        setRejection(null);
      } else if (msg.type === 'error') {
        setRejection(msg.message);
      } else if (msg.type === 'whatif') {
        settle((p) => p.resolve(msg.whatif));
      } else if (msg.type === 'whatif_error') {
        // **착수 거절과 갈라 둔다.** 저쪽은 판 위의 실패라 판 옆에 뜨고, 이쪽은 가정 수순
        // 패널 안의 실패다 — 한 자리에 뭉치면 「두다가 뭘 잘못했나」로 읽힌다.
        settle((p) => p.reject(new Error(msg.message)));
      }
    });

    return () => {
      socketRef.current = null;
      // 끊긴 연결에는 답이 안 온다. 기다리는 쪽을 영원히 매달아 두지 않는다.
      settle((p) => p.reject(new Error('接続が切れました。')));
      socket.close();
    };
  }, [generation, settle]);

  const send = useCallback((msg: ClientMessage) => {
    const socket = socketRef.current;
    if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify(msg));
  }, []);

  const play = useCallback((usi: string) => send({ type: 'move', usi }), [send]);
  const resign = useCallback(() => send({ type: 'resign' }), [send]);
  const dismissRejection = useCallback(() => setRejection(null), []);

  const whatif = useCallback<Send>(
    (req: WhatIfRequest, signal: AbortSignal) =>
      new Promise<WhatIfNode>((resolve, reject) => {
        const socket = socketRef.current;
        if (socket?.readyState !== WebSocket.OPEN) {
          reject(new Error('接続していません。'));
          return;
        }
        // 앞의 것이 아직 매달려 있으면 접는다. 답은 하나만 온다.
        settle((p) => p.reject(new Error('やり直しました。')));
        asking.current = { resolve, reject };
        signal.addEventListener('abort', () => settle((p) => p.reject(new Error('やめました。'))), { once: true });
        socket.send(JSON.stringify({ type: 'whatif', ply: req.ply, moves: req.moves } satisfies ClientMessage));
      }),
    [settle],
  );

  const restart = useCallback(() => {
    // 판을 먼저 비운다. 끝난 대국이 남아 있으면 새 판이 오기 전 한 순간 그게 보인다.
    setSnapshot(null);
    setRejection(null);
    setConnection('connecting');
    setGeneration((n) => n + 1);
  }, []);

  return {
    connection,
    snapshot,
    rejection,
    interventionEpisode,
    play,
    resign,
    dismissRejection,
    restart,
    whatif,
  };
}
