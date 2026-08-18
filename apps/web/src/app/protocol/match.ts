// 대인전의 계약. 서버의 `internal/match` · `internal/server/ws_match.go` 와 짝이다.
//
// **`protocol/game.ts` 와 갈라 둔 이유는 여기 없는 것들 때문이다.** 개입·힌트·待った·
// 詰み 게이지·태그·상대의 강함이 전부 없고, 대신 시계와 상대의 접속이 있다 — 하나로
// 합치면 「대국 화면에 뜰 수 있는 것」이 실제로 뜨는 것보다 두 배가 된다.

import type { Color } from '@/protocol/game';

/**
 * 승패가 없는 끝이 **둘**이다. 갈라 두는 이유는 화면이 할 말이 정반대이기 때문이다.
 *
 * - `aborted` — 서버가 내려갔다. 두 사람 다 잘못한 것이 없다
 * - `expired` — **한 수도 안 둔 채** 시간이 다 됐다. 아무도 안 뒀으면 판이 없었던 것이라
 *   승패를 안 적는다(그래야 0手짜리 판이 두 사람의 전적에 win/loss 로 남지 않는다)
 *
 * `timeout` 은 **수를 두고 나서** 시간을 넘긴 것이고, 그쪽은 승부가 난다.
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
 * 기보의 한 수를 **누가 뒀나**. 절대 이름(先手/後手)이 아니라 **보는 사람 기준**이다.
 *
 * 대인전에는 사람이 둘이라, 서버가 같은 기보를 두 관점으로 펴서 보낸다 — 절대 이름을
 * 쓰면 두 화면이 같은 수를 같은 색으로 그린다.
 */
export type MatchSide = 'you' | 'opponent';

export interface MatchMove {
  usi: string;
  /** 棋譜 표기(▲7六歩). 서버가 만든 것을 그대로 그린다. */
  ja: string;
  by: MatchSide;
}

export interface MatchSnapshot {
  sfen: string;
  ply: number;
  turn: Color;
  yourTurn: boolean;
  inCheck: boolean;
  /** 이 사람이 잡은 쪽. 한 판에서 안 바뀐다. */
  yourColor: Color;
  /**
   * 둘 수 있는 수. **자기 차례가 아니면 안 온다** — 주면 상대의 수를 화면에서
   * 훑어볼 수 있고, 대인전에서 그건 그냥 부정행위 보조다.
   */
  legalMoves: string[] | null;
  moves: MatchMove[] | null;
  status: MatchStatus;
  winner?: MatchSide;
  /**
   * 상대의 표시 이름. **여기 오는 상대 정보는 이것 하나뿐이다** — 段級도 전적도 안 온다
   * (실력 프로파일은 본인만 보는 값이다).
   */
  opponentName: string;
  /**
   * 상대가 지금 화면을 보고 있는가.
   *
   * **판은 이 값과 무관하게 돈다.** 나가 있어도 시계는 흐르고, 그것이 판이 끝나는
   * 유일한 장치다.
   */
  opponentOnline: boolean;
  /** 한 수에 주는 시간(ms). 한 판에서 안 바뀐다. */
  turnLimitMs: number;
  /**
   * **지금 수번**에 남은 시간(ms). 누구의 것인지는 `yourTurn` 이 말한다.
   *
   * 서버가 정본이고 화면은 세기만 한다 — 남은 시간을 화면이 계산하면 탭을 멈춰 둔
   * 브라우저에서 시간이 안 간다.
   */
  turnLeftMs: number;
}

/** 방 하나. **id 말고는 아무것도 없다.** */
export interface Room {
  id: string;
  /** 이 사람이 잡을 쪽. */
  yourColor: Color;
  /** 방을 만든 사람의 이름. */
  hostName: string;
  /** 아직 상대가 안 들어왔는가. 참이면 화면이 초대 링크를 그린다. */
  waiting: boolean;
  /**
   * 보는 사람이 이 방을 만들었는가.
   *
   * **`waiting` 과 함께 「아직 안 앉은 손님」을 가른다.** 그 사람에게만 확인 화면이 뜬다 —
   * 자리에 앉는 순간 시계가 돌기 시작하므로, 링크를 잘못 누른 사람이 모르는 사이에
   * 남의 방 자리를 태우면 안 된다.
   */
  isHost: boolean;
}

export type MatchServerMessage =
  | { type: 'waiting'; room: Room }
  | { type: 'snapshot'; snapshot: MatchSnapshot }
  | { type: 'error'; reason: string; message: string }
  // 판이 끝난 뒤 **한 번** 온다. 「振り返り」로 건너가는 링크가 이 값으로 선다.
  | { type: 'record'; gameId: number };

export type MatchClientMessage = { type: 'move'; usi: string } | { type: 'resign' };

/** 방을 만들 때 고르는 手番. `'r'` 는 振り駒 — **뽑는 것은 서버다**(createRoom). */
export type SeatChoice = Color | 'r';

/**
 * 방을 연다. **로그인해야 한다** — 안 했으면 401이다.
 *
 * 手番은 방을 만드는 사람이 고르고, 상대는 나머지를 잡는다. 振り駒를 골랐으면 결과는
 * 돌아온 `yourColor` 에 있다 — 만든 사람도 그때 안다.
 */
export async function createRoom(choice: SeatChoice, signal: AbortSignal): Promise<Room> {
  const res = await fetch(`/api/rooms?color=${choice}`, { method: 'POST', signal });
  if (!res.ok) throw new Error(res.status === 401 ? 'ログインが必要です。' : '対局部屋を作れませんでした。');
  return (await res.json()) as Room;
}

/**
 * 링크로 들어온 방을 확인한다. **자리를 잡지 않는다** — 앉는 것은 WebSocket 이 붙을 때다.
 *
 * 볼 수 없으면 404 하나다. **왜인지는 안 갈라진다** — 없는 방·만료된 방·남이 이미 찬 방·
 * 로그인 안 한 요청이 전부 같은 답이라야 방 id 를 훑어보는 것이 성립하지 않는다.
 */
export async function fetchRoom(id: string, signal: AbortSignal): Promise<Room | null> {
  const res = await fetch(`/api/rooms/${encodeURIComponent(id)}`, { signal });
  if (res.status === 404) return null;
  if (!res.ok) throw new Error('対局部屋を読み込めませんでした。');
  return (await res.json()) as Room;
}
