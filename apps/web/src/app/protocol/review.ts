import type { Player } from '@/protocol/game';

export type MyColor = 'b' | 'w';

/** 사람 기준 결과. */
export type GameResult = 'win' | 'loss' | 'draw' | 'abandoned';

export interface GameSummary {
  id: number;
  myColor: MyColor;
  startedAt: string;
  /** 끝나지 않은 판에는 없다. 빈 문자열로 보내면 1970년으로 표시된다. */
  finishedAt?: string;
  result?: GameResult;
  moveCount: number;
  interventionCount: number;
  handicapJa?: string;
  /**
   * 없으면 AI 연습 대국이다. 서버는 `false` 를 보내지 않는다.
   *
   * 대인전은 두는 동안 엔진 판정이 없다. 개입 0건이나 빈 평가치를 「블런더가 없었다」로 표시하지
   * 않는다(journal §83).
   */
  isMatch?: boolean;
  /**
   * 없으면 여기서 둔 판이다. 서버는 `false` 를 보내지 않는다.
   *
   * 가져온 판의 개입 기록은 실제로 수를 되돌리지 않았다. 표식은 「介入」 대신
   * 「悪手」로 한다(journal §126).
   */
  imported?: boolean;
  /**
   * 대인전 판의 평가치를 서버가 채우는 중인가. 이 상태는 서버 메모리에만 있어 배포하면 분석이
   * 끊기고 이 값도 사라진다. 그 판에는 평가치가 붙지 않는다.
   */
  analyzing?: boolean;
}

export interface ReviewMove {
  ply: number;
  usi: string;
  ja: string;
  by: Player;
  /**
   * 화면은 수를 직접 두지 않고 이 값을 쓴다. 기록이 중간에 끊기면 그 뒤 수에는 없지만, 수
   * 자체는 목록에 남는다.
   */
  sfen?: string;
  /**
   * 플레이어 관점 cp. 없으면 0 이 아니다(평가가 없거나 詰み이다). `mateIn` 과 함께 오지
   * 않는다.
   */
  evalCp?: number;
  /** 양수면 내가 詰ます 쪽이다. */
  mateIn?: number;
  checked?: string;
  /** 대인전과 `024_move_good.sql` 이전 판은 판정 자체가 없어 늘 오지 않는다. */
  good?: boolean;
}

export interface ReviewIntervention {
  /** 물러진 수의 手数. 그 수는 기보에 없으므로 물러진 국면은 `ply - 1` 手目의 판이다. */
  ply: number;
  kind: string;
  /** 화면에는 `categoryJa` 를 쓴다. */
  category: string;
  categoryJa?: string;
  message?: string;
  /** 0~1. */
  deltaWin: number;
  levelBucket?: string;
  retractedUsi?: string;
  /**
   * 플레이어 관점 cp. `005_intervention_cp.sql` 이전 판에는 없다. `deltaWin` 은 승률 차라
   * 이 값을 되살릴 수 없다(미지수 둘에 식 하나).
   */
  afterCp?: number;
  /** 플레이어 관점. `afterCp` 와 함께 오지 않는다. */
  afterMate?: number;
  /** 플레이어 관점 cp. */
  bestCp?: number;
  retractedJa?: string;
}

/**
 * 사람이 스스로 무른 수(待った). 개입으로 물러진 수(`ReviewIntervention`)와 합치지 않는다.
 * 무르기에는 판정이 없어 카테고리도 문구도 없다.
 */
export interface ReviewUndo {
  /** `ReviewIntervention.ply` 와 같은 규약이다. */
  ply: number;
  usi: string;
  ja?: string;
  /** 플레이어 관점 cp. */
  evalCp?: number;
  /** `ReviewMove.mateIn` 과 같은 규약이다. */
  mateIn?: number;
}

export interface GameDetail extends GameSummary {
  startSfen: string;
  /**
   * 駒落ち 판의 「형세 0」(플레이어 관점 cp). 형세를 그릴 때는 `evalCp` 에서 이 값을 뺀다.
   * 빼지 않으면 형세 그래프가 천장에 붙는다.
   */
  baselineCp?: number;
  moves: ReviewMove[];
  interventions: ReviewIntervention[];
  /** `008_game_undos.sql` 이전 판은 기록이 없어 빈 배열이다. 무르지 않았다는 뜻이 아니다. */
  undos: ReviewUndo[];
}

export interface GameListResponse {
  games: GameSummary[];
}

export interface ApiError {
  error: string;
  message: string;
}
