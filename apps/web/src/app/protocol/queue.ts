// 상대의 Glicko 를 싣지 않는다. 매칭이 쓰는 내부 값이라 어느 API 도 돌려주지 않는다(01-core.md §5).

import type { Color } from '@/protocol/game';

export type QueueStatus =
  | {
      status: 'waiting';
      waitedMs: number;
      /** 지금 대기열에 서 있는 사람 수. 자기 포함. */
      waiting: number;
    }
  | {
      status: 'matched';
      roomId: string;
      yourColor: Color;
    };

/**
 * 대기열에 서기 · 살아 있다고 알리기 · 짝짓기를 한 호출이 한다. 화면이 주기적으로 부르기를
 * 멈추면 서버가 대기열에서 걷어낸다(서버의 `queue.StaleAfter`).
 */
export async function pollQueue(signal: AbortSignal): Promise<QueueStatus> {
  const res = await fetch('/api/queue', { method: 'POST', signal });
  if (res.status === 401) throw new Error('ログインが必要です。');
  if (!res.ok) throw new Error('対局相手を探せませんでした。');
  return (await res.json()) as QueueStatus;
}

/**
 * 대기열에서 빠진다. 부르지 않고 화면을 떠나면 서버가 걷어갈 때까지 대기열에 남고, 그 사이에
 * 잡힌 짝은 누구도 오지 않는 방이 된다.
 */
export async function leaveQueue(): Promise<void> {
  // `keepalive` 다. 탭을 닫는 자리에서도 부르므로(`useQueue`) 언마운트와 함께 취소되면 이
  // 요청이 아예 나가지 않는다.
  await fetch('/api/queue', { method: 'DELETE', keepalive: true }).catch(() => {
    // 실패해도 할 일이 없다. 서버가 만료로 걷어간다
  });
}
