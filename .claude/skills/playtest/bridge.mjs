// 플레이테스트용 대국 프록시.
//
// **이 파일의 존재 이유는 토큰이다.** 서버는 한 수마다 legalMoves 200개가 든 스냅샷을
// 통째로 보내는데, 그것을 에이전트 문맥에 그대로 넣으면 한 판에 수십만 토큰이 든다.
// 그래서 여기서 **판을 그리고 줄여서** 짧은 평문만 돌려주고, 원본 JSON은 전부
// jsonl 파일로 흘린다. 리포트는 문맥이 아니라 그 파일에서 만든다.
//
// 세션이 연결에 매여 있으므로(internal/server/ws.go) 이 프로세스가 살아 있는 동안만
// 대국이 유지된다. 에이전트 하나당 하나씩 띄운다.
//
//   PORT=9971 AGENT=a1 LOG=/tmp/a1.jsonl WS_URL=wss://show-gi.com/ws/game node bridge.mjs
//
//   curl -s      :9971/s          판
//   curl -sd 7g7f :9971/m         착수 (본문이 USI라 + · * 인코딩이 필요 없다)
//   curl -s      :9971/l          합법수 (요청할 때만)
//   curl -s      :9971/l?p=7f     그 칸의 합법수만
//   curl -s      :9971/why        직전 개입 전문
//   curl -s      :9971/f          최종 요약
//   curl -sX POST :9971/resign    투료

import http from 'node:http';
import fs from 'node:fs';

const WS_URL = process.env.WS_URL ?? 'ws://localhost:8081/ws/game';
const PORT = Number(process.env.PORT ?? 9971);
const AGENT = process.env.AGENT ?? 'agent';
const LOG = process.env.LOG ?? `/tmp/playtest-${AGENT}.jsonl`;
const MOVE_TIMEOUT_MS = Number(process.env.MOVE_TIMEOUT_MS ?? 240_000);

let snap = null;
let seq = 0;
let closed = null;
const waiters = [];
const seen = { interventions: [], hints: [], rejects: [] };
let lastIvKey = null;
let lastHintKey = null;

const logStream = fs.createWriteStream(LOG, { flags: 'a' });
const log = (dir, msg) => logStream.write(JSON.stringify({ t: Date.now(), agent: AGENT, dir, msg }) + '\n');

const ws = new WebSocket(WS_URL);
ws.addEventListener('open', () => log('sys', { open: WS_URL }));

ws.addEventListener('message', (ev) => {
  const m = JSON.parse(ev.data);
  log('in', m);
  if (m.type === 'snapshot') {
    snap = m.snapshot;
    seq++;
    // 개입·힌트는 스냅샷에 얹혀 오고 다음 착수에 지워진다. 리포트용으로 여기서 모은다.
    //
    // **같은 개입이 스냅샷 여러 장에 실려 온다.** 지워질 때까지 계속 얹혀 오므로 그대로
    // 밀어 넣으면 한 건이 여러 건으로 센다 (journal §91). 직전 것과 같으면 건너뛴다.
    if (snap.intervention) {
      const k = `${snap.ply}:${snap.intervention.retractedUsi}`;
      if (k !== lastIvKey) {
        lastIvKey = k;
        seen.interventions.push({ ply: snap.ply, ...snap.intervention });
      }
    }
    if (snap.hint) {
      const k = `${snap.ply}:${JSON.stringify(snap.hint)}`;
      if (k !== lastHintKey) {
        lastHintKey = k;
        seen.hints.push({ ply: snap.ply, ...snap.hint });
      }
    }
  } else if (m.type === 'error') {
    seen.rejects.push(m);
  }
  for (const w of waiters.splice(0)) w(m);
});

const die = (why) => () => {
  closed = why;
  log('sys', { closed: why });
  for (const w of waiters.splice(0)) w({ type: 'closed' });
};
ws.addEventListener('close', die('closed'));
ws.addEventListener('error', die('error'));

const next = () => new Promise((res) => waiters.push(res));

