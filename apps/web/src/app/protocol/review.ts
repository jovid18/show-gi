// `/api/games` 계약이다. 서버 쪽 짝은 `internal/server/review.go` 다.
//
// 대국(`/ws/game`)과 달리 요청/응답 방식이고, 끝난 대국을 볼 때 가져온다.

import type { Player } from '@/protocol/game';

/** 사람이 잡은 쪽이다. `b` = 先手다. */
export type MyColor = 'b' | 'w';

/** 사람 기준 결과다. 끝나지 않은 판은 필드 자체가 오지 않는다. */
export type GameResult = 'win' | 'loss' | 'draw' | 'abandoned';

export interface GameSummary {
  id: number;
  myColor: MyColor;
  /**
   * RFC3339 형식이다. 끝나지 않은 판은 `finishedAt` 이 없다. 빈 값을 보내면 1970 년으로
   * 표시한다.
   */
  startedAt: string;
  finishedAt?: string;
  result?: GameResult;
  moveCount: number;
  interventionCount: number;
  /**
   * 그 판의 手合割 이름이다(二枚落ち). 平手면 오지 않는다. `Ja` 접미 규약은
   * `Snapshot.handicapJa` 와 같다.
   *
   * 이 값이 없으면 駒落ち 판의 형세 그래프가 +2000 대에서 시작하는 이유가 화면에 없다. 되짚는
   * 사람은 그것을 자기 실력으로 오해한다.
   */
  handicapJa?: string;
  /**
   * 대인전 판인지다. 없으면 AI 연습 대국이다(서버는 false 를 보내지 않는다).
   *
   * 대인전은 대국 중 엔진 판정이 없다. 개입 0건과 빈 평가치를 「블런더 없음」으로 표시하면 안
   * 된다(journal §83).
   */
  isMatch?: boolean;
  /**
   * 외부 대국을 가져온 판인지다. 없으면 여기서 둔 판이다(서버는 false 를 보내지 않는다).
   *
   * 가져온 판은 평가치와 개입 기록이 있지만 실제로 수를 되돌리지는 않았다. 기보에 남은 수에는
   * 「介入」 대신 「悪手」 표식을 붙인다(journal §126).
   */
  imported?: boolean;
  /**
   * 평가치를 채우는 중인지다. 대인전에만 온다. 대인전은 대국 중 엔진을 돌리지 않으므로 끝난
   * 뒤 서버가 평가치를 채운다.
   *
   * 기보는 기다리지 않는다. true 인 동안에도 판과 棋譜를 표시한다.
   *
   * 이 상태는 서버 메모리에 있어 배포하면 사라진다. 사라지면 화면은 「남지 않았다」 상태로
   * 돌아가고, 실제로 평가치도 붙지 않는다.
   */
  analyzing?: boolean;
}

/**
 * 기보의 한 수다. `sfen` 을 담으므로 화면은 수를 두지 않는다. 대국의 반박
 * 수순(`RefutationMove`)과 같은 방식이다.
 */
export interface ReviewMove {
  ply: number;
  usi: string;
  /** 棋譜 표기다(▲7六歩). 서버가 만든 값을 그대로 표시한다. */
  ja: string;
  by: Player;
  /**
   * 이 수를 둔 뒤의 국면이다. 기록이 중간에 끊기면 그 지점부터 재현을 멈추고 오지 않는다.
   *
   * 이미 둔 수이므로 목록에서 빼지 않는다. 그래서 화면은 판을 그릴 수 없는 수를 만날 수 있다.
   */
  sfen?: string;
  /**
   * 플레이어 관점 cp 다. 없으면 그 手数에 평가치가 붙지 않았거나 詰み이고, 0(호각)과 다르다.
   *
   * 詰み이면 이 칸이 비고 `mateIn` 이 채워진다. 두 칸이 함께 오지 않는 것은 서버
   * 규약이다(`store.Candidate`). 화면은 항상 `mateIn` 을 먼저 본다.
   */
  evalCp?: number;
  /** 詰み까지의 手数다(플레이어 관점). 양수면 내가 詰ます 쪽이다. */
  mateIn?: number;
  /** 王手를 받는 玉의 칸이다(`5a`). 화면은 규칙을 모르므로 서버가 정해 보낸다. */
  checked?: string;
  /**
   * 사람의 이 수가 好手인지다. 서버가 판정한다(`intervene.IsGood`). 오지 않으면 好手가
   * 아니다.
   *
   * 대인전과 `024_move_good.sql` 이전 판은 판정이 없어 항상 오지 않는다.
   */
  good?: boolean;
}

