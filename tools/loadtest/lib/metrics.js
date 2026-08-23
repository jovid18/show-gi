// 도구가 직접 재는 것. 서버 지표와 겹치지 않는 것만 둔다 —
// 겹치면 두 숫자가 갈릴 때 어느 쪽이 맞는지 정할 수가 없다.
import { Counter, Trend } from 'k6/metrics';

// 착수 왕복. 내 수를 보낸 순간부터 다시 내 차례가 되기까지다.
//
// 이 안에 판정과 상대의 탐색이 다 들어 있다 — 사람이 실제로 기다리는 시간이고,
// 서버의 engine_search_duration_seconds 와 갈리는 자리다(저쪽은 탐색 하나다).
export const moveCycle = new Trend('showgi_move_cycle', true);

// 짝이 잡히기까지 줄에서 기다린 시간. 서버도 재지만(match_pairing_wait_seconds)
// 이쪽은 못 잡힌 회차까지 안다.
export const queueWait = new Trend('showgi_queue_wait', true);

export const games = new Counter('showgi_games');
export const plies = new Counter('showgi_plies');
export const interventions = new Counter('showgi_interventions');
export const rejects = new Counter('showgi_rejects');
export const queueTimeouts = new Counter('showgi_queue_timeouts');

// 시한이 끊은 판. `cut` 라벨로 갈린다 — 둘을 합치면 깨짐 신호로 쓸 수 없다.
//
//   cut=stall   스냅샷이 STALL_TIMEOUT_MS 동안 안 왔다. 서버가 멈춘 것이다
//   cut=capped  판이 GAME_TIMEOUT_MS 를 넘겼다. 우리 시한이 자른 것이다
//
// capped 는 회차 길이가 그 시한에 가까우면 저절로 생긴다. 「깨질 때까지」 올리는 회차에서
// 보는 것은 stall 쪽이고, capped 를 섞으면 서버가 무너진 것처럼 보인다.
export const stalls = new Counter('showgi_stalls');
