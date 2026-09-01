// 대기열의 계약. 서버의 `internal/server/queue.go` 와 짝이다.
//
// `protocol/match.ts` 와 갈라 둔 이유는 여기 판이 하나도 없기 때문이다. 대기열에 서는 것은
// 방을 만들기 전의 일이고, 짝이 잡히면 답은 방 id 하나다 — 그 뒤로는 저쪽 계약이 맡는다.
//
// 상대에 대해 아무것도 안 온다. 이름도 레이팅도 없다 — 레이팅은 어느 API 도 안 돌려주고
// (docs/01-core.md §5), 이름은 방에 붙으면 스냅샷이 준다.

import type { Color } from '@/protocol/game';

/** 대기열에 선 사람이 받는 답. */
export type QueueStatus =
  | {
      status: 'waiting';
      /** 대기열에 선 뒤로 흐른 시간(ms). 정본이 서버라 새로고침해도 이어 센다. */
      waitedMs: number;
      /** 지금 대기열에 서 있는 사람 수(자기 포함). */
      waiting: number;
    }
  | {
      status: 'matched';
      /** 갈 방. 화면이 `/rooms/:id` 로 옮겨 간다. */
      roomId: string;
      /** 그 방에서 잡을 쪽. 대기열은 平手 확정 · 先手 랜덤이다. */
      yourColor: Color;
    };

/**
 * 대기열에 서거나, 이미 서 있으면 다시 물어본다.
 *
 * 같은 호출 하나가 셋을 한다 — 대기열에 서기 · 살아 있다고 알리기 · 짝짓기. 그래서 화면은
 * 이것을 주기적으로 부르기만 하면 되고, 멈추면 서버가 알아서 대기열에서 걷어낸다
 * (서버의 `queue.StaleAfter`).
 */
export async function pollQueue(signal: AbortSignal): Promise<QueueStatus> {
  const res = await fetch('/api/queue', { method: 'POST', signal });
  if (res.status === 401) throw new Error('ログインが必要です。');
  if (!res.ok) throw new Error('対局相手を探せませんでした。');
  return (await res.json()) as QueueStatus;
}

/**
 * 대기열에서 빠진다. 안 부르고 화면을 떠나도 서버가 걷어가지만, 그때까지 상대에게는
 * 내가 대기열에 있는 것으로 보인다 — 그 사이에 잡힌 짝은 아무도 안 오는 방이 된다.
 */
export async function leaveQueue(): Promise<void> {
  // `keepalive` 다. 탭을 닫는 자리에서도 부르므로(`useQueue`) 언마운트와 함께 취소되면
  // 이 요청이 아예 안 나간다.
  await fetch('/api/queue', { method: 'DELETE', keepalive: true }).catch(() => {
    // 실패해도 할 일이 없다. 서버가 만료로 걷어간다
  });
}
