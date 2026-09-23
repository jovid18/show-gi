// `/ws/game` 의 계약. 서버의 `internal/game/session.go` · `internal/server/ws.go` 와 짝이다.
//
// Go 는 nil 슬라이스를 `[]` 가 아니라 `null` 로 직렬화한다. 대국 직후의 `moves` 와 엔진
// 차례의 `legalMoves` 가 실제로 `null` 로 온다. 타입에서 이를 감추면 첫 렌더에서
// 터진다(journal §37).

import type { WhatIfNode } from '@/protocol/whatif';

/**
 * 엔진에게서 수를 받지 못해 대국을 중단한 상태. 승패가 없으므로 `winner` 는 오지 않는다.
 *
 * `resigned` 와 구분한다. 엔진이 응답하지 않은 판에 「相手が投了しました」를 띄우면 지고 있던
 * 판이 이긴 판으로 보인다.
 */
export type Status = 'playing' | 'checkmate' | 'stalemate' | 'resigned' | 'repetition' | 'aborted';
export type Player = 'human' | 'engine';

export interface KifuMove {
  usi: string;
  /** 棋譜 표기(▲7六歩). 서버가 일본어로 보내므로 그대로 표시한다. */
  ja: string;
  by: Player;
}

/**
 * 제지형 개입 하나. 직전 수를 물렸을 때만 오고, 다음 수를 두면 서버가 지운다.
 *
 * 판단은 서버가 끝냈다. 화면은 표시만 한다. 최선수는 여기에 없다(01-core.md §1).
 */
export interface Intervention {
  kind: 'blunder';
  /**
   * 이 수가 나쁜 이유. 서버가 결정적 룰로 정한다(01-core.md §3).
   *
   * 화면은 이 값으로 문장을 만들지 않는다. 문구는 `message` 에 있다. 문구를 두 곳에서 만들면
   * 서로 어긋났을 때 어느 쪽이 맞는지 알 수 없다.
   *
   * 카테고리별로 연출을 나누는 데 쓴다. 그래서 유니온으로 닫지 않고 문자열로 뒀다.
   */
  category: string;
  /** 물린 수의 USI. */
  retractedUsi: string;
  /** 물린 수의 棋譜 표기(▲3三角成). 서버가 만든 문자열을 그대로 표시한다. */
  retractedJa: string;
  /** 승률 낙폭(0~1). */
  deltaWin: number;
  /** 詰み을 놓쳐서 개입했는가. */
  lostMate: boolean;
  /** 화면에 그대로 표시하는 일본어 문구. */
  message: string;
  /** 물린 수를 둔 직후의 국면. 물린 뒤의 현재 판(`Snapshot.sfen`)과 다르다. */
  retractedSfen: string;
  /** 물린 수가 王手였다면 그 王手를 건 駒들. */
  retractedChecks?: Attack[];
  /**
   * 「상대는 이렇게 詰ませてくる」. 첫 수는 상대의 수다.
   *
   * 詰み이 증명된 수순일 때만 온다(서버의 analyst.go). 최선수와는 다른 값이다. 이 수순의 시작
   * 국면은 수를 물렸으므로 지금 판에는 없다.
   */
  refutation?: RefutationMove[];
}

/**
 * 반박 수순의 한 수.
 *
 * 수마다 `sfen` 이 있어 화면이 직접 수를 둘 필요가 없다. 클라이언트가 수를 두려면 규칙 엔진을
 * 하나 더 구현해야 한다.
 */
export interface RefutationMove extends KifuMove {
  /** 이 수를 둔 뒤의 국면. 持ち駒까지 들어 있으므로 駒台도 이 값으로 그린다. */
  sfen: string;
  /**
   * 이 수 뒤에 玉에 王手를 걸고 있는 駒들. 王手가 아니면 오지 않는다.
   *
   * 어느 駒가 王手를 거는지 알려면 규칙이 필요하고, 화면에는 규칙이 없다. 両王手면 둘이 온다.
   * 그 둘이 「잡아서 풀 수 없다」의 근거다.
   */
  checks?: Attack[];
}