/**
 * 물러진 수 하나다. 기보에 없는 수를 담는다.
 *
 * `ply` 는 물러진 수의 手数이고 그 수는 확정되지 않았다. 물러진 수는 `ply - 1` 手目 국면에서
 * 뒀으므로, 그 국면을 보려면 그 판을 그린다.
 */
export interface ReviewIntervention {
  ply: number;
  kind: string;
  /** 기계용 코드이고 화면에 표시하지 않는다. 표시에는 `categoryJa` 를 쓴다. */
  category: string;
  /** 카테고리의 짧은 이름이다(タダ捨て). 서버가 만든다. */
  categoryJa?: string;
  /** 수가 나빴던 이유다. 서버가 만든 일본어 문구를 그대로 표시한다. */
  message?: string;
  /** 승률 낙폭이다. 범위는 0~1이다. */
  deltaWin: number;
  levelBucket?: string;
  retractedUsi?: string;
  /**
   * 그 수를 둔 뒤의 평가치다(플레이어 관점 cp). `moves[].evalCp` 와 같은 척도다.
   *
   * `005_intervention_cp.sql` 이전에 기록한 판에는 영영 없다. 남은 낙폭은 승률 차라 미지수
   * 둘에 식 하나여서 복원할 수 없다. 화면이 다시 측정해 채운다(`useMoveEvals`).
   */
  afterCp?: number;
  /** 그 수 뒤 詰み까지의 手数다(플레이어 관점). `afterCp` 와 배타다. */
  afterMate?: number;
  /**
   * 판정 당시 최선수의 cp 다(플레이어 관점). 낙폭을 다시 계산하려면 이 값과 `afterCp` 가
   * 필요하다.
   */
  bestCp?: number;
  /** 물러진 수의 棋譜 표기다. 그 국면까지 재현하지 못하면 없다. */
  retractedJa?: string;
}

/**
 * 사람이 스스로 무른 수 하나다(待った). `ReviewIntervention` 과 따로 둔다.
 *
 * `ReviewIntervention` 은 AI 가 막은 수이고 `ReviewUndo` 는 사람이 되돌리고 싶었던 수라
 * 되짚기에서 전하는 이야기가 정반대다. 무르기에는 판정이 없어 카테고리와 문구도 없다.
 */
export interface ReviewUndo {
  /** 무른 수의 手数다. 그 수는 기보에 없으므로 국면은 `ply-1` 手目 판이다. */
  ply: number;
  usi: string;
  /** 무른 수의 棋譜 표기다. 그 국면까지 재현하지 못하면 없다. */
  ja?: string;
  /** 그 수 뒤 평가치다(플레이어 관점 cp). 무를 때 판정이 끝나지 않았으면 없다. */
  evalCp?: number;
  /** 詰み까지의 手数다(플레이어 관점). `ReviewMove` 와 같은 규약이다. */
  mateIn?: number;
}

export interface GameDetail extends GameSummary {
  /** 0手目 국면이다. 手数를 끝까지 되감으면 이 국면이 된다. */
  startSfen: string;
  /**
   * 이 판의 「형세 0」이다(플레이어 관점 cp). 平手면 오지 않는다.
   *
   * `evalCp` 와 관점이 같아 그대로 뺀다. 형세 그래프(`EvalGraph`)와 후보 줄 색(`evalTone`)
   * 에서 뺀다.
   *
   * 빼지 않으면 駒落ち 판의 곡선이 천장에 붙고, 「호각」 선이 핸디캡을 다 잃은 지점에
   * 그려지고, 후보 줄이 전부 최대 파랑으로 칠해진다.
   */
  baselineCp?: number;
  moves: ReviewMove[];
  interventions: ReviewIntervention[];
  /**
   * 사람이 스스로 무른 수 목록이다. 옛 판은 빈 배열이고, `008_game_undos.sql` 이전 판은 기록
   * 자체가 없다.
   */
  undos: ReviewUndo[];
}

export interface GameListResponse {
  games: GameSummary[];
}

/** 서버 실패 응답 형태다. `message` 는 화면에 그대로 표시한다. */
export interface ApiError {
  error: string;
  message: string;
}
