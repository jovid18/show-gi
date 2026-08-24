// 대인전 한 판. 줄에 서서 짝을 기다리고, 잡히면 그 방에서 끝까지 둔다.
//
// 로그인이 필요하다. 쿠키는 우리가 굽는다(lib/session.js) — Google 왕복이 없으므로
// 헤드리스에서 그대로 돈다.
//
// VU 하나가 사람 하나다. 그래서 LT_UIDS 가 VU 수보다 적으면 두 VU 가 같은 사람으로
// 줄에 서고, 큐가 사람마다 한 행이라(match_queue 의 PK) 둘이 서로를 못 만난다.
import http from 'k6/http';
import { sleep } from 'k6';
import { WebSocket } from 'k6/experimental/websockets';
import { clearTimeout, setTimeout } from 'k6/timers';

import {
  BASE,
  GAME_TIMEOUT_MS,
  LT_UIDS,
  MAX_PLIES,
  QUEUE_INTERVAL,
  QUEUE_TRIES,
  SEED,
  STALL_TIMEOUT_MS,
  SESSION_SECRET,
  WS_BASE,
} from './lib/config.js';
import { games, moveCycle, plies, queueTimeouts, queueWait, rejects, stalls } from './lib/metrics.js';
import { makeRNG, pickMove } from './lib/moves.js';
import { cookieHeader, mintSession } from './lib/session.js';
import { think } from './lib/think.js';

export default function humanMatch() {
  if (LT_UIDS.length === 0) {
    throw new Error('LT_UIDS 가 비어 있다. seed.sql 이 찍어 준 번호를 넘긴다');
  }
  const uid = LT_UIDS[(__VU - 1) % LT_UIDS.length];
  const headers = cookieHeader(mintSession(SESSION_SECRET, uid, `LT${uid}`, 3600));

  const joinedAt = Date.now();
  let room = '';
  for (let i = 0; i < QUEUE_TRIES; i++) {
    // POST 하나가 셋을 한다 — 줄에 서기·살아 있다고 알리기·짝짓기(api.md §2).
    const res = http.post(`${BASE}/api/queue`, null, { headers, tags: { name: 'POST /api/queue' } });
    if (res.status !== 200) {
      rejects.add(1, { reason: `queue_${res.status}` });
      // 물러나기 전에 한 박자 쉰다. 즉시 돌아가면 k6 가 곧바로 다음 이터레이션을 시작해서
      // 같은 요청을 다시 보내고, 서버가 내려가 있는 동안 VU 수만큼 초당 수십 번이 된다 —
      // 한 번 그렇게 재서 1분짜리 장애가 회차의 96.9% 실패로 읽혔다(journal §107).
      sleep(QUEUE_INTERVAL);
      return;
    }
    const body = res.json();
    if (body.status === 'matched') {
      room = body.roomId;
      break;
    }
    sleep(QUEUE_INTERVAL);
  }
  if (room === '') {
    // 짝이 안 잡힌 것도 결과다. 줄에서 스스로 빠진다 — 안 빠지면 다음 회차의
    // 첫 짝이 이 유령과 잡힌다(seen_at 이 낡기까지 12초가 걸린다).
    queueTimeouts.add(1);
    http.del(`${BASE}/api/queue`, null, { headers });
    return;
  }
  queueWait.add(Date.now() - joinedAt);
  play(room, headers, uid);
}

// play 는 그 방에서 끝까지 둔다. 개입도 힌트도 待った도 없는 프로토콜이다(api.md §6).
function play(room, headers, uid) {
  const rng = makeRNG(SEED * 7919 + uid * 104729 + __ITER);
  const ws = new WebSocket(`${WS_BASE}/ws/match?room=${room}`, null, { headers });

  let sentAt = 0;
  let sentPly = -1;
  let counted = false;
  let stallTimer = 0;
  let gameTimer = 0;
  let thinkTimer = 0;

  // cut 은 무엇이 끊었나다 — 빈 문자열이면 판이 스스로 끝난 것이고, 아니면 시한이다.
  const finish = (cut) => {
    clearTimeout(stallTimer);
    clearTimeout(gameTimer);
    clearTimeout(thinkTimer);
    if (!counted) {
      counted = true;
      games.add(1, { kind: 'match' });
      if (cut !== '') {
        stalls.add(1, { kind: 'match', cut });
      }
    }
    ws.close();
  };

  // 대인전에는 시계가 있다(1手 60초). 그래도 우리 쪽 시한을 두는 이유는 상대가 우리
  // 자신이기 때문이다 — 한쪽 VU 가 멈추면 다른 쪽은 시간패를 기다리며 자리를 잡고 있다.
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
      rejects.add(1, { reason: msg.reason });
      return;
    }
    // record 는 판이 끝난 뒤 한 번 온다. 총평이 아니라 되짚기로 갈 번호다.
    if (msg.type === 'record') {
      finish('');
      return;
    }
    if (msg.type !== 'snapshot') {
      return;
    }

    const s = msg.snapshot;
    if (s.status !== 'playing') {
      finish('');
      return;
    }
    if (!s.yourTurn) {
      return;
    }
    // 같은 手에 두 번 보내지 않는다. 상대가 접속을 놓았다 붙는 것만으로도 스냅샷이
    // 다시 나가고(opponentOnline), 그 두 번째 착수는 not_your_turn 이다.
    if (sentPly === s.ply) {
      return;
    }

    if (sentAt > 0) {
      moveCycle.add(Date.now() - sentAt, { kind: 'match' });
      sentAt = 0;
    }
    if (s.ply >= MAX_PLIES) {
      ws.send(JSON.stringify({ type: 'resign' }));
      return;
    }

    const usi = pickMove(s.legalMoves, rng);
    if (!usi) {
      ws.send(JSON.stringify({ type: 'resign' }));
      return;
    }
    // 手数는 여기서 잠근다. 미루는 사이에도 스냅샷이 오므로(opponentOnline) 잠그지
    // 않으면 같은 手에 타이머가 여러 개 걸린다.
    sentPly = s.ply;
    // 앞선 타이머를 지운다. 지금은 같은 手에 두 번 여기까지 오지 않지만, 가드가
    // 바뀌는 날 조용히 두 번 보내는 쪽으로 깨진다.
    clearTimeout(thinkTimer);
    thinkTimer = think(() => {
      if (counted) {
        return;
      }
      plies.add(1, { kind: 'match' });
      sentAt = Date.now();
      ws.send(JSON.stringify({ type: 'move', usi }));
    });
  });

  ws.addEventListener('error', () => finish(''));
  ws.addEventListener('close', () => finish(''));
}
