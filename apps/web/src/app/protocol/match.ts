// 대인전의 계약. 서버의 `internal/match` · `internal/server/ws_match.go` 와 짝이다.
//
// 대인전에는 개입·힌트·待った·詰み 게이지·태그·상대의 강도가 없고, 대신 시계와 상대의 접속
// 상태가 있다. 쓰지 않는 필드를 함께 두지 않으려고 `protocol/game.ts` 와 나눴다.

import type { Color } from '@/protocol/game';

/**
 * 승패 없이 끝나는 경우가 둘이다. 화면에 띄울 말이 정반대라서 따로 둔다.
 *
 * - `aborted` — 서버가 내려갔다. 두 사람 모두 잘못이 없다
 * - `expired` — 한 수도 두지 않고 시간이 다 됐다. 아무도 두지 않은 판은 없었던 것으로 보고
 *   승패를 적지 않는다
 *
 * `timeout` 은 수를 둔 뒤에 시간을 넘긴 경우라서 승패가 난다.
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
 * 기보의 이 수를 누가 뒀나. 보는 사람을 기준으로 한다.
 *
 * 서버가 같은 기보를 두 사람의 관점으로 각각 바꿔 보낸다. 先手/後手 같은 절대 표기를 쓰면 두
 * 화면이 같은 수를 같은 색으로 그린다.
 */
export type MatchSide = 'you' | 'opponent';

export interface MatchMove {
  usi: string;
  /** 棋譜 표기(▲7六歩). 서버가 만든 문자열을 그대로 표시한다. */
  ja: string;
  by: MatchSide;
}

export interface MatchSnapshot {
  sfen: string;
  ply: number;
  turn: Color;
  yourTurn: boolean;
  inCheck: boolean;
  /** 이 사람이 맡은 쪽. 한 판 동안 바뀌지 않는다. */
  yourColor: Color;
  /**
   * 둘 수 있는 수. 자기 차례가 아니면 오지 않는다. 상대 차례에도 보내면 상대가 둘 수 있는
   * 수를 화면에서 들여다볼 수 있다. 대인전에서는 부정행위를 돕는 셈이다.
   */
  legalMoves: string[] | null;
  moves: MatchMove[] | null;
  status: MatchStatus;
  winner?: MatchSide;
  /**
   * 상대의 표시 이름. 상대에 대해 오는 정보는 이것뿐이다. 段級도 전적도 보내지 않는다(실력
   * 프로파일은 본인만 본다).
   */
  opponentName: string;
  /**
   * 상대가 접속해 있는가. 연결이 끊겨도 대국과 시계는 계속 진행되고, 판은 시간 초과로 끝난다.
   */
  opponentOnline: boolean;
  /** 한 수에 주어지는 시간(ms). 한 판 동안 바뀌지 않는다. */
  turnLimitMs: number;
  /**
   * 현재 차례에 남은 시간(ms). 누구 차례인지는 `yourTurn` 으로 안다. 기준은 서버의 값이고,
   * 화면은 그 값에서 시간을 세기만 한다(`useTurnClock`).
   */
  turnLeftMs: number;
}

/** 방 하나. id 외에는 아무 정보도 없다. */
export interface Room {
  id: string;
  /** 이 사람이 맡을 쪽. */
  yourColor: Color;
  /** 방을 만든 사람의 이름. */
  hostName: string;
  /** 아직 상대가 들어오지 않았는가. 참이면 화면에 초대 링크를 보여 준다. */
  waiting: boolean;
  /**
   * 보는 사람이 이 방을 만들었는가.
   *
   * `waiting` 과 함께 보고 「아직 입장하지 않은 손님」인지 가린다. 확인 화면은 그 사람에게만
   * 뜬다. 입장하는 순간 자리가 정해지고 시계가 돌기 시작한다.
   */
  isHost: boolean;
}

export type MatchServerMessage =
  | { type: 'waiting'; room: Room }
  | { type: 'snapshot'; snapshot: MatchSnapshot }
  | { type: 'error'; reason: string; message: string }
  // 판이 끝난 뒤 한 번 온다. 「振り返り」로 가는 링크를 이 값으로 만든다.
  | { type: 'record'; gameId: number };

export type MatchClientMessage = { type: 'move'; usi: string } | { type: 'resign' };

/** 방을 만들 때 고르는 手番. `'r'` 는 振り駒이고, 추첨은 서버가 한다(createRoom). */
export type SeatChoice = Color | 'r';

/**
 * 방을 만든다. 로그인하지 않았으면 401이다.
 *
 * 手番은 방을 만드는 사람이 고르고, 상대는 남은 쪽을 맡는다. 振り駒를 골랐으면 결과가 응답의
 * `yourColor` 에 있다. 만든 사람도 이때 처음 안다.
 */
export async function createRoom(choice: SeatChoice, signal: AbortSignal): Promise<Room> {
  const res = await fetch(`/api/rooms?color=${choice}`, { method: 'POST', signal });
  if (!res.ok) throw new Error(res.status === 401 ? 'ログインが必要です。' : '対局部屋を作れませんでした。');
  return (await res.json()) as Room;
}

/**
 * 링크로 들어온 방을 확인한다. 자리는 잡지 않는다. 입장은 WebSocket 이 연결될 때 한다.
 *
 * 볼 수 없는 방이면 무조건 404 다. 없는 방, 만료된 방, 다른 사람이 이미 들어간 방, 로그인하지
 * 않은 요청에 모두 같은 응답을 준다. 그래야 방 id 를 하나씩 넣어 보며 찾아낼 수 없다.
 */
export async function fetchRoom(id: string, signal: AbortSignal): Promise<Room | null> {
  const res = await fetch(`/api/rooms/${encodeURIComponent(id)}`, { signal });
  if (res.status === 404) return null;
  if (!res.ok) throw new Error('対局部屋を読み込めませんでした。');
  return (await res.json()) as Room;
}
