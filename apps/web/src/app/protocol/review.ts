// `/api/games` 의 계약. 서버의 `internal/server/review.go` 와 짝이다.
//
// 대국(`/ws/game`)과 달리 요청/응답 방식이다. 끝난 대국을 조회할 때 받아 온다.

import type { Player } from '@/protocol/game';

/** 사람이 어느 쪽을 맡았나. `b` 가 先手다. */
export type MyColor = 'b' | 'w';

/** 사람 기준의 결과. 끝나지 않은 판이면 오지 않는다. */
export type GameResult = 'win' | 'loss' | 'draw' | 'abandoned';

export interface GameSummary {
  id: number;
  myColor: MyColor;
  /**
   * RFC3339. 끝나지 않은 판에는 `finishedAt` 이 없다. 빈 값으로 보내면 화면에 1970년이
   * 찍힌다.
   */
  startedAt: string;
  finishedAt?: string;
  result?: GameResult;
  moveCount: number;
  interventionCount: number;
  /**
   * 그 판의 手合割 이름(二枚落ち). 平手면 오지 않는다. 이름 끝에 `Ja` 를 붙이는 이유는
   * `Snapshot.handicapJa` 에 있다.
   *
   * 이 줄이 없으면 駒落ち 판의 형세 그래프가 왜 +2000대에서 시작하는지 화면 어디서도 알 수
   * 없다. 그러면 되짚는 사람이 그 우세를 자기 실력으로 착각한다.
   */
  handicapJa?: string;
  /**
   * 사람과 둔 판인가.
   *
   * 없으면 AI 연습 대국이다(서버는 false 를 보내지 않는다). 대인전 중에는 엔진이 판정하지
   * 않는다. 그래서 개입 0건이나 빈 평가치를 「블런더가 없었다」로 표시하지 않는다(journal
   * §83).
   */
  isMatch?: boolean;
  /**
   * 밖에서 둔 판을 가져온 것인가.
   *
   * 없으면 여기서 둔 판이다(서버는 false 를 보내지 않는다). 가져온 판에도 평가치와 개입
   * 기록이 있지만, 실제로 수를 물리지는 않았다. 기보에 남은 수이므로 표식을 「介入」 대신
   * 「悪手」로 표시한다(journal §126).
   */
  imported?: boolean;
  /**
   * 지금 평가치를 채우는 중인가. 대인전에만 온다.
   *
   * 대인전은 두는 동안 엔진을 돌리지 않으므로 평가치가 비어 있다. 판이 끝나면 서버가 채운다.
   * 기보는 이를 기다리지 않는다. 이 값이 참인 동안에도 판과 棋譜는 그대로 보인다.
   *
   * 이 상태는 서버 메모리에만 있어서 배포하면 끊긴다. 그러면 이 값이 사라지고 화면은
   * 「평가치가 남지 않았다」로 돌아간다. 실제로도 그 판에는 평가치가 붙지 않는다.
   */
  analyzing?: boolean;
}

/**
 * 기보의 한 수.
 *
 * 수마다 `sfen` 이 있어 화면이 직접 수를 두지 않는다. 대국의 반박 수순(`RefutationMove`)과
 * 같은 설계다.
 */
export interface ReviewMove {
  ply: number;
  usi: string;
  /** 棋譜 표기(▲7六歩). 서버가 만든 문자열을 그대로 표시한다. */
  ja: string;
  by: Player;
  /**
   * 이 수를 둔 뒤의 국면.
   *
   * 없을 수 있다. 기록이 중간에 끊기면 그 지점부터 재현이 멈춘다. 이미 둔 수라서 목록에서
   * 빼지는 않는다. 그래서 화면은 판을 그릴 수 없는 수를 만날 수 있다.
   */
  sfen?: string;
  /**
   * 플레이어 관점 cp. 없으면 그 手数에 평가치가 없거나 詰み이다. 0(호각)과는 다르다.
   *
   * 詰み이면 이 필드가 비고 `mateIn` 이 채워진다. 두 필드가 함께 오지 않는 것은 서버 쪽
   * 규약이다(`store.Candidate`). 화면은 언제나 `mateIn` 을 먼저 확인한다.
   */
  evalCp?: number;
  /** 詰み까지의 手数(플레이어 관점). 양수면 내가 詰ます 쪽이다. */
  mateIn?: number;
  /** 王手를 받고 있는 玉의 칸(`5a`). 화면에는 규칙이 없으므로 서버가 알려 준다. */
  checked?: string;
  /**
   * 사람이 둔 이 수가 好手였는가. 판정은 서버가 한다(`intervene.IsGood`).
   *
   * 오지 않으면 好手가 아니다. 대인전과 `024_move_good.sql` 이전의 판은 판정이 없으므로 늘
   * 오지 않는다.
   */
  good?: boolean;
}

