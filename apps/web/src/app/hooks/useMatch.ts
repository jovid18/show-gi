import { useCallback, useEffect, useRef, useState } from 'react';

import type { MatchClientMessage, MatchServerMessage, MatchSnapshot, Room } from '@/protocol/match';

export type MatchConnection = 'connecting' | 'open' | 'closed';

export interface MatchState {
  connection: MatchConnection;
  /**
   * 방. **스냅샷보다 먼저 온다** — 상대를 기다리는 동안 화면이 그릴 것이 이것뿐이다
   * (초대 링크와 「◯◯さんを待っています」).
   */
  room: Room | null;
  /** 판. 상대가 들어오기 전에는 null이다. */
  snapshot: MatchSnapshot | null;
  /** 서버가 착수를 거절한 이유. 일본어 문구가 그대로 온다. */
  rejection: string | null;
  /** 끝난 이 판이 기록에 남은 번호. 「振り返り」 링크가 이 값으로 선다. */
  gameId: number | null;
  play: (usi: string) => void;
  resign: () => void;
  dismissRejection: () => void;
}

/**
 * `/ws/match` 에 붙어 방 하나의 자리에 앉는다.
 *
 * **`useGame` 과 갈리는 것이 둘이다.**
 *
 *  1. **어느 쪽인지를 안 보낸다.** 자리에서 이미 정해졌고(서버의 `Hub.Enter`), 요청으로
 *     보내면 두 사람이 같은 쪽을 주장할 수 있다.
 *  2. **끊겨도 판이 안 끝난다.** 상대가 남아 있어서다 — 같은 주소로 다시 붙으면 그 자리로
 *     돌아간다. 그동안 시계는 흐른다.
 *
 * 스냅샷은 언제나 전체 상태라 이전 것과 합치지 않는다(`useGame` 과 같은 규약).
 */
export function useMatch(roomId: string): MatchState {
  const [connection, setConnection] = useState<MatchConnection>('connecting');
  const [room, setRoom] = useState<Room | null>(null);
  const [snapshot, setSnapshot] = useState<MatchSnapshot | null>(null);
  const [rejection, setRejection] = useState<string | null>(null);
  const [gameId, setGameId] = useState<number | null>(null);
  const socketRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    const scheme = window.location.protocol === 'https:' ? 'wss' : 'ws';
    const socket = new WebSocket(`${scheme}://${window.location.host}/ws/match?room=${encodeURIComponent(roomId)}`);
    socketRef.current = socket;
    setConnection('connecting');

    /**
     * 이 소켓이 아직 **지금 방의 것**인가. 정리에서 `close()` 를 부르면 그 이벤트가
     * 뒤늦게 도착해 방금 정한 상태를 덮는다(`useGame` 이 같은 함정을 이미 적어 뒀다).
     */
    let current = true;
    const dropped = (): void => {
      if (current) setConnection('closed');
    };

    socket.addEventListener('open', () => {
      if (current) setConnection('open');
    });
    socket.addEventListener('close', dropped);
    socket.addEventListener('error', dropped);

    socket.addEventListener('message', (event) => {
      let msg: MatchServerMessage;
      try {
        msg = JSON.parse(String(event.data)) as MatchServerMessage;
      } catch {
        return; // 우리가 못 읽는 것은 무시한다. 판을 지우는 것보다 낫다
      }
      if (msg.type === 'snapshot') {
        setSnapshot(msg.snapshot);
        setRejection(null);
      } else if (msg.type === 'waiting') {
        setRoom(msg.room);
      } else if (msg.type === 'record') {
        setGameId(msg.gameId);
      } else if (msg.type === 'error') {
        setRejection(msg.message);
      }
    });

    return () => {
      current = false;
      socketRef.current = null;
      socket.close();
    };
  }, [roomId]);

  const send = useCallback((msg: MatchClientMessage) => {
    const socket = socketRef.current;
    if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify(msg));
  }, []);

  const play = useCallback((usi: string) => send({ type: 'move', usi }), [send]);
  const resign = useCallback(() => send({ type: 'resign' }), [send]);
  const dismissRejection = useCallback(() => setRejection(null), []);

  return { connection, room, snapshot, rejection, gameId, play, resign, dismissRejection };
}

/**
 * 지금 수번에 남은 밀리초. **서버가 준 값에서 화면이 세어 내려간다.**
 *
 * 서버가 매 초 보내지 않는 이유는 그것이 두 사람 몫의 프레임을 초당 두 개씩 만들기
 * 때문이고, 화면이 혼자 세지 않는 이유는 **탭을 멈춰 둔 브라우저에서 시간이 안 가기**
 * 때문이다. 그래서 정본은 서버이고 화면은 마지막으로 받은 값에서 이어 센다 —
 * 스냅샷이 올 때마다 다시 맞춰진다.
 */
export function useTurnClock(snapshot: MatchSnapshot | null): number {
  const [left, setLeft] = useState(0);
  // 마지막 스냅샷을 받은 시각. **`performance.now`** 다 — 시스템 시계가 바뀌어도 안 튄다.
  const at = useRef(0);
  const from = useRef(0);

  useEffect(() => {
    if (!snapshot || snapshot.status !== 'playing') {
      setLeft(0);
      return;
    }
    at.current = performance.now();
    from.current = snapshot.turnLeftMs;
    setLeft(snapshot.turnLeftMs);

    const id = window.setInterval(() => {
      const next = from.current - (performance.now() - at.current);
      setLeft(next > 0 ? next : 0);
    }, 200);
    return () => window.clearInterval(id);
  }, [snapshot]);

  return left;
}