/** 판 위에 그릴 선 하나. 칸은 USI 좌표(`4i`)로 적는다. */
export interface Attack {
  from: string;
  to: string;
}

/**
 * 같은 국면에서 여러 번 물렸을 때 단계적으로 열리는 안내.
 *
 * 단계마다 무엇을 보낼지는 서버가 정한다(`game.buildHint`). 답을 미리 실어 보내면 화면에
 * 그리지 않아도 답을 알려 준 것과 같다.
 *
 * 개입(`intervention`)과 수명이 같지만 가리키는 수가 다르다. 개입은 방금 둔 수를, 힌트는 이제
 * 둘 수를 말한다.
 */
export interface Hint {
  /** 움직일 駒가 있는 칸(`5d`). 駒를 打つ 수면 오지 않는다. */
  square?: string;
  /** 駒台에서 꺼낼 駒(`B`). 판 위의 駒를 움직이는 수면 오지 않는다. */
  drop?: string;
  /** 수 전체. 마지막 단계에서만 온다. */
  usi?: string;
}

/**
 * 국면에 붙은 囲い·전법 이름 하나.
 *
 * 화면은 `nameJa` 를 그대로 표시한다. 화면이 코드를 일본어로 옮기면 이름 목록을 서버와 화면이
 * 따로 갖게 된다. `code` 는 화면에 표시하지 않고 검색 키로만 쓴다.
 */
export interface StyleTag {
  code: string;
  nameJa: string;
  /**
   * 분류 축. 서버의 `tag.Kind` 와 값이 같아야 한다. 이쪽 정의가 뒤처지면 모르는 값을 받는다.
   */
  kind: 'castle' | 'formation' | 'opening' | 'tesuji';
}

/**
 * 대국은 계속되지만 서버가 처리하지 못한 일 하나. 다음 수를 두면 서버가 지운다.
 *
 * 개입(`intervention`)과 따로 보낸다. 개입은 판에 대한 판단이고, 이 알림은 서버 쪽 사정이다.
 * 둘을 섞으면 「시한을 넘겨 확인하지 못했다」가 「이 수는 괜찮았다」로 읽힌다.
 */
export interface Notice {
  /** 기계용 코드. 화면은 이 값으로 문장을 만들지 않고 `message` 를 그대로 표시한다. */
  code: string;
  /** 화면에 그대로 표시하는 일본어. */
  message: string;
}

/** 先手·後手 중 어느 쪽을 맡았나. 서버의 `games.my_color` 와 값이 같다. */
export type Color = 'b' | 'w';

