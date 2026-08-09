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

/**
 * 제지형 개입 하나. **직전 수가 물러졌을 때만** 실려 오고, 다음 착수에서 서버가 지운다.
 *
 * 판단은 이미 서버에서 끝나 있다. 화면이 하는 일은 이걸 그리는 것뿐이고,
 * **최선수는 여기 없다** — 어느 수를 뒀어야 했는지는 알려주지 않는 것이 설계다
 * (docs/01-core.md §1).
 */
export interface Intervention {
  kind: 'blunder';
  /**
   * **왜** 나쁜가. 서버가 결정적 룰로 정한다(docs/01-core.md §3).
   *
   * 화면은 이걸로 문장을 짓지 않는다 — 문구는 `message`에 이미 들어 있고, 두 벌이
   * 되면 어긋났을 때 어느 쪽이 맞는지 알 수 없다. 연출을 카테고리별로 갈라야 할 때
   * 쓰는 자리다. 서버가 새 카테고리를 늘려도 화면이 안 깨지게 문자열로 열어 둔다.
   */
  category: string;
  /** 물러진 수의 USI. 판 위에서 어느 칸에서 어느 칸이었는지를 되짚는 데 쓴다. */
  retractedUsi: string;
  /** 그 수의 棋譜 표기(▲3三角成). 서버가 만든 것을 그대로 그린다. */
  retractedJa: string;
  /** 승률 낙폭(0~1). */
  deltaWin: number;
  /** 詰み을 놓쳐서 걸렸는가. */
  lostMate: boolean;
  /** 화면에 그대로 나가는 일본어 문구. */
  message: string;
  /**
   * 「상대는 이렇게 벌한다」. 물러진 수를 그대로 뒀을 때의 수순이고 **첫 수가 상대의
   * 수**다. 서버가 못 구했으면 아예 오지 않는다.
   *
   * **최선수가 아니다.** 이 수순이 시작하는 국면은 되물러서 이미 사라졌으므로 여기
   * 있는 어느 수도 「지금 이렇게 두라」가 되지 않는다. 카테고리가 이유를 못 대는
   * 국면에서도 이쪽은 나오고, 그게 이 절이 있는 이유다(docs/06-status.md §17).
   */
  refutation?: KifuMove[];
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
  /** 방금 둔 수를 판정하는 중. 이 동안은 `yourTurn`이 내려가 입력이 잠긴다. */
  judging: boolean;
  intervention?: Intervention;
}

export type ServerMessage =
  | { type: 'snapshot'; snapshot: Snapshot }
  | { type: 'error'; reason: string; message: string };

export type ClientMessage = { type: 'move'; usi: string } | { type: 'resign' };
