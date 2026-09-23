// Go 의 nil 슬라이스는 `[]` 가 아니라 `null` 로 온다. 대국 시작 직후의 `moves` 와 엔진 차례의
// `legalMoves` 가 실제로 `null` 이다. 타입에서 `| null` 을 빼면 첫 렌더에서 터진다(journal §37).

import type { WhatIfNode } from '@/protocol/whatif';

/**
 * `aborted` 는 엔진이 수를 내지 못해 대국을 중단한 상태다. `resigned` 와 합치면 엔진이 응답하지
 * 않은 판에 「相手が投了しました」가 떠서, 지고 있던 판이 이긴 판으로 보인다.
 */
export type Status = 'playing' | 'checkmate' | 'stalemate' | 'resigned' | 'repetition' | 'aborted';
export type Player = 'human' | 'engine';

export interface KifuMove {
  usi: string;
  ja: string;
  by: Player;
}

/** 최선수는 담지 않는다(01-core.md §1). */
export interface Intervention {
  kind: 'blunder';
  /** 화면은 이 값으로 문장을 만들지 않는다. 문구는 `message` 를 쓴다(01-core.md §3). */
  category: string;
  retractedUsi: string;
  retractedJa: string;
  /** 0~1. */
  deltaWin: number;
  lostMate: boolean;
  message: string;
  /** 물러진 수를 둔 직후의 국면. 되돌아온 지금 판(`Snapshot.sfen`)과 다르다. */
  retractedSfen: string;
  retractedChecks?: Attack[];
  /** 상대의 詰み 수순이다. 첫 수는 상대의 수이고, 詰み이 증명됐을 때만 온다. 최선수가 아니다. */
  refutation?: RefutationMove[];
}

export interface RefutationMove extends KifuMove {
  /** 화면은 수를 직접 두지 않고 이 값을 쓴다. 클라이언트에 규칙 엔진을 두지 않기 위해서다. */
  sfen: string;
  /** 両王手면 둘이 온다. */
  checks?: Attack[];
}

/** 칸은 USI 좌표(`4i`)다. */
export interface Attack {
  from: string;
  to: string;
}

/**
 * 단계적으로 열리는 수 안내. 서버가 단계마다 필요한 만큼만 보낸다(`game.buildHint`). 화면에서
 * 가려도 페이로드에 있으면 답을 알려 준 것과 같다.
 */
export interface Hint {
  square?: string;
  drop?: string;
  usi?: string;
}

/** 화면에는 `nameJa` 를 쓴다. `code` 는 검색 키로만 쓴다. */
export interface StyleTag {
  code: string;
  nameJa: string;
  /** 서버의 `tag.Kind` 와 같아야 한다. */
  kind: 'castle' | 'formation' | 'opening' | 'tesuji';
}

/**
 * 서버가 처리하지 못한 일(판정 시한 초과 등)을 알린다. 개입과 합치지 않는다. 합치면 「시한을
 * 넘겨 확인하지 못했다」가 「이 수는 괜찮았다」로 읽힌다.
 */
export interface Notice {
  code: string;
  message: string;
}

export type Color = 'b' | 'w';

