// 테스트가 서버 자리에 선다. 엔진과 DB 없이 화면이 프로토콜대로 그리는지만 본다.
//
// 메시지 타입을 앱의 protocol 에서 가져온다. 서버 모양이 바뀌어 화면 타입이 고쳐지면 여기
// 가짜도 typecheck 에서 같이 깨진다.

import type { Page, WebSocketRoute } from '@playwright/test';

import type { ClientMessage, ServerMessage, Snapshot } from '../src/app/protocol/game';

export const HIRATE = 'lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1';

/** 初手 ▲7六歩 뒤. */
export const AFTER_76 = 'lnsgkgsnl/1r5b1/ppppppppp/9/9/2P6/PP1PPPPPP/1B5R1/LNSGKGSNL w - 2';

/** ▲7六歩 △3四歩 뒤. */
export const AFTER_34 = 'lnsgkgsnl/1r5b1/pppppp1pp/6p2/9/2P6/PP1PPPPPP/1B5R1/LNSGKGSNL b - 3';

/** 화면이 駒를 집을 수 있게 하는 만큼만 둔다. 합법수 전부일 필요는 없다. */
const OPENING_MOVES = ['7g7f', '2g2f', '5i4h', '6i7h'];

export function snapshot(over: Partial<Snapshot> = {}): Snapshot {
  return {
    sfen: HIRATE,
    ply: 0,
    turn: 'b',
    yourColor: 'b',
    yourTurn: true,
    inCheck: false,
    thinking: false,
    legalMoves: OPENING_MOVES,
    moves: null,
    status: 'playing',
    judging: false,
    undoLeft: 3,
    canUndo: false,
    hintLeft: 6,
    canHint: true,
    ...over,
  };
}

/**
 * 첫 화면이 묻는 HTTP 를 답한다. 익명 사용자이고 이어할 판이 없다.
 *
 * 나머지 `/api` 는 404 다. 가로채지 않은 요청이 vite 프록시를 타고 로컬 api 에 붙으면 테스트가
 * 그 사람의 DB 에 따라 달라진다.
 */
export async function fakeApi(page: Page): Promise<void> {
  await page.route('**/api/**', (r) => r.fulfill({ status: 404, json: {} }));
  await page.route('**/api/me', (r) => r.fulfill({ json: { enabled: false, user: null } }));
  await page.route('**/api/openings', (r) => r.fulfill({ json: { openings: [] } }));
  await page.route('**/api/handicaps', (r) => r.fulfill({ json: { handicaps: [] } }));
  await page.route('**/api/resumable', (r) => r.fulfill({ json: { game: null } }));
}

type Received<T extends ClientMessage['type']> = Extract<ClientMessage, { type: T }>;

/**
 * `/ws/game` 의 서버 쪽. 연결 하나가 대국 하나다(useGame).
 *
 * 받은 메시지는 쌓아 두고 `next` 가 순서대로 꺼낸다. 화면이 보낸 것을 테스트가 확인한 뒤에
 * 답을 보내야 실제 서버와 같은 순서가 된다.
 */
export class FakeGame {
  private socket: WebSocketRoute | null = null;
  private opened: Array<(url: URL) => void> = [];
  private inbox: ClientMessage[] = [];
  private waiting: Array<{ type: string; resolve: (m: ClientMessage) => void }> = [];

  static async install(page: Page): Promise<FakeGame> {
    const game = new FakeGame();
    await page.routeWebSocket('**/ws/game**', (ws) => {
      game.socket = ws;
      ws.onMessage((raw) => game.receive(JSON.parse(String(raw)) as ClientMessage));
      for (const resolve of game.opened.splice(0)) resolve(new URL(ws.url()));
    });
    return game;
  }

  /** 화면이 붙을 때까지 기다리고 그 주소를 돌려준다. 시작 설정이 쿼리에 실린다. */
  connected(): Promise<URL> {
    if (this.socket) return Promise.resolve(new URL(this.socket.url()));
    return new Promise((resolve) => this.opened.push(resolve));
  }

  send(msg: ServerMessage): void {
    if (!this.socket) throw new Error('화면이 아직 붙지 않았다');
    this.socket.send(JSON.stringify(msg));
  }

  snapshot(over: Partial<Snapshot> = {}): void {
    this.send({ type: 'snapshot', snapshot: snapshot(over) });
  }

  /** 화면이 보낸 다음 `type` 메시지. 그 앞의 다른 종류는 건너뛰지 않고 남겨 둔다. */
  next<T extends ClientMessage['type']>(type: T): Promise<Received<T>> {
    const at = this.inbox.findIndex((m) => m.type === type);
    if (at !== -1) return Promise.resolve(this.inbox.splice(at, 1)[0] as Received<T>);
    return new Promise((resolve) => this.waiting.push({ type, resolve: (m) => resolve(m as Received<T>) }));
  }

  private receive(msg: ClientMessage): void {
    const at = this.waiting.findIndex((w) => w.type === msg.type);
    if (at === -1) {
      this.inbox.push(msg);
      return;
    }
    this.waiting.splice(at, 1)[0]?.resolve(msg);
  }
}