// 보낸 수가 반영될 때까지 기다린다. fromSeq 를 넘기지 않으면 서버에 닿기도 전에
// 「이미 내 차례」를 보고 그냥 돌아온다 — 실제로 그렇게 한 번 틀렸다.
async function settle(fromSeq) {
  const deadline = Date.now() + MOVE_TIMEOUT_MS;
  const errs = [];
  for (;;) {
    if (closed) return { errs, closed };
    if (errs.length) return { errs };
    const done =
      seq > fromSeq && snap && (snap.status !== 'playing' || (snap.yourTurn && !snap.thinking && !snap.judging));
    if (done) return { errs };
    if (Date.now() > deadline) return { errs, timeout: true };
    // 순서대로 기다리는 것이 요점이다 — 다음 프레임을 봐야 끝났는지 알 수 있다.
    // eslint-disable-next-line no-await-in-loop
    const m = await Promise.race([next(), new Promise((r) => setTimeout(() => r(null), 5000))]);
    if (m?.type === 'error') errs.push(m);
  }
}

// ── 렌더 ──────────────────────────────────────────────────────────────────
// 대문자 = 나(先手, 아래에서 위로), 소문자 = 상대. + 는 成.

// SFEN 持ち駒는 `[개수]駒` 의 나열이다 — `S5P2n2p` 는 S · 5P · 2n · 2p 다.
// 개수는 뒤따르는 駒의 것이라, 대소문자만으로 정규식을 가르면 경계에서 남의 개수가
// 내 쪽에 붙는다 (journal §91).
function parseHands(hands) {
  const mine = [];
  const theirs = [];
  for (const [, n, p] of hands.matchAll(/(\d*)([A-Za-z])/g)) {
    (p === p.toUpperCase() ? mine : theirs).push(n + p);
  }
  return [mine.join('') || '-', theirs.join('') || '-'];
}

function board(sfen) {
  const [b, , hands] = sfen.split(' ');
  const out = ['   9 8 7 6 5 4 3 2 1'];
  b.split('/').forEach((row, i) => {
    const cells = [];
    let promo = false;
    for (const ch of row) {
      if (ch === '+') {
        promo = true;
        continue;
      }
      if (/\d/.test(ch)) {
        for (let k = 0; k < +ch; k++) cells.push(' .');
        continue;
      }
      cells.push((promo ? '+' : ' ') + ch);
      promo = false;
    }
    out.push(` ${String.fromCharCode(97 + i)}${cells.join('')}  ${i + 1}`);
  });
  const [mine, theirs] = parseHands(hands);
  out.push(` hand you:${mine} opp:${theirs}`);
  return out.join('\n');
}

function state(extra = '') {
  if (closed) return `CLOSED ${closed}\n`;
  if (!snap) return 'no snapshot yet\n';
  const s = snap;
  const head = `ply ${s.ply} turn ${s.turn}${s.yourTurn ? ' YOU' : ''}${s.inCheck ? ' CHECK' : ''}${s.thinking ? ' thinking' : ''}`;
  const last = (s.moves ?? [])
    .slice(-2)
    .map((m) => m.ja ?? m.usi)
    .join(' ');
  const lines = [head, board(s.sfen)];
  if (last) lines.push(` last: ${last}`);
  lines.push(` legal ${s.legalMoves?.length ?? 0}`);
  if (s.status !== 'playing') lines.push(`STATUS ${s.status}${s.winner ? ` winner=${s.winner}` : ''}`);
  if (extra) lines.push(extra);
  return lines.join('\n') + '\n';
}

// 개입은 짧게. sfen·retractedSfen 은 문맥에 넣을 이유가 없다(jsonl 에 그대로 있다).
function intervention(iv) {
  if (!iv) return '';
  const line = (iv.refutation ?? []).map((m) => m.ja).join(' ');
  return [
    `!! RETRACTED ${iv.retractedJa}  ${iv.category}  -${Math.round(iv.deltaWin * 100)}%`,
    `   ${iv.message}`,
    line ? `   咎め: ${line}` : '',
  ]
    .filter(Boolean)
    .join('\n');
}

