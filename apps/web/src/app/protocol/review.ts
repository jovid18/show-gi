// `/api/games`의 계약. 서버의 `internal/server/review.go`와 짝이다.
//
// 대국(`/ws/game`)과 달리 여기는 요청/응답이다 — 끝난 판은 스스로 움직이지 않으므로
// 사람이 넘길 때만 오간다.

import type { Player } from '@/protocol/game';

/** 사람이 어느 쪽을 잡았나. `b`가 先手다. */
export type MyColor = 'b' | 'w';

/** 사람 기준 결과. 끝나지 않은 판이면 아예 오지 않는다. */
export type GameResult = 'win' | 'loss' | 'draw' | 'abandoned';

export interface GameSummary {
  id: number;
  myColor: MyColor;
  /** RFC3339. 끝나지 않은 판에는 `finishedAt`이 없다 — 빈 값으로 오면 1970년이 그려진다. */
  startedAt: string;
  finishedAt?: string;
  result?: GameResult;
  moveCount: number;
  interventionCount: number;
}

/**
 * 기보의 한 수.
 *
 * `sfen`이 있어서 **화면은 수를 두지 않는다.** 대국의 반박 수순과 같은 자리다 —
 * 클라이언트가 스스로 두면 규칙 엔진을 한 벌 더 갖는 것이고, 어긋났을 때 어느 쪽이
 * 맞는지 알 수 없다.
 */
export interface ReviewMove {
  ply: number;
  usi: string;
  /** 棋譜 표기(▲7六歩). 서버가 만든 것을 그대로 그린다. */
  ja: string;
  by: Player;
  /**
   * 이 수를 **둔 뒤**의 국면.
   *
   * **없을 수 있다.** 기록이 중간에 끊기면 거기서부터 재현이 멈춘다. 그 수를 목록에서
   * 빼지는 않으므로(둔 것은 둔 것이다) 화면은 「판을 못 그리는 수」를 만난다.
   */
  sfen?: string;
  /** **플레이어 관점** cp. 없으면 그 手数에 평가치가 안 붙은 것이고, 0(호각)과 다르다. */
  evalCp?: number;
  /** 王手를 받고 있는 玉의 칸(`5a`). 서버가 짚는다 — 화면은 규칙을 모른다. */
  checked?: string;
}

/**
 * 물러진 수 하나.
 *
 * **기보에 없는 것이 여기에 있다.** `ply`는 물러진 수의 手数이고 그 수는 확정되지 않았으므로,
 * 그 국면을 보려면 `ply - 1` 手目의 판을 그려야 한다 — 물러진 수는 거기서 두어졌다.
 */
export interface ReviewIntervention {
  ply: number;
  kind: string;
  /** 기계용 코드. **화면에 안 나간다** — 나가는 것은 `categoryJa`다. */
  category: string;
  /** 카테고리의 짧은 이름(タダ捨て). 서버가 만든다. */
  categoryJa?: string;
  /** 왜 나빴는가. 서버가 만든 일본어 문구가 그대로 온다. */
  message?: string;
  /** 승률 낙폭(0~1). */
  deltaWin: number;
  levelBucket?: string;
  retractedUsi?: string;
  /** 물러진 수의 棋譜 표기. 그 국면까지 재현이 못 갔으면 없다. */
  retractedJa?: string;
}

export interface GameDetail extends GameSummary {
  /** 0手目의 국면. 手数를 되감으면 여기까지 온다. */
  startSfen: string;
  moves: ReviewMove[];
  interventions: ReviewIntervention[];
}

export interface GameListResponse {
  games: GameSummary[];
}

/** 서버가 실패를 말하는 모양. `message`는 그대로 화면에 나간다. */
export interface ApiError {
  error: string;
  message: string;
}
