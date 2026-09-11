/**
 * 지금 두는 중인가. 대국 화면이 쓰고 앱 셸이 읽는다(App.tsx, journal §86).
 *
 * 값 하나에 가게를 따로 둔 것은 소유권 때문이다. 판의 상태는 `useGame` 이 갖고 있고 그건
 * WebSocket 하나에 매여 있어서, 셸이 그 훅을 한 번 더 부르면 연결도 판도 둘이 된다.
 *
 * 흐름은 한 방향이다. 대국 화면이 알리고 셸이 듣는다. 반대로 쓰는 쪽이 생기면 소유권을
 * 다시 본다.
 *
 * `useSyncExternalStore` 가 읽으므로 `getPlaying` 은 같은 값에 같은 것을 돌려줘야 한다.
 * `setPlaying` 이 값이 그대로면 알리지 않는 것도 같은 규약이다.
 */
let playing = false;

const listeners = new Set<() => void>();

/** 대국 화면만 부른다. 값이 그대로면 누구도 깨우지 않는다. */
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
