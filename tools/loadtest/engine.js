// 엔진 대국 한 판. /ws/game 은 로그인 없이도 열린다(journal §18).
//
// 연결 하나가 대국 하나이고 서버가 먼저 말을 건다(api.md §5). 그래서 이 함수는
// 수를 두는 루프가 아니라 스냅샷에 반응하는 핸들러다.
//
// 그래도 쿠키를 굽는다. 익명으로 열면 games.user_id 가 NULL 로 남아 회차가 만든 판과
// 실제 익명 사용자의 판을 구별할 수 없고, cleanup.sql 이 그 판에 닿지 않는다.
import { WebSocket } from 'k6/experimental/websockets';
import { clearTimeout, setTimeout } from 'k6/timers';

import {
  GAME_TIMEOUT_MS,
  HANDICAP,
  LT_UIDS,
  MAX_PLIES,
  SEED,
  SESSION_SECRET,
  STALL_TIMEOUT_MS,
  WS_BASE,
} from './lib/config.js';
import { games, interventions, moveCycle, plies, rejects, stalls } from './lib/metrics.js';
import { makeRNG, pickMove } from './lib/moves.js';
import { cookieHeader, mintSession } from './lib/session.js';
import { think } from './lib/think.js';

export default function engineGame() {
  // 둘이 다 있어야 그 사람의 판으로 남긴다. 하나만 있으면 익명으로 돈다 — 시딩 없이
  // 로컬에서 걸어 보는 자리를 남긴다. 그 회차는 main.js 의 setup 이 경고한다.
  const authed = SESSION_SECRET !== '' && LT_UIDS.length > 0;
  const uid = authed ? LT_UIDS[(__VU - 1) % LT_UIDS.length] : 0;
  const headers = authed ? cookieHeader(mintSession(SESSION_SECRET, uid, `LT${uid}`, 3600)) : {};

  const rng = makeRNG(SEED * 7919 + __VU * 104729 + __ITER);
  const query = HANDICAP ? `?handicap=${HANDICAP}` : '?color=b';
  const ws = new WebSocket(`${WS_BASE}/ws/game${query}`, null, { headers });

  let sentAt = 0;
  let sentPly = -1;
  let sentRetracted = '';
  let counted = false;
  let stallTimer = 0;
  let gameTimer = 0;
  let thinkTimer = 0;

  // 판을 닫는 자리를 한 곳에 둔다. 두 군데서 닫으면 games 가 두 번 세어진다.
  //
  // cut 은 무엇이 끊었나다 — 빈 문자열이면 판이 스스로 끝난 것이고, 아니면 시한이다.
  const finish = (cut) => {
    clearTimeout(stallTimer);
    clearTimeout(gameTimer);
    clearTimeout(thinkTimer);
    if (!counted) {
      counted = true;
      games.add(1, { kind: 'engine' });
      if (cut !== '') {
        stalls.add(1, { kind: 'engine', cut });
      }
    }
    ws.close();
  };

  // 멈춘 판을 우리가 끊는다. 서버의 착수 시한보다 넉넉히 크게 둔다 — 작으면 오래
  // 생각하는 상대를 우리가 끊는다.
  const bump = () => {
    clearTimeout(stallTimer);
    stallTimer = setTimeout(() => finish('stall'), STALL_TIMEOUT_MS);
  };
  gameTimer = setTimeout(() => finish('capped'), GAME_TIMEOUT_MS);
  bump();

  ws.addEventListener('message', (event) => {
    bump();
    const msg = JSON.parse(event.data);

    if (msg.type === 'error') {
      // 정상 경로에서는 안 온다. 서버가 준 legalMoves 만 보내므로, 오면 우리 버그다.
      rejects.add(1, { reason: msg.reason });
      return;
    }
    if (msg.type === 'summary') {
      finish('');
      return;
    }
    if (msg.type !== 'snapshot') {
      return;
    }

    const s = msg.snapshot;
    if (s.intervention) {
      interventions.add(1, { category: s.intervention.category || 'unknown' });
    }
    if (s.status !== 'playing') {
      // 총평을 기다리지 않는다. 판이 끝난 뒤에 오는데(api.md §5) 회차 길이를
      // 그 대기로 늘리면 동시 판수가 실제보다 낮게 유지된다.
      finish('');
      return;
    }
    // 판정 중이거나 상대가 생각하는 중이면 둘 수 없다. 보내면 not_your_turn 이다.
    if (!s.yourTurn || s.judging || s.thinking) {
      return;
    }
    // 같은 手에 두 번 보내지 않는다.
    //
    // 내 차례인 스냅샷이 한 手에 여러 번 온다 — 힌트가 도착하거나 상대의 세기가 바뀌면
    // 그때마다 전체 상태가 다시 나간다(api.md §5 의 「부분 갱신이 없다」). 보낸 뒤로
    // 판이 안 움직였으면 그것은 그 스냅샷들이고, 두 번째 착수는 판정 중에 도착해
    // not_your_turn 으로 거절된다.
    //
    // 개입은 예외다. 물러진 手는 手数가 그대로 돌아오는데 그때는 다시 둬야 한다.
    // 다만 그 카드도 여러 스냅샷에 실려 오므로, 무른 수까지 같으면 같은 카드다.
    const retracted = s.intervention ? s.intervention.retractedUsi || '' : '';
    if (sentPly === s.ply && (retracted === '' || retracted === sentRetracted)) {
      return;
    }

    if (sentAt > 0) {
      moveCycle.add(Date.now() - sentAt);
      sentAt = 0;
    }
    if (s.ply >= MAX_PLIES) {
      ws.send(JSON.stringify({ type: 'resign' }));
      return;
    }

    // 물러진 수는 후보에서 뺀다. 같은 수를 다시 고르면 같은 카드가 다시 오고, 위
    // 가드가 그것을 「이미 답한 카드」로 보고 넘겨서 판이 그 자리에 선다 — 서버의
    // 60초 시한이 풀어 줄 때까지.
    const pool = retracted === '' ? s.legalMoves : s.legalMoves.filter((m) => m !== retracted);
    const usi = pickMove(pool, rng);
    if (!usi) {
      ws.send(JSON.stringify({ type: 'resign' }));
      return;
    }
    // 手数와 무른 수를 여기서 잠근다. 미루는 사이에도 스냅샷이 오므로(힌트·세기 변경)
    // 잠그지 않으면 같은 手에 타이머가 여러 개 걸린다.
    sentPly = s.ply;
    sentRetracted = retracted;
    // 앞선 타이머를 지운다. 개입이 오면 같은 手에 다시 두는데, 안 지우면 그 타이머가
    // 나중에 깨어나 물러진 수를 그대로 보낸다.
    clearTimeout(thinkTimer);
    thinkTimer = think(() => {
      if (counted) {
        return;
      }
      plies.add(1);
      sentAt = Date.now();
      ws.send(JSON.stringify({ type: 'move', usi }));
    });
  });

  // 끊기는 것도 결과다. 세션이 시한을 넘겨 닫히면 서버는 그 판을 abandoned 로 남긴다.
  ws.addEventListener('error', () => finish(''));
  ws.addEventListener('close', () => finish(''));
}