export interface Snapshot {
  sfen: string;
  ply: number;
  turn: 'b' | 'w';
  /**
   * 사람이 맡은 쪽. 한 판 동안 바뀌지 않는다.
   *
   * `turn` 으로 대신하면 안 된다. `turn` 으로 판 방향을 정하면 상대가 생각하는 동안 판이
   * 뒤집힌다.
   */
  yourColor: Color;
  /**
   * 상대가 쓰는 전법의 일본어 이름. 「おまかせ」로 시작했으면 오지 않는다.
   *
   * 시작 화면에서 사람이 고른 값을 그대로 돌려준다. 이 값이 01-core.md §7 과 충돌하지 않는
   * 이유는 서버의 `Snapshot.OpponentOpening` 에 적혀 있다.
   */
  opponentOpening?: string;
  /**
   * 이 판의 「형세 0」(플레이어 관점 cp). 平手면 오지 않는다.
   *
   * 개입 카드에서 후보 줄의 색을 정할 때 이 값을 뺀다(`evalTone`). 줄에 적는 숫자는 빼지 않은
   * 원래 값이다.
   */
  baselineCp?: number;
  /**
   * 이 판의 手合割 이름(二枚落ち). 平手면 오지 않는다. 서버가 시작 국면에서
   * 구한다(`handicap.NameOf`).
   *
   * 이름 끝에 `Ja` 를 붙였다. 다른 곳(`GameSetup` · `ResumableGame`)의 `handicap` 필드는 모두
   * id 다. 둘을 같은 이름으로 부르면 서로 바꿔 넣어도 타입 검사를 통과한다.
   */
  handicapJa?: string;
  yourTurn: boolean;
  inCheck: boolean;
  thinking: boolean;
  legalMoves: string[] | null;
  moves: KifuMove[] | null;
  status: Status;
  winner?: Player;
  /** 방금 둔 수를 판정하는 중. 이 동안 `yourTurn` 이 false 가 되어 입력이 잠긴다. */
  judging: boolean;
  intervention?: Intervention;
  hint?: Hint;
  notice?: Notice;
  /**
   * 플레이어가 지금 짜고 있는 囲い·전법. 상대의 것은 서버가 보내지 않는다.
   *
   * 初期配置에서는 비어 있다. 첫 수 전에 이름을 보여 주면 아직 고르지 않은 전법에 이름이
   * 붙는다.
   */
  styleTags?: StyleTag[];
  /**
   * 이 국면에서 둘 수 있는 수 가운데 새 이름이 붙는 수가 있으면 그 이름.
   *
   * 제안형 힌트에 쓴다. 어떤 수인지는 알려 주지 않는다(01-core.md §7.1).
   */
  tagHints?: StyleTag[];
  /**
   * 詰み 게이지의 세기(1~5). 0이거나 오지 않으면 게이지를 끈다.
   *
   * 게이지는 상대 玉 쪽 하나뿐이다. 켜져 있으면 언제나 「내가 詰み에 가까워졌다」로 읽는다.
   * 양쪽 게이지를 같은 테두리에 그리면 이기는 중인지 지는 중인지 거꾸로 읽힌다.
   *
   * 세기만 보낸다. 手数를 실어 보내면 화면에 그리지 않아도 手数를 알려 준 것과 같다.
   */
  mateHeat?: number;
  /**
   * 상대가 지금 맞추고 있는 강도(1~5). 5에 가까울수록 최선수를 둔다.
   *
   * 오지 않으면 강도 조절이 꺼진 판이다. 이때 0이나 3으로 채우면 조절하지 않는 판에 눈금이
   * 그려진다.
   *
   * 「あなたの実力」과는 다른 값이다. 이 값이 아는 것은 이 판에서 얼마나 헤맸는가뿐이다.
   */
  opponentStrength?: number;
  /**
   * 사람이 무를 수 있는 남은 횟수(待った). 0이면 이 판에서 다 썼다.
   *
   * `canUndo` 와 다르다. 이 값은 남은 횟수라서 상대 차례에도 그대로다. `canUndo` 는 「지금 이
   * 순간 누를 수 있나」를 나타낸다.
   */
  undoLeft: number;
  /**
   * 지금 무르기를 누를 수 있는가. 사람 차례인지, 횟수가 남았는지, 되돌릴 사람의 수가 있는지를
   * 서버가 이미 확인했다.
   */
  canUndo: boolean;

  /** 사람이 쓸 수 있는 최선수 힌트의 남은 횟수. `undoLeft` 와 같은 규약이다. */
  hintLeft: number;
  /**
   * 지금 힌트를 누를 수 있는가. `canUndo` 와 달리 국면마다 달라진다. 횟수가 남아도 같은
   * 국면에서 세 번째로 요청하면 false 다.
   */
  canHint: boolean;
}

/**
 * 段級 하나. 이름은 서버가 보낸다. 화면이 `step` 으로 이름을 만들면 단계를 늘릴 때 한쪽만
 * 고쳐진다(`skill.Rank`).
 */
