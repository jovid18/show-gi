// 회차 손잡이 전부. k6 는 환경변수로만 값을 받는다(__ENV).
//
// 기본값은 로컬 api 컨테이너를 가리킨다 — 첫 판은 프로덕션이 아니라 여기서 돌린다.
export const BASE = __ENV.BASE || 'http://localhost:8080';

// WS 주소는 BASE 에서 만든다. 둘을 따로 받으면 한쪽만 바꿔 놓고 로컬을 재는 사고가 난다.
export const WS_BASE = BASE.replace(/^http/, 'ws');

export const SESSION_SECRET = __ENV.SESSION_SECRET || '';

// 手数 상한. 이걸로 판 길이를 우리가 정한다 — 무작위로 두면 판이 잘 안 끝나서
// 분석에 들어가는 手数 분포가 실제 대국과 달라진다(journal §101 의 27·34·123手).
//
// 60이었을 때 완주한 24판이 전부 정확히 60手였다(journal §104) — 규칙으로 끝난 판이
// 하나도 없어서 詰み도 終盤도 안 돌았다. 100은 終盤에 닿게 하려는 값이다.
export const MAX_PLIES = Number(__ENV.MAX_PLIES || 100);

// 한 手를 두기 전에 기다릴 시간. 사람의 생각 시간을 도구에 넣는 자리다.
//
// 0이 기본이라 지금까지의 회차와 그대로 견줄 수 있다(journal §104).
//
// 대인전에는 넣어야 한다. 양쪽이 다 도구라서 기다릴 상대가 없고, 그래서 100手 판이
// RTT 속도로 2초에 끝난다 — 사람 넷이 93초에 54판을 두는 도착률이 되고, 그것이 사후
// 분석의 배수구(0.64판/분)를 70배 넘긴다(journal §105).
//
// 엔진 대국에는 없어도 된다. 엔진의 탐색 시간이 저절로 페이서가 되어 도구가 상대를
// 기다린다 — 대인전에만 그 자리가 비어 있다.
//
// 넣으면 VU 수를 사람 수로 그대로 읽을 수 있다. 안 넣었을 때의 「VU 하나는 사람 하나보다
// 크다」 환산이 그때 없어진다.
//
// move_cycle 을 이 값만큼 빼서 읽는다. 그 지표는 우리가 보낸 뒤 다시 우리 차례가 될
// 때까지이고, 그 사이에 상대의 생각 시간이 들어 있다 — 대인전에서는 상대가 우리 자신이라
// THINK_MS 가 그대로 얹힌다. 3초로 잰 회차의 중앙값 3.03초에서 서버 몫은 30ms 다.
export const THINK_MS = Number(__ENV.THINK_MS || 0);

// 씨앗. 회차를 다시 돌릴 수 있어야 「느렸다」를 같은 수순으로 재현할 수 있다.
export const SEED = Number(__ENV.SEED || 1);

// 회차가 쓸 사용자 번호. seed.sql 이 마지막에 찍어 주는 목록을 그대로 넘긴다.
//
// 대인전에는 없으면 안 되고, 엔진 대국에는 있으면 그 사람의 판으로 남는다 — 없으면
// 익명이라 cleanup.sql 이 닿지 않는다.
export const LT_UIDS = (__ENV.LT_UIDS || '')
  .split(',')
  .map((s) => Number(s.trim()))
  .filter((n) => n > 0);

// 접쇼기. 六枚落ち 같은 것을 걸면 사람이 이기기 쉬워 판이 짧게 끝난다.
export const HANDICAP = __ENV.HANDICAP || '';

// 판 하나에 줄 최대 시간과, 스냅샷이 끊긴 뒤 기다릴 시간.
//
// 둘이 필요하다. 판이 길어지는 것과 판이 멈추는 것은 다른 일이고, 멈춘 판을 그냥 두면
// 회차의 동시 판수가 조용히 줄어든다 — VU 하나가 아무것도 안 하면서 자리를 잡고 있다.
//
// 멈춤을 잡는 것은 STALL 쪽이다. GAME 쪽은 마지막 보루라서 건강한 회차에서는 아예 안
// 터져야 한다 — 5분이었을 때 8판 동시의 판이 그것에 닿아 cut=capped 가 멈춘 판 집계를
// 오염시켰다(journal §104). 15분은 100手 판 위로 넉넉히 둔 값이다.
//
// 스냅샷 시한이 서버의 착수 시한(60초)보다 커야 한다. 작으면 정상적으로 오래 생각하는
// 상대를 우리가 끊는다.
export const GAME_TIMEOUT_MS = Number(__ENV.GAME_TIMEOUT_MS || 900000);
export const STALL_TIMEOUT_MS = Number(__ENV.STALL_TIMEOUT_MS || 90000);

// 대기열을 몇 번까지 물어볼 것인가. 화면은 2초마다 부른다(api.md §2).
export const QUEUE_TRIES = Number(__ENV.QUEUE_TRIES || 30);
export const QUEUE_INTERVAL = Number(__ENV.QUEUE_INTERVAL || 2);
