// 회차의 입구. 시나리오를 MODE 로 고른다.
//
// 셋을 한 파일로 두는 이유는 「엔진 + 대인전」이 별도 시나리오가 아니라 조합이기
// 때문이다 — 둘을 같이 걸어야 engine_pool_wait_seconds 의 borrower 라벨이 갈리는
// 것을 볼 수 있고, 그것이 이 회차의 주된 물음이다(journal §101).
//
//   MODE=engine   엔진 대국만. 쿠키는 있으면 쓰고 없으면 익명이다
//   MODE=match    대인전만. 로그인·큐가 필요하다
//   MODE=both     둘을 동시에. 서로 굶히는지를 재는 회차다
import engineGame from './engine.js';
import humanMatch from './match.js';
import { LT_UIDS, SESSION_SECRET } from './lib/config.js';

const MODE = __ENV.MODE || 'engine';
const DURATION = __ENV.DURATION || '3m';
const VUS_ENGINE = Number(__ENV.VUS_ENGINE || 3);
const VUS_MATCH = Number(__ENV.VUS_MATCH || 2);

// 시나리오는 상수 VU 다. ramping 을 안 쓰는 이유는 이 앱의 부하 단위가 「동시 판수」라서다 —
// 판 하나가 연결 하나를 몇 분 잡으므로, 초당 도착률로 말하면 동시 판수가 안 보인다.
function scenarios() {
  const engineScenario = {
    executor: 'constant-vus',
    exec: 'engine',
    vus: VUS_ENGINE,
    duration: DURATION,
    tags: { scenario: 'engine' },
  };
  const matchScenario = {
    executor: 'constant-vus',
    exec: 'match',
    vus: VUS_MATCH,
    duration: DURATION,
    tags: { scenario: 'match' },
  };
  if (MODE === 'engine') {
    return { engine: engineScenario };
  }
  if (MODE === 'match') {
    return { match: matchScenario };
  }
  return { engine: engineScenario, match: matchScenario };
}

// scenarioSplit 은 시나리오별 하위 지표를 요약에 찍히게 한다.
//
// k6 는 태그가 붙은 하위 지표를 임계치로 선언해야 요약에 넣는다. 그래서 늘 통과하는
// 조건을 선언으로 쓴다 — 게이트가 아니고, 표본이 0이어도 0으로 찍히고 통과한다.
//
// MODE=both 에서만 붙인다. 그 회차의 물음이 「둘이 서로 굶히나」인데 합친 move_cycle
// 하나로는 답할 수 없어서다(journal §113). 한 갈래만 도는 회차는 전체가 곧 그 갈래다.
function scenarioSplit() {
  if (MODE !== 'both') {
    return {};
  }
  const split = {};
  for (const name of ['engine', 'match']) {
    // 사람이 기다린 시간. 이 회차에서 갈라 볼 값이 이것 하나다 — 나머지 셋은 그 값을
    // 몇 판·몇 手에서 잰 것인지 읽는 데 쓴다.
    split[`showgi_move_cycle{scenario:${name}}`] = ['p(95)>=0'];
    split[`showgi_games{scenario:${name}}`] = ['count>=0'];
    split[`showgi_plies{scenario:${name}}`] = ['count>=0'];
    split[`showgi_stalls{scenario:${name}}`] = ['count>=0'];
  }
  return split;
}

export const options = {
  scenarios: scenarios(),
  thresholds: {
    // 거절은 0이어야 한다. 서버가 준 legalMoves 만 보내므로 하나라도 있으면 도구 버그다.
    showgi_rejects: ['count==0'],
    // 스스로 끊는 장치. 프로덕션에는 레이트 리밋이 아무 데도 없어서(Caddy·ALB·앱)
    // 막아 줄 것이 없다 — 5xx 가 늘면 회차가 스스로 멈춘다.
    http_req_failed: [{ threshold: 'rate<0.05', abortOnFail: true, delayAbortEval: '30s' }],
    ...scenarioSplit(),
  },
};

// setup 은 사람 목록이 회차를 끌고 갈 만한지 먼저 본다. VU 안에서 터지면 판이 몇 개
// 열린 뒤에 알게 되고, 그때는 이미 지워야 할 익명 판이 남는다.
export function setup() {
  if (MODE !== 'engine' && (SESSION_SECRET === '' || LT_UIDS.length === 0)) {
    throw new Error('대인전에는 SESSION_SECRET 과 LT_UIDS 가 둘 다 필요하다 — 쿠키가 없으면 큐에 설 수 없다');
  }

  if (SESSION_SECRET === '' || LT_UIDS.length === 0) {
    // 위에서 걸렀으므로 여기는 MODE=engine 뿐이다. 막지는 않는다 — 시딩 없이 로컬에서
    // 걸어 보는 것이 이 도구의 첫 회차였다.
    console.warn('익명으로 돈다 — games.user_id 가 NULL 이라 cleanup.sql 이 이 판들에 닿지 않는다');
    return {};
  }

  // __VU 는 회차 전체에서 유일하다. 그래서 목록이 두 시나리오의 VU 합보다 짧으면 두
  // VU 가 같은 사람을 잡고, 큐가 사람마다 한 행이라(match_queue 의 PK) 서로를 못 만난다.
  const need = (MODE === 'match' ? 0 : VUS_ENGINE) + (MODE === 'engine' ? 0 : VUS_MATCH);
  if (LT_UIDS.length < need) {
    throw new Error(`LT_UIDS 가 ${LT_UIDS.length}개인데 VU 가 ${need}개다 — 사람이 겹치면 짝이 안 잡힌다`);
  }
  return {};
}

export function engine() {
  engineGame();
}

export function match() {
  humanMatch();
}