export interface SkillRank {
  /** 0..max. 클수록 강하다. */
  step: number;
  max: number;
  /** 「8級」·「初段」. */
  nameJa: string;
}

/**
 * 대국이 끝난 뒤 한 번 오는 총평.
 *
 * 스냅샷과 따로 온다. 기록 저장이 끝나기를 기다리므로 결과 문구보다 늦게 도착한다(서버의
 * `dbRecorder.done`).
 *
 * 숫자와 문장을 나눠 보낸다. `body` 에는 手数도 개입 횟수도 없다. 그 숫자는 `stats` 에
 * 있다(서버의 `explain.GameFacts`).
 */
export interface GameSummary {
  /** 화면에 그대로 표시하는 일본어. 비어서 오지 않는다. 쓸 사실이 적으면 짧아질 뿐이다. */
  body: string;
  /**
   * 이 판에서 段級이 어떻게 바뀌었나. 표본이 모자라 판정하지 못하면 없다. 첫 판이거나
   * 익명이면 `before` 가 없다.
   *
   * 되짚기로 지난 판을 열면 언제나 없다. 추정치는 판이 아니라 사람에게 붙는 값이라서 판마다
   * 당시 값을 남기지 않았다.
   */
  skill?: { before?: SkillRank; after: SkillRank };
  /**
   * 이 판의 기록 번호. 되짚기로 가는 링크를 이 값으로 만든다. 기록은 WS 밖에서 비동기로
   * 저장되므로, 대국 화면은 이 값을 받기 전까지 자기 판의 번호를 모른다.
   *
   * 되짚기에서 부르는 총평에는 없다. 그 판을 이미 열고 있기 때문이다.
   */
  gameId?: number;
  stats: {
    /** 사람이 확정한 수. 물린 수는 기보에 없으므로 여기에도 없다. */
    playerMoves: number;
    /** 물린 횟수. 같은 국면에서 여러 번 물렸으면 그만큼 센다. */
    interventions: number;
    /** 횟수가 많은 순서. 서버가 정한 순서를 그대로 쓴다. */
    categories?: { code: string; nameJa: string; count: number }[];
    /**
     * 「이 국면을 다시 봐라」. 그 판에서 낙폭이 가장 컸던 개입이다. 개입이 없었으면 오지
     * 않는다.
     *
     * `ply` 는 물린 수의 手数다. 물린 수는 기보에 없으므로 되짚기는 그 한 수 앞(`ply - 1`)을
     * 연다. 그 국면이 「다시 생각할 국면」이다.
     */
    focus?: { ply: number; category: string; nameJa: string };
  };
}

export type ServerMessage =
  | { type: 'snapshot'; snapshot: Snapshot }
  | { type: 'summary'; summary: GameSummary }
  | { type: 'error'; reason: string; message: string }
  // 가정 수순의 한 국면. 스냅샷과 따로 온다. 하나로 합치면 화면이 대국 판과 가정 판을
  // 구분하지 못한다.
  | { type: 'whatif'; whatif: WhatIfNode }
  | { type: 'whatif_error'; reason: string; message: string };

export type ClientMessage =
  | { type: 'move'; usi: string }
  | { type: 'resign' }
  // 手数를 보내지 않는다. 무엇을 되돌릴지는 서버가 자기 기보를 보고 정한다. 화면이 手数를
  // 정해 보내면, 그사이 새 스냅샷이 도착했을 때 엉뚱한 수를 되돌린다.
  | { type: 'undo' }
  // 국면을 보내지 않는다. 어느 국면의 힌트인지는 서버가 자기 판을 보고 정한다(`undo` 와 같은
  // 규약).
  | { type: 'hint' }
  // 판(SFEN)을 보내지 않는다. 분기의 시작 국면은 서버가 방금 보낸 스냅샷으로 만든다.
  | { type: 'whatif'; ply: number; moves: string[] };
