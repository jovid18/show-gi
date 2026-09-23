import type { Color } from '@/protocol/game';

/**
 * `aborted` 와 `expired` 는 둘 다 승패 없이 끝나지만 원인이 다르다.
 *
 * - `aborted`: 서버가 내려가 대국이 중단됐다.
 * - `expired`: 첫 수가 나오기 전에 시간이 다 됐다. 대국이 성립하지 않은 것으로 본다.
 *
 * 둘을 합치면 아무도 두지 않은 판에 「サーバーの都合」 문구가 떠서, 없는 서버 장애를 알린다.
 *
 * `timeout` 은 수를 둔 뒤에 시간을 넘긴 경우라 승패가 난다.
 */
export type MatchStatus =
  | 'playing'
  | 'checkmate'
  | 'stalemate'
  | 'resigned'
  | 'repetition'
  | 'timeout'
  | 'expired'
  | 'aborted';

/**
 * 先手/後手가 아니라 보는 사람 기준이다. 棋譜가 이 값으로 내 수와 상대 수를 다른 색으로
 * 칠한다(`Kifu` 의 `data-by`). 先手/後手로 바꾸면 두 사람 화면에서 같은 수가 같은 색이 된다.
 */
export type MatchSide = 'you' | 'opponent';

export interface MatchMove {
  usi: string;
  ja: string;
  by: MatchSide;
}

export interface MatchSnapshot {
  sfen: string;
  ply: number;
  turn: Color;
  yourTurn: boolean;
  inCheck: boolean;
  yourColor: Color;
  /** 상대 차례에는 `null` 이다(02-architecture.md §7 위협 1). */
  legalMoves: string[] | null;
  moves: MatchMove[] | null;
  status: MatchStatus;
  winner?: MatchSide;
  opponentName: string;
  opponentOnline: boolean;
  turnLimitMs: number;
  /** 지금 두는 쪽의 남은 시간이다. 내 시간인지는 `yourTurn` 으로 안다. */
  turnLeftMs: number;
}

export interface Room {
  id: string;
  yourColor: Color;
  hostName: string;
  /** 상대 자리가 비어 있는가. 상대가 입장한 뒤 연결이 끊긴 동안에는 `false` 다. */
  waiting: boolean;
  isHost: boolean;
}

export type MatchServerMessage =
  | { type: 'waiting'; room: Room }
  | { type: 'snapshot'; snapshot: MatchSnapshot }
  | { type: 'error'; reason: string; message: string }
  | { type: 'record'; gameId: number };

export type MatchClientMessage = { type: 'move'; usi: string } | { type: 'resign' };

/** `'r'` 은 振り駒. */
export type SeatChoice = Color | 'r';

export async function createRoom(choice: SeatChoice, signal: AbortSignal): Promise<Room> {
  const res = await fetch(`/api/rooms?color=${choice}`, { method: 'POST', signal });
  if (!res.ok) throw new Error(res.status === 401 ? 'ログインが必要です。' : '対局部屋を作れませんでした。');
  return (await res.json()) as Room;
}

/**
 * 볼 수 없는 방은 이유와 상관없이 404다. 없는 방·만료된 방·이미 찬 방·로그인하지 않은 요청을
 * 구분해 답하면, 방 id 를 하나씩 넣어 보며 있는 방을 찾아낼 수 있다.
 */
export async function fetchRoom(id: string, signal: AbortSignal): Promise<Room | null> {
  const res = await fetch(`/api/rooms/${encodeURIComponent(id)}`, { signal });
  if (res.status === 404) return null;
  if (!res.ok) throw new Error('対局部屋を読み込めませんでした。');
  return (await res.json()) as Room;
}
