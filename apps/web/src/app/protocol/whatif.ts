// 「そのとき、こう指していたら」 계약이다. 서버 쪽 짝은 `internal/server/whatif.go` 다.
//
// 되짚기 판(HTTP)과 대국 중 블런더 화면(WebSocket)이 함께 쓴다. 경로만 다르고 주고받는 형태는
// 하나다. 나누면 같은 기능이 두 화면에서 따로 바뀐다.

import type { Player } from '@/protocol/game';

/** 手番이다. SFEN 과 기록에서 쓰는 한 글자 표기와 같다. */
export type Turn = 'b' | 'w';

/**
 * 「ply 手目에서 이 수순을 뒀다면」을 묻는 요청이다. 판은 보내지 않고, 시작 국면은 서버가
 * 자기 기록을 재생해 만든다. SFEN 을 받는 엔드포인트를 두면 아무 국면이나 평가하는 공개
 * 엔진으로 쓰일 수 있다(whatif.go).
 *
 * `moves` 는 사람이 양쪽 모두 둔 수 전부다. 서버는 한 수도 대신 두지 않는다.
 */
export interface WhatIfRequest {
  ply: number;
  moves: string[];
}

/**
 * 분기의 한 수다. `ReviewMove` 와 같은 낱말을 쓴다. 타입이 다르면 화면이 실제 기보와 가정
 * 수순을 따로 처리해야 한다.
 */
export interface WhatIfMove {
  ply: number;
  usi: string;
  ja: string;
  by: Player;
  /** 이 수를 둔 뒤의 국면이다. */
  sfen: string;
  checked?: string;
}

/**
 * 그 국면에서 수번인 쪽의 좋은 수 하나다. 첫 번째가 최선수이고 판 위의 초록 화살표로 그린다.
 *
 * 초록 화살표는 이미 「다음에 올 수」에 배정되어 있어 새 표시를 만들지 않는다(03-frontend.md
 * §2).
 */
export interface WhatIfCandidate {
  usi: string;
  ja: string;
  /**
   * 그 수를 둔 쪽 관점 cp 다. 어느 쪽 관점인지는 노드의 `turn` 이 정한다.
   *
   * 詰み이면 오지 않는다. `mateIn` 과 배타이고 저장하는 쪽도 같은 규약이다(journal §131).
   */
  evalCp?: number;
  /**
   * 최선수 대비 낙폭이다(「이 수를 고르면 얼마를 내주나」). 최선수 자신과 詰み이 섞인 줄에는
   * 없다.
   *
   * 詰み와 cp 는 척도가 달라 뺄 수 없다(서버의 `candidatesOf`).
   */
  lossCp?: number;
  /** 詰み까지의 手数다. 비어 있으면 詰み이 없다. */
  mateIn?: number;
}

/**
 * 분기 안의 현재 위치다. 국면 하나가 노드 하나다.
 *
 * 넘겨 보기와 둬 보기가 모두 이 노드를 조회하므로 두 화면이 같은 훅을 쓴다.
 */
export interface WhatIfNode {
  /** 분기를 시작한 手数다. 「分岐の前へ」가 돌아가는 지점이다. */
  basePly: number;
  ply: number;
  sfen: string;
  /** 현재 手番이다. 합법수와 후보는 이 쪽의 것이고, 집을 수 있는 駒台도 이 값으로 정한다. */
  turn: Turn;
  yourTurn: boolean;
  checked?: string;
  status: 'playing' | 'checkmate' | 'stalemate' | 'resigned';
  /**
   * 화면이 규칙을 모르므로 서버가 보낸다. 대국 스냅샷과 같은 방식이다.
   *
   * 詰み·手詰まり 국면에는 가능한 수가 없어 `null` 일 수 있다(`protocol/game.ts` 머리말).
   * 타입에서 숨기면 `??` 사슬이 대국 판의 합법수를 대신 쓴다(journal §37).
   */
  legalMoves: string[] | null;
  /** 플레이어 관점 cp 다. 종료 국면에서는 없다. */
  evalCp?: number;
  mateIn?: number;
  line: WhatIfMove[];
  candidates: WhatIfCandidate[];
}
