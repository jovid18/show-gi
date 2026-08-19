// 「そのとき、こう指していたら」의 계약. 서버의 `internal/server/whatif.go` 와 짝이다.
//
// 두 화면이 같은 것을 쓴다. 되짚는 판(HTTP)과 대국 중의 블런더 화면(WebSocket)이 오가는
// 길만 다르고 주고받는 모양은 하나다 — 갈라 두면 같은 장치가 두 화면에서 다르게 자란다.

import type { Player } from '@/protocol/game';

/** 手番. SFEN·기록과 같은 한 글자다. */
export type Turn = 'b' | 'w';

/**
 * 「ply 手目에서 이 수순을 뒀다면」.
 *
 * 판을 보내지 않는다. 뿌리 국면은 서버가 자기 기록에서 다시 둬서 만든다 — SFEN을 받는
 * 표면이었다면 그건 아무 국면이나 깊이 12로 재 주는 공개 엔진이 된다(whatif.go).
 *
 * `moves` 에는 사람이 양쪽으로 둔 수가 전부 들어 있다. 서버는 한 수도 대신 두지 않는다.
 */
export interface WhatIfRequest {
  ply: number;
  moves: string[];
}

/** 분기의 한 수. `ReviewMove` 와 같은 어휘다 — 실제 기보와 가정 수순이 다른 타입이면 화면이 갈린다. */
export interface WhatIfMove {
  ply: number;
  usi: string;
  ja: string;
  by: Player;
  /** 이 수를 둔 뒤의 국면. 화면은 그대로 그린다. */
  sfen: string;
  checked?: string;
}

/**
 * 그 국면에서 수번 쪽이 둘 수 있는 좋은 수 하나.
 *
 * 첫 번째가 최선수이고 그것이 판 위의 초록 화살표다 — 「다음에 올 수」에 이미 배정된
 * 채널이라 새 신호를 꺼내지 않는다(03-frontend.md §2).
 */
export interface WhatIfCandidate {
  usi: string;
  ja: string;
  /** 그 수를 둔 쪽 관점 cp. 주인은 노드의 `turn` 이다. */
  evalCp: number;
  /**
   * 최선수 대비 낙폭 — 「이 수를 고르면 얼마를 내주나」.
   *
   * 없는 자리가 둘이다: 최선수 자신(기준)과 詰み이 섞인 줄. 뒤엣것은 cp가 환산값이라
   * 뺄셈이 낙폭이 아니게 된다(서버의 `candidatesOf`).
   */
  lossCp?: number;
  /** 詰み까지의 手数. 없으면 詰み이 아니다 — cp로 보내면 30000이 그대로 화면에 나간다. */
  mateIn?: number;
}

/**
 * 분기에서 지금 서 있는 자리. 국면 하나 = 노드 하나다.
 *
 * 넘겨 보는 것도 둬 보는 것도 이 하나를 묻는 일이고, 그래서 두 화면이 같은 훅을 쓴다.
 */
export interface WhatIfNode {
  /** 분기가 갈라져 나온 手数. 「分岐の前へ」가 돌아가는 자리다. */
  basePly: number;
  ply: number;
  sfen: string;
  /** 지금 手番. 합법수와 후보가 이 쪽의 것이고, 어느 駒台를 집을 수 있는지도 이 값이 정한다. */
  turn: Turn;
  yourTurn: boolean;
  checked?: string;
  status: 'playing' | 'checkmate' | 'stalemate' | 'resigned';
  /**
   * 화면이 규칙을 모르기 때문에 온다. 대국의 스냅샷과 같은 자리다.
   *
   * `null` 로 올 수 있다. 詰み·手詰まり 국면에는 둘 수가 없고, Go의 nil 슬라이스는
   * `[]` 가 아니라 `null` 로 직렬화된다 — 타입에서 그걸 숨기면 `?? ` 사슬이 대국 판의
   * 합법수로 흘러내린다(GameScreen에서 실제로 그랬다).
   */
  legalMoves: string[] | null;
  /** 플레이어 관점 cp. 끝난 국면이면 없다. */
  evalCp?: number;
  mateIn?: number;
  line: WhatIfMove[];
  candidates: WhatIfCandidate[];
}
