// 생각 시간. 착수를 THINK_MS 만큼 미룬다.
//
// setTimeout 이고 sleep 이 아니다. 착수가 WS 이벤트 핸들러 안에서 일어나는데
// (k6/experimental/websockets) sleep 은 그 이벤트 루프를 통째로 세운다 — 그러면 상대의
// 스냅샷도 같이 늦어져서, 미룬 것이 서버 지연으로 잡힌다.
import { setTimeout } from 'k6/timers';

import { THINK_MS } from './config.js';

// think 는 fn 을 THINK_MS 뒤에 부르고 그 타이머를 준다. 0이면 그 자리에서 부르고 0을
// 주는데, clearTimeout(0) 이 무해하므로 부르는 쪽이 두 경우를 안 가른다.
//
// 판이 그 사이에 끝날 수 있다. 부르는 쪽이 타이머를 붙잡고 있다가 끝낼 때 지운다 —
// 안 지우면 닫힌 소켓에 착수를 보낸다.
export function think(fn) {
  if (THINK_MS <= 0) {
    fn();
    return 0;
  }
  return setTimeout(fn, THINK_MS);
}
