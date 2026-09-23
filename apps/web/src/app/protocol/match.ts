// 대인전 계약이다. 서버 쪽 짝은 `internal/match` 와 `internal/server/ws_match.go` 다.
//
// 대인전에는 개입·힌트·待った·詰み 게이지·태그·상대 강함이 없고, 시계와 상대 접속 상태가
// 있다. 쓰지 않는 필드를 함께 쓰지 않도록 `protocol/game.ts` 와 나눈다.

import type { Color } from '@/protocol/game';

/**
 * 승패 없이 끝나는 상태가 둘이고, 화면 문구가 정반대라 따로 둔다.
 *
 * - `aborted`: 서버가 내려갔다. 양쪽 모두 과실이 없다.
 * - `expired`: 한 수도 두지 않은 채 시간이 다 됐다. 판이 없었던 것으로 보고 승패를 기록하지
 *   않는다.
 * - `timeout`: 수를 둔 뒤 시간을 넘겼다. 승부가 난다.
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
 * 기보의 한 수를 둔 쪽이다. 보는 사람 기준이다.
 *
 * 서버는 같은 기보를 두 관점으로 펼쳐 보낸다. 先手/後手 같은 절대 이름을 쓰면 두 화면이 같은
 * 수를 같은 색으로 표시한다.
 */
export type MatchSide = 'you' | 'opponent';

export interface MatchMove {
  usi: string;
  /** 棋譜 표기다(▲7六歩). 서버가 만든 값을 그대로 표시한다. */
  ja: string;
  by: MatchSide;
}

export interface MatchSnapshot {
  sfen: string;
  ply: number;
  turn: Color;
  yourTurn: boolean;
  inCheck: boolean;
  /** 한 판 동안 바뀌지 않는다. */
  yourColor: Color;
  /**
   * 자기 차례가 아니면 오지 않는다. 보내면 상대의 가능한 수를 볼 수 있어 대인전에서
   * 부정행위를 돕는다.
   */
  legalMoves: string[] | null;
  moves: MatchMove[] | null;
  status: MatchStatus;
  winner?: MatchSide;
  /**
   * 상대에 대해 오는 정보는 이것뿐이고 段級·전적은 보내지 않는다. 실력 프로파일은 본인만 보는
   * 값이다.
   */
  opponentName: string;
  /** 접속이 끊겨도 대국과 시계는 계속되고, 시간 초과로 끝난다. */
  opponentOnline: boolean;
  /** 한 수 제한 시간이다. 한 판 동안 바뀌지 않는다. */
  turnLimitMs: number;
  /**
   * 현재 수번의 남은 시간이다. 누구의 시간인지는 `yourTurn` 으로 안다.
   *
   * 정본은 서버 값이고 화면은 카운트만 한다(`useTurnClock`).
   */
  turnLeftMs: number;
}

export interface Room {
  id: string;
  yourColor: Color;
  hostName: string;
  /** 상대가 아직 들어오지 않았는지다. true 면 화면이 초대 링크를 표시한다. */
  waiting: boolean;
  /**
   * 보는 사람이 방을 만든 사람인지다. `waiting` 과 함께 보면 「아직 앉지 않은 손님」을
   * 가린다. 그 손님에게만 확인 화면을 표시한다.
   *
   * 앉는 순간 자리가 정해지고 시계가 시작된다.
   */
  isHost: boolean;
}

export type MatchServerMessage =
  | { type: 'waiting'; room: Room }
  | { type: 'snapshot'; snapshot: MatchSnapshot }
  | { type: 'error'; reason: string; message: string }
  // 판이 끝난 뒤 한 번 온다. 「振り返り」로 가는 링크를 만드는 데 쓴다.
  | { type: 'record'; gameId: number };

export type MatchClientMessage = { type: 'move'; usi: string } | { type: 'resign' };

/** 방을 만들 때 고르는 手番이다. `'r'` 은 振り駒이고 서버가 추첨한다(createRoom). */
export type SeatChoice = Color | 'r';

/**
 * 로그인하지 않았으면 401이다. 만든 사람이 手番을 고르고 상대는 나머지를 잡는다.
 *
 * 振り駒를 고르면 결과는 응답의 `yourColor` 로 오고, 만든 사람도 이때 안다.
 */
export async function createRoom(choice: SeatChoice, signal: AbortSignal): Promise<Room> {
  const res = await fetch(`/api/rooms?color=${choice}`, { method: 'POST', signal });
  if (!res.ok) throw new Error(res.status === 401 ? 'ログインが必要です。' : '対局部屋を作れませんでした。');
  return (await res.json()) as Room;
}

/**
 * 링크로 들어온 방을 확인한다. 자리를 확보하지 않는다. 착석은 WebSocket 을 연결할 때다.
 *
 * 없는 방·만료된 방·이미 찬 방·로그인하지 않은 요청은 모두 404 하나로 답한다. 나누어 답하면
 * 방 id 를 열거해 탐색할 수 있다.
 */
export async function fetchRoom(id: string, signal: AbortSignal): Promise<Room | null> {
  const res = await fetch(`/api/rooms/${encodeURIComponent(id)}`, { signal });
  if (res.status === 404) return null;
  if (!res.ok) throw new Error('対局部屋を読み込めませんでした。');
  return (await res.json()) as Room;
}