/**
 * 물린 수 하나.
 *
 * 기보에 없는 수가 여기에 있다. `ply` 는 물린 수의 手数이고, 그 수는 확정되지 않았다. 물린
 * 수는 `ply - 1` 手目 국면에서 뒀으므로 그 국면을 보려면 `ply - 1` 手目의 판을 그린다.
 */
export interface ReviewIntervention {
  ply: number;
  kind: string;
  /** 기계용 코드. 화면에 표시하지 않는다. 표시하는 것은 `categoryJa` 다. */
  category: string;
  /** 카테고리의 짧은 이름(タダ捨て). 서버가 만든다. */
  categoryJa?: string;
  /** 이 수가 나빴던 이유. 서버가 만든 일본어 문구가 그대로 온다. */
  message?: string;
  /** 승률 낙폭(0~1). */
  deltaWin: number;
  levelBucket?: string;
  retractedUsi?: string;
  /**
   * 그 수를 둔 뒤의 평가치(플레이어 관점 cp). `moves[].evalCp` 와 같은 척도다.
   *
   * `005_intervention_cp.sql` 이전에 기록된 판에는 없고, 앞으로도 채울 수 없다. 그때 남긴
   * 낙폭은 승률 차이라서 여기서 cp 를 되살릴 수 없다(미지수 둘에 식 하나). 화면이 그 자리를
   * 다시 계산해 채운다(`useMoveEvals`).
   */
  afterCp?: number;
  /** 그 수 뒤 詰み까지의 手数(플레이어 관점). `afterCp` 와 함께 오지 않는다. */
  afterMate?: number;
  /**
   * 판정 당시 최선수의 cp(플레이어 관점). 낙폭을 다시 구하려면 이 값과 `afterCp` 가 필요하다.
   */
  bestCp?: number;
  /** 물린 수의 棋譜 표기. 재현이 그 국면까지 이르지 못했으면 없다. */
  retractedJa?: string;
}

/**
 * 사람이 스스로 무른 수 하나(待った).
 *
 * `ReviewIntervention` 과 따로 둔다. 개입은 AI가 막은 수이고, 무르기는 사람이 스스로 되돌린
 * 수다. 되짚기에서 두 기록이 전하는 이야기가 정반대다. 무르기는 판정하지 않으므로 카테고리도
 * 문구도 없다.
 */
export interface ReviewUndo {
  /** 무른 수의 手数. 그 수는 기보에 없으므로 해당 국면은 `ply-1` 手目의 판이다. */
  ply: number;
  usi: string;
  /** 무른 수의 棋譜 표기. 재현이 그 국면까지 이르지 못했으면 없다. */
  ja?: string;
  /** 그 수 뒤의 평가치(플레이어 관점 cp). 무를 때 판정이 아직 끝나지 않았으면 없다. */
  evalCp?: number;
  /** 詰み까지의 手数(플레이어 관점). `ReviewMove` 와 같은 규약이다. */
  mateIn?: number;
}

export interface GameDetail extends GameSummary {
  /** 0手目의 국면. 수를 처음까지 되감으면 이 국면이 된다. */
  startSfen: string;
  /**
   * 이 판의 「형세 0」(플레이어 관점 cp). 平手면 오지 않는다.
   *
   * `evalCp` 와 관점이 같으므로 그대로 빼면 된다. 이 값을 빼는 곳은 형세
   * 그래프(`EvalGraph`)와 후보 줄의 색(`evalTone`)이다.
   *
   * 빼지 않으면 駒落ち 판의 곡선이 천장에 붙고, 「호각」 선이 핸디캡을 다 잃은 지점에
   * 그려진다. 후보 줄은 모두 가장 짙은 파랑이 된다.
   */
  baselineCp?: number;
  moves: ReviewMove[];
  interventions: ReviewIntervention[];
  /**
   * 사람이 스스로 무른 수들. 옛 판에서는 빈 배열이다. `008_game_undos.sql` 이전에 둔 판에는
   * 이 기록이 아예 없다.
   */
  undos: ReviewUndo[];
}

export interface GameListResponse {
  games: GameSummary[];
}

/** 서버의 오류 응답 형태. `message` 는 화면에 그대로 표시한다. */
export interface ApiError {
  error: string;
  message: string;
}