const hint = (h) =>
  h
    ? `?? HINT ${Object.entries(h)
        .map(([k, v]) => `${k}=${v}`)
        .join(' ')}`
    : '';

// ── HTTP ──────────────────────────────────────────────────────────────────

const body = (req) =>
  new Promise((res) => {
    let d = '';
    req.on('data', (c) => (d += c));
    req.on('end', () => res(d.trim()));
  });

http
  .createServer(async (req, res) => {
    const url = new URL(req.url, 'http://x');
    const send = (code, text) => {
      res.writeHead(code, { 'content-type': 'text/plain; charset=utf-8' });
      res.end(text);
    };

    if (url.pathname === '/s') return send(200, state());

    if (url.pathname === '/l') {
      const p = url.searchParams.get('p');
      const all = snap?.legalMoves ?? [];
      const list = p ? all.filter((m) => m.startsWith(p)) : all;
      const by = {};
      for (const m of list) (by[m.slice(0, 2)] ??= []).push(m.slice(2));
      return send(
        200,
        Object.entries(by)
          .map(([k, v]) => `${k}>${v.join(',')}`)
          .join('  ') + `\n(${list.length})\n`,
      );
    }

    if (url.pathname === '/why') {
      const iv = seen.interventions.at(-1);
      if (!iv) return send(200, 'no intervention yet\n');
      const line = (iv.refutation ?? [])
        .map((m, i) => `   ${i + 1}. ${m.ja}${m.checks?.length ? ' (王手)' : ''}`)
        .join('\n');
      return send(200, `${intervention(iv)}\n${line}\n`);
    }

    if (url.pathname === '/f') {
      const cat = {};
      for (const i of seen.interventions) cat[i.category] = (cat[i.category] ?? 0) + 1;
      return send(
        200,
        [
          `agent ${AGENT}`,
          `status ${snap?.status ?? '?'}${snap?.winner ? ` winner=${snap.winner}` : ''} ply ${snap?.ply ?? '?'}`,
          `interventions ${seen.interventions.length} ${JSON.stringify(cat)}`,
          `hints ${seen.hints.length} ${JSON.stringify(seen.hints)}`,
          `rejects ${seen.rejects.length}`,
          `log ${LOG}`,
        ].join('\n') + '\n',
      );
    }

    if (url.pathname === '/m') {
      const usi = (await body(req)) || url.searchParams.get('u');
      if (!usi) return send(400, 'usi required (send it as the request body)\n');
      if (closed) return send(409, `CLOSED ${closed}\n`);
      if (!snap?.legalMoves?.includes(usi)) return send(400, `ILLEGAL ${usi} — not in legalMoves. GET /l to look.\n`);
      const from = seq;
      const ivBefore = seen.interventions.length;
      log('out', { type: 'move', usi });
      ws.send(JSON.stringify({ type: 'move', usi }));
      const r = await settle(from);
      const parts = [];
      if (r.timeout) parts.push('!! TIMEOUT — 엔진이 오래 걸린다. /s 로 다시 본다');
      for (const e of r.errs) parts.push(`!! REJECTED ${e.reason} ${e.message}`);
      const iv = seen.interventions.length > ivBefore ? seen.interventions.at(-1) : null;
      if (iv) parts.push(intervention(iv));
      if (snap?.hint) parts.push(hint(snap.hint));
      return send(200, state(parts.join('\n')));
    }

    if (url.pathname === '/resign') {
      const from = seq;
      log('out', { type: 'resign' });
      ws.send(JSON.stringify({ type: 'resign' }));
      await settle(from);
      return send(200, state());
    }

    send(404, 'paths: /s /m /l /why /f /resign\n');
  })
  .listen(PORT, '127.0.0.1', () => console.log(`${AGENT} :${PORT} -> ${WS_URL}  log=${LOG}`));