export interface Snapshot {
  sfen: string;
  ply: number;
  turn: 'b' | 'w';
  yourColor: Color;
  /**
   * 이 값을 보여 줘도 01-core.md §7 과 충돌하지 않는 이유는 서버의 `Snapshot.OpponentOpening` 에
   * 있다.
   */
  opponentOpening?: string;
  /**
   * 駒落ち 판의 「형세 0」(플레이어 관점 cp). 후보 줄의 색에서만 빼고, 적는 숫자에서는 빼지 않는다.
   */
  baselineCp?: number;
  /**
   * `handicap` 으로 이름을 바꾸지 않는다. 다른 곳의 `handicap` 은 id 라서(`GameSetup` ·
   * `ResumableGame`), 이름이 같으면 서로 바꿔 넣어도 타입 검사를 통과한다.
   */
  handicapJa?: string;
  yourTurn: boolean;
  inCheck: boolean;
  thinking: boolean;
  legalMoves: string[] | null;
  moves: KifuMove[] | null;
  status: Status;
  winner?: Player;
  /** 방금 둔 사람의 수를 판정하는 중이다. 엔진이 생각하는 중(`thinking`)과 다르다. */
  judging: boolean;
  intervention?: Intervention;
  hint?: Hint;
  notice?: Notice;
  /** 플레이어 쪽만 온다. */
  styleTags?: StyleTag[];
  /** 다음 수로 새로 붙을 수 있는 이름. 어떤 수인지는 보내지 않는다(01-core.md §7.1). */
  tagHints?: StyleTag[];
  /**
   * 상대 玉 쪽 詰み 게이지의 세기(1~5)다. 手数는 보내지 않는다. 보내면 화면에서 가려도 알려 준 것과
   * 같다.
   */
  mateHeat?: number;
  /**
   * 1~5, 5가 최선수 쪽이다. 오지 않으면 강도 조절이 꺼진 판이니 기본값으로 채우지 않는다.
   * 「あなたの実力」과 다른 값이다.
   */
  opponentStrength?: number;
  /** 남은 待った 횟수다. 상대 차례에도 줄지 않는다. 지금 누를 수 있는지는 `canUndo` 로 본다. */
  undoLeft: number;
  canUndo: boolean;

  hintLeft: number;
  /** `hintLeft` 가 남아도 같은 국면에서 세 번째로 요청하면 false 다. */
  canHint: boolean;
}

/** 이름은 `step` 으로 만들지 않고 `nameJa` 를 쓴다(`skill.Rank`). */
export interface SkillRank {
  /** 클수록 강하다. */
  step: number;
  max: number;
  nameJa: string;
}

/**
 * 대국 결과보다 늦게, 스냅샷과 따로 온다. 기록 저장이 끝나기를 기다리기
 * 때문이다(`dbRecorder.done`).
 */
export interface GameSummary {
  body: string;
  /** 되짚기에서 부르는 총평에는 없다. 판마다 당시 段級을 저장하지 않는다. */
  skill?: { before?: SkillRank; after: SkillRank };
  /**
   * 대국 화면은 이 값을 받기 전까지 자기 판의 번호를 모른다. 기록이 WS 밖에서 비동기로 저장된다.
   */
  gameId?: number;
  stats: {
    /** 물러진 수는 세지 않는다. */
    playerMoves: number;
    interventions: number;
    categories?: { code: string; nameJa: string; count: number }[];
    /** 낙폭이 가장 큰 개입. `ply` 는 물러진 수의 手数라, 그 국면은 `ply - 1` 手目 판이다. */
    focus?: { ply: number; category: string; nameJa: string };
    /** 사람 쪽 精度(0~100). 평가치가 없거나 분석 중이면 없다. 확정한 수순만 센다. */
    accuracy?: number;
  };
}

export type ServerMessage =
  | { type: 'snapshot'; snapshot: Snapshot }
  | { type: 'summary'; summary: GameSummary }
  | { type: 'error'; reason: string; message: string }
  // 스냅샷과 합치지 않는다. 합치면 화면이 대국 판과 가정 판을 구분하지 못한다.
  | { type: 'whatif'; whatif: WhatIfNode }
  | { type: 'whatif_error'; reason: string; message: string };

export type ClientMessage =
  | { type: 'move'; usi: string }
  | { type: 'resign' }
  // 手数를 보내지 않는다. 화면이 정해 보내면, 그사이 도착한 스냅샷과 어긋난 수를 되돌린다.
  | { type: 'undo' }
  // 국면을 보내지 않는다(`undo` 와 같은 이유).
  | { type: 'hint' }
  // 판(SFEN)을 보내지 않는다(`WhatIfRequest` 와 같은 이유).
  | { type: 'whatif'; ply: number; moves: string[] };
