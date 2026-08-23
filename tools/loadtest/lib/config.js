// 회차 손잡이 전부. k6 는 환경변수로만 값을 받는다(__ENV).
//
// 기본값은 로컬 api 컨테이너를 가리킨다 — 첫 판은 프로덕션이 아니라 여기서 돌린다.
export const BASE = __ENV.BASE || 'http://localhost:8080';

// WS 주소는 BASE 에서 만든다. 둘을 따로 받으면 한쪽만 바꿔 놓고 로컬을 재는 사고가 난다.
export const WS_BASE = BASE.replace(/^http/, 'ws');

export const SESSION_SECRET = __ENV.SESSION_SECRET || '';

// 手数 상한. 이걸로 판 길이를 우리가 정한다 — 무작위로 두면 판이 잘 안 끝나서
// 분석에 들어가는 手数 분포가 실제 대국과 달라진다(journal §101 의 27·34·123手).
export const MAX_PLIES = Number(__ENV.MAX_PLIES || 60);

// 씨앗. 회차를 다시 돌릴 수 있어야 「느렸다」를 같은 수순으로 재현할 수 있다.
export const SEED = Number(__ENV.SEED || 1);

// 대인전에 쓸 사용자 번호. seed.sql 이 마지막에 찍어 주는 목록을 그대로 넘긴다.
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
// 스냅샷 시한이 서버의 착수 시한(60초)보다 커야 한다. 작으면 정상적으로 오래 생각하는
// 상대를 우리가 끊는다.
export const GAME_TIMEOUT_MS = Number(__ENV.GAME_TIMEOUT_MS || 300000);
export const STALL_TIMEOUT_MS = Number(__ENV.STALL_TIMEOUT_MS || 90000);

// 대기열을 몇 번까지 물어볼 것인가. 화면은 2초마다 부른다(api.md §2).
export const QUEUE_TRIES = Number(__ENV.QUEUE_TRIES || 30);
export const QUEUE_INTERVAL = Number(__ENV.QUEUE_INTERVAL || 2);
