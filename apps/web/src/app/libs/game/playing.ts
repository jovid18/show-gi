/**
 * 지금 두는 중인가. **대국 화면이 쓰고 헤더가 읽는다.**
 *
 * 이 한 값을 위해 가게를 따로 두는 이유가 소유권이다 — 판의 상태는 `useGame` 이 들고
 * 있고 그건 WebSocket 하나에 매여 있어서, 헤더가 알려고 그 훅을 한 번 더 부르면
 * **연결이 둘이 되고 판도 둘이 된다.** 위로 끌어올리는 것도 답이 아니다: 대국 화면이
 * 라우트 밖에 상시 마운트인 이유가 그 연결이라(App.tsx), 그 훅은 거기 있어야 한다.
 *
 * 그래서 **한 방향으로만 흐른다** — 대국 화면이 알리고, 헤더가 듣는다. 반대로 쓰는 쪽이
 * 생기면 판의 상태가 두 자리에 있게 되므로 그때는 이 파일이 아니라 소유권을 다시 본다.
 *
 * `useSyncExternalStore` 가 읽으므로 **`getPlaying` 은 같은 값에 같은 것을 돌려줘야 한다**
 * (원시값이라 저절로 맞는다). 아래 `setPlaying` 이 값이 그대로면 안 알리는 것도 같은 규약이다 —
 * 매 手마다 스냅샷이 오는데 그때마다 헤더를 다시 그릴 이유가 없다.
 */
let playing = false;

const listeners = new Set<() => void>();

/** 대국 화면만 부른다. 값이 그대로면 아무도 안 깨운다. */
export function setPlaying(next: boolean): void {
  if (playing === next) return;
  playing = next;
  for (const notify of listeners) notify();
}

export function subscribePlaying(notify: () => void): () => void {
  listeners.add(notify);
  return () => {
    listeners.delete(notify);
  };
}

export function getPlaying(): boolean {
  return playing;
}
