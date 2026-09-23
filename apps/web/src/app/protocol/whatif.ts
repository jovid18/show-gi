// 「そのとき、こう指していたら」의 계약. 서버의 `internal/server/whatif.go` 와 짝이다.
//
// 두 화면이 같은 타입을 쓴다. 되짚기 판(HTTP)과 대국 중 블런더 화면(WebSocket)은 전송 경로만
// 다르고, 주고받는 데이터는 같다. 따로 두면 같은 기능이 두 화면에서 조금씩 달라진다.

import type { Player } from '@/protocol/game';

/** 手番. SFEN·기록과 같은 한 글자 표기다. */
export type Turn = 'b' | 'w';

/**
 * 「ply 手目에서 이 수순을 뒀다면」.
 *
 * 판을 보내지 않는다. 시작 국면은 서버가 자기 기록의 수를 다시 둬서 만든다. SFEN 을 받는 API
 * 라면 아무 국면이나 평가해 주는 공개 엔진이 된다(whatif.go).
 *
 * `moves` 에는 사람이 양쪽 편을 번갈아 둔 수가 모두 들어 있다. 서버는 한 수도 대신 두지
 * 않는다.
 */
export interface WhatIfRequest {
  ply: number;
  moves: string[];
}

/**
 * 분기의 한 수. `ReviewMove` 와 필드 구성이 같다. 타입이 다르면 화면이 실제 기보와 가정
 * 수순을 따로 처리해야 한다.
 */
export interface WhatIfMove {
  ply: number;
  usi: string;
  ja: string;
  by: Player;
  /** 이 수를 둔 뒤의 국면. 화면은 이 값을 그대로 그린다. */
  sfen: string;
  checked?: string;
}

/**
 * 그 국면에서 차례인 쪽이 둘 수 있는 좋은 수 하나.
 *
 * 첫 번째가 최선수이고, 판 위에 초록 화살표로 그린다. 초록 화살표는 이미 「다음에 둘 수」를
 * 나타내는 표시이므로 새 표시를 만들지 않는다(03-frontend.md §2).
 */
export interface WhatIfCandidate {
  usi: string;
  ja: string;
  /**
   * 그 수를 둔 쪽 관점의 cp. 어느 쪽인지는 노드의 `turn` 으로 정해진다.
   *
   * 詰み이면 오지 않는다. `mateIn` 과 함께 오지 않으며, 저장할 때도 같은 규약을
   * 따른다(journal §131).
   */
  evalCp?: number;
  /**
   * 최선수 대비 낙폭(「이 수를 고르면 얼마를 내주나」).
   *
   * 최선수 자신(기준)과 詰み이 걸린 줄에는 없다. 詰み 手数와 cp 는 척도가 달라서 뺄 수
   * 없다(서버의 `candidatesOf`).
   */
  lossCp?: number;
  /** 詰み까지의 手数. 비어 있으면 詰み이 없고, 값이 있으면 `evalCp` 는 오지 않는다. */
  mateIn?: number;
}

/**
 * 분기 안에서 지금 보고 있는 국면. 국면 하나가 노드 하나다.
 *
 * 수순을 넘겨 보는 것도 새로 둬 보는 것도 결국 이 노드를 요청하는 일이다. 그래서 두 화면이
 * 같은 훅을 쓴다.
 */
export interface WhatIfNode {
  /** 분기가 시작된 手数. 「分岐の前へ」를 누르면 여기로 돌아간다. */
  basePly: number;
  ply: number;
  sfen: string;
  /**
   * 현재 手番. 합법수와 후보는 이쪽의 수다. 어느 駒台의 駒를 쓸 수 있는지도 이 값으로 정한다.
   */
  turn: Turn;
  yourTurn: boolean;
  checked?: string;
  status: 'playing' | 'checkmate' | 'stalemate' | 'resigned';
  /**
   * 화면에 규칙이 없으므로 서버가 보낸다. 대국 스냅샷의 `legalMoves` 와 같은 역할이다.
   *
   * `null` 로 올 수 있다(`protocol/game.ts` 머리말). 詰み·手詰まり 국면에는 둘 수 있는 수가
   * 없다. 타입에서 이를 감추면 `??` 사슬을 따라 대국 판의 합법수가 대신 쓰인다(journal §37).
   */
  legalMoves: string[] | null;
  /** 플레이어 관점 cp. 끝난 국면이면 없다. */
  evalCp?: number;
  mateIn?: number;
  line: WhatIfMove[];
  candidates: WhatIfCandidate[];
}
