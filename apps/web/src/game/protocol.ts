// `/ws/game`의 계약. 서버의 `internal/game/session.go`·`internal/server/ws.go`와 짝이다.
//
// Go의 nil 슬라이스는 `[]`가 아니라 **`null`로 직렬화된다.** 대국 시작 직후의 `moves`,
// 엔진 차례의 `legalMoves`가 실제로 그렇게 온다. 타입에서 그걸 숨기면 첫 렌더에서 터진다.

export type Status = 'playing' | 'checkmate' | 'stalemate' | 'resigned' | 'repetition';
export type Player = 'human' | 'engine';

export interface KifuMove {
  usi: string;
  /** 棋譜 표기(▲7六歩). 서버가 일본어로 주므로 그대로 그린다. */
  ja: string;
  by: Player;
}

export interface Snapshot {
  sfen: string;
  ply: number;
  turn: 'b' | 'w';
  yourTurn: boolean;
  inCheck: boolean;
  thinking: boolean;
  legalMoves: string[] | null;
  moves: KifuMove[] | null;
  status: Status;
  winner?: Player;
}

export type ServerMessage =
  | { type: 'snapshot'; snapshot: Snapshot }
  | { type: 'error'; reason: string; message: string };

export type ClientMessage = { type: 'move'; usi: string } | { type: 'resign' };
