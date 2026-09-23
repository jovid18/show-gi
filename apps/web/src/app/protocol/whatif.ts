import type { Player } from '@/protocol/game';

export type Turn = 'b' | 'w';

/**
 * 국면(SFEN)을 보내지 않는다. 서버가 자기 기록의 수를 `ply` 까지 다시 둬서 시작 국면을 만든다.
 * SFEN 을 받으면 아무 국면이나 평가해 주는 공개 엔진으로 쓰일 수 있다(whatif.go).
 *
 * `moves` 에는 양쪽 수가 모두 들어간다. 서버는 상대 쪽 수를 대신 두지 않는다.
 */
export interface WhatIfRequest {
  ply: number;
  moves: string[];
}

export interface WhatIfMove {
  ply: number;
  usi: string;
  ja: string;
  by: Player;
  sfen: string;
  checked?: string;
}

export interface WhatIfCandidate {
  usi: string;
  ja: string;
  /**
   * 노드의 `turn` 쪽 관점 cp 다. 노드의 `evalCp` 와 관점이 다르다.
   *
   * `mateIn` 과 함께 오지 않는다(journal §131).
   */
  evalCp?: number;
  /** 최선수와의 차이. 최선수 자신과 詰み이 걸린 수에는 없다. */
  lossCp?: number;
  mateIn?: number;
}

export interface WhatIfNode {
  /** 분기가 시작된 手数. `ply` 는 지금 보고 있는 手数다. */
  basePly: number;
  ply: number;
  sfen: string;
  /** 가정 수순에서는 사람이 양쪽을 두므로, 합법수·후보·쓸 수 있는 駒台는 이 값을 따른다. */
  turn: Turn;
  yourTurn: boolean;
  checked?: string;
  status: 'playing' | 'checkmate' | 'stalemate' | 'resigned';
  /**
   * 詰み·手詰まり 국면에서는 `null` 이다. `??` 로 대국 판의 합법수를 대신 쓰지 않는다(journal §37).
   */
  legalMoves: string[] | null;
  /** 플레이어 관점 cp. */
  evalCp?: number;
  mateIn?: number;
  line: WhatIfMove[];
  /** 첫 번째가 최선수다. */
  candidates: WhatIfCandidate[];
}
