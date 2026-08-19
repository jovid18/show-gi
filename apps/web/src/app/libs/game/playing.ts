/**
 * 지금 두는 중인가. 대국 화면이 쓰고 앱 셸이 읽는다(App.tsx) — 그 값 하나가 다른 주소를
 * 판으로 되돌리고, 헤더에서 누를 것을 없애고, 홈 메뉴의 검토 줄을 감춘다(journal §86).
 *
 * 값 하나에 가게를 따로 둔 이유는 소유권이다. 판의 상태는 `useGame` 이 들고 있고 그건
 * WebSocket 하나에 매여 있어서, 셸이 그 훅을 한 번 더 부르면 연결도 판도 둘이 된다.
 * 위로 끌어올릴 수도 없다 — 대국 화면이 라우트 밖에 상시 마운트인 이유가 그 연결이다.
 *
 * 그래서 한 방향으로만 흐른다. 대국 화면이 알리고 셸이 듣는다 — 반대로 쓰는 쪽이 생기면
 * 이 파일이 아니라 소유권을 다시 본다.
 *
 * `useSyncExternalStore` 가 읽으므로 `getPlaying` 은 같은 값에 같은 것을 돌려줘야 한다.
 * `setPlaying` 이 값이 그대로면 안 알리는 것도 같은 규약이다 — 매 手마다 스냅샷이 오는데
 * 그때마다 셸을 다시 그릴 이유가 없다.
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
