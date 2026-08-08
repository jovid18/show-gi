import { useCallback, useEffect, useRef, useState } from 'react';

import type { ClientMessage, ServerMessage, Snapshot } from './protocol';

export type Connection = 'connecting' | 'open' | 'closed';

export interface GameState {
  connection: Connection;
  snapshot: Snapshot | null;
  /** 서버가 착수를 거절한 이유. 일본어 문구가 그대로 온다. */
  rejection: string | null;
  play: (usi: string) => void;
  resign: () => void;
  dismissRejection: () => void;
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
  const socketRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    const socket = new WebSocket(socketUrl());
    socketRef.current = socket;

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
        setSnapshot(msg.snapshot);
        setRejection(null);
      } else if (msg.type === 'error') {
        setRejection(msg.message);
      }
    });

    return () => {
      socketRef.current = null;
      socket.close();
    };
  }, []);

  const send = useCallback((msg: ClientMessage) => {
    const socket = socketRef.current;
    if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify(msg));
  }, []);

  const play = useCallback((usi: string) => send({ type: 'move', usi }), [send]);
  const resign = useCallback(() => send({ type: 'resign' }), [send]);
  const dismissRejection = useCallback(() => setRejection(null), []);

  return { connection, snapshot, rejection, play, resign, dismissRejection };
}
