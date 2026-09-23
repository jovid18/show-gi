// `/ws/game` 의 메시지 계약이다. 서버 쪽 짝은 `internal/game/session.go` 와
// `internal/server/ws.go` 다.
//
// Go 의 nil 슬라이스는 빈 배열 `[]` 이 아니라 JSON `null` 로 온다. 대국 시작 직후의 `moves`
// 와 엔진 차례의 `legalMoves` 가 실제로 `null` 이다. 타입에서 `null` 을 숨기면 첫 렌더에서
// 크래시한다(journal §37).

import type { WhatIfNode } from '@/protocol/whatif';

/**
 * `aborted` 는 엔진의 수를 받지 못해 대국을 중단한 상태다. 승패가 없으므로 `winner` 를 보내지
 * 않는다.
 *
 * `resigned` 와 합치면 엔진이 응답하지 않은 판을 「相手が投了しました」로 표시해 지던 판이
 * 승리로 보인다.
 */
export type Status = 'playing' | 'checkmate' | 'stalemate' | 'resigned' | 'repetition' | 'aborted';
export type Player = 'human' | 'engine';

export interface KifuMove {
  usi: string;
  /** 棋譜 표기다(▲7六歩). 서버가 일본어로 만들어 보내고 화면은 그대로 표시한다. */
  ja: string;
  by: Player;
}

/**
 * 제지형 개입 하나다. 직전 수가 물러졌을 때만 오고, 다음 착수 때 서버가 지운다.
 *
 * 판단은 서버가 끝낸 상태로 오고 화면은 표시만 한다. 최선수는 담지 않는다(01-core.md §1).
 */
export interface Intervention {
  kind: 'blunder';
  /**
   * 수가 나쁜 이유를 나타내는 카테고리다. 서버가 결정적 룰로 정한다(01-core.md §3).
   *
   * 화면은 이 값으로 문장을 만들지 않고 문구는 `message` 를 쓴다. 문구를 두 곳에서 따로
   * 관리하면 어긋났을 때 어느 쪽이 맞는지 판별할 수 없다.
   *
   * 카테고리별 연출을 나누는 데만 쓰므로 유니온으로 제한하지 않고 string 으로 둔다.
   */
  category: string;
  retractedUsi: string;
  /** 물러진 수의 棋譜 표기다(▲3三角成). 서버가 만든 값을 그대로 표시한다. */
  retractedJa: string;
  /** 승률 낙폭이다. 범위는 0~1이다. */
  deltaWin: number;
  /** 詰み을 놓친 것이 개입 원인인지 나타낸다. */
  lostMate: boolean;
  /** 화면에 그대로 표시하는 일본어 문구다. */
  message: string;
  /** 물러진 수를 둔 직후의 국면이다. 되돌아온 현재 판(`Snapshot.sfen`)과 다르다. */
  retractedSfen: string;
  /** 물러진 수가 王手였을 때 王手를 건 駒들이다. */
  retractedChecks?: Attack[];
  /**
   * 「상대가 이렇게 詰ませてくる」 수순이다. 첫 수는 상대 수다.
   *
   * 증명된 詰み 수순일 때만 온다(서버의 analyst.go). 최선수와는 별개다.
   *
   * 수순의 시작 국면은 개입이 수를 되돌려서 판에 남아 있지 않다.
   */
  refutation?: RefutationMove[];
}

/**
 * 반박 수순의 한 수다. `sfen` 을 담아 보내므로 화면은 수를 직접 두지 않는다.
 *
 * 클라이언트가 수를 두면 규칙 엔진을 서버와 클라이언트 두 곳에서 따로 관리한다.
 */
export interface RefutationMove extends KifuMove {
  /** 이 수를 둔 뒤의 국면이다. 持ち駒도 담으므로 駒台도 이 값으로 표시한다. */
  sfen: string;
  /**
   * 이 수 뒤에 玉에 王手를 걸고 있는 駒들이다. 王手가 아니면 오지 않는다.
   *
   * 어느 駒가 王手를 거는지 가리려면 규칙이 필요하고 화면은 규칙을 모르므로 서버가 보낸다.
   * 両王手면 2개가 오고, 이것이 「먹어서 풀 수 없다」의 근거다.
   */
  checks?: Attack[];
}

/** 판 위에 긋는 선 하나다. 칸 좌표는 USI 다(`4i`). */
export interface Attack {
  from: string;
  to: string;
}

/**
 * 같은 국면에서 거듭 물러지면 단계적으로 열리는 안내다. 단계는 서버가
 * 나눈다(`game.buildHint`).
 *
 * 답이 페이로드에 있으면 표시하지 않아도 답을 알려 준 것과 같다.
 *
 * 개입(`intervention`)과 수명이 같고 방향이 반대다. 개입은 방금 둔 수를 다루고 힌트는 지금 둘
 * 수를 다룬다.
 */
export interface Hint {
  /** 움직일 駒가 지금 있는 칸이다(`5d`). 打つ 수면 오지 않는다. */
  square?: string;
  /** 駒台에서 집을 駒다(`B`). 판 위 이동이면 오지 않는다. */
  drop?: string;
  /** 수 전체다. 마지막 단계에서만 온다. */
  usi?: string;
}

/**
 * 국면에 붙는 이름 하나다(囲い·전법). `nameJa` 를 그대로 표시한다.
 *
 * 화면이 코드를 일본어로 바꾸면 이름을 두 곳에서 따로 관리한다. `code` 는 화면에 표시하지
 * 않고 검색 키로만 쓴다.
 */
export interface StyleTag {
  code: string;
  nameJa: string;
  /**
   * 서버 `tag.Kind` 와 값이 같아야 한다. 이 유니온이 뒤처지면 화면이 모르는 종류를 받는다.
   */
  kind: 'castle' | 'formation' | 'opening' | 'tesuji';
}

/**
 * 대국은 계속되지만 서버가 처리하지 못한 일 하나다. 다음 착수 때 서버가 지운다.
 *
 * 개입(`intervention`)과 따로 보낸다. 개입은 판에 대한 판단이고 이 값은 서버 사정이다. 섞으면
 * 「시한을 넘겨 확인하지 못했다」를 「이 수는 괜찮았다」로 잘못 읽는다.
 */
export interface Notice {
  /** 기계용 코드다. 화면은 이 값으로 문장을 만들지 않고 `message` 를 그대로 표시한다. */
  code: string;
  message: string;
}

/** 잡은 쪽이다. 서버 `games.my_color` 와 같은 값이다. */
export type Color = 'b' | 'w';

export interface Snapshot {
  sfen: string;
  ply: number;
  turn: 'b' | 'w';
  /**
   * 사람이 잡은 쪽이다. 한 판 동안 바뀌지 않는다.
   *
   * `turn` 으로 유추하면 안 된다. `turn` 으로 판 방향을 정하면 상대가 생각하는 동안 판이
   * 뒤집힌다.
   */
  yourColor: Color;
  /**
   * 상대가 쓰는 진형의 일본어 이름이다. 시작 화면에서 사람이 고른 값을 반영하고,
   * 「おまかせ」로 시작했으면 오지 않는다.
   *
   * 01-core.md §7과 맞는지의 근거는 서버 `Snapshot.OpponentOpening` 에 있다.
   */
  opponentOpening?: string;
  /**
   * 이 판의 「형세 0」이다(플레이어 관점 cp). 平手면 오지 않는다.
   *
   * 개입 카드의 후보 줄 색은 이 값을 빼고 계산한다(`evalTone`). 줄에 표시하는 숫자는 원본
   * 그대로다.
   */
  baselineCp?: number;
  /**
   * 이 판의 手合割 이름이다(二枚落ち). 平手면 오지 않는다. 서버가 시작 국면에서
   * 구한다(`handicap.NameOf`).
   *
   * `handicap` 필드는 어디서나 id 다(`GameSetup` · `ResumableGame`). 이름이 같으면 두 값을
   * 바꿔 넣어도 타입 검사를 통과하므로 `Ja` 를 붙인다.
   */
  handicapJa?: string;
  yourTurn: boolean;
  inCheck: boolean;
  thinking: boolean;
  legalMoves: string[] | null;
  moves: KifuMove[] | null;
  status: Status;
  winner?: Player;
  /** 방금 둔 수를 판정하는 중이다. 이 동안 `yourTurn` 은 false 라 입력이 잠긴다. */
  judging: boolean;
  intervention?: Intervention;
  hint?: Hint;
  notice?: Notice;
  /**
   * 플레이어가 지금 짜고 있는 囲い·전법이다. 상대 쪽은 서버가 보내지 않는다.
   *
   * 初期配置에서는 비어 있다. 첫 수 전에 이름을 표시하면 아직 하지 않은 선택에 이름을 붙인다.
   */
  styleTags?: StyleTag[];
  /**
   * 이 국면에서 둘 수 있는 수가 새 이름을 만들 때 그 이름이다. 제안형 힌트 데이터이고, 수
   * 자체는 가리키지 않는다(01-core.md §7.1).
   */
  tagHints?: StyleTag[];
  /**
   * 詰み 게이지의 세기다. 범위는 1~5이고, 0이거나 오지 않으면 꺼진다.
   *
   * 상대 玉 쪽만 다루므로 켜져 있으면 항상 「내가 詰み에 가깝다」다. 같은 테두리에 양쪽을
   * 표시하면 우세와 열세를 반대로 읽는다.
   *
   * 세기만 보내고 手数는 보내지 않는다. 手数를 담으면 표시하지 않아도 알려 준 것과 같다.
   */
  mateHeat?: number;
  /**
   * 상대가 지금 목표로 하는 강함이다. 범위는 1~5이고 5가 최선수 쪽이다.
   *
   * 오지 않으면 조절이 꺼진 것이다. 0이나 3으로 채우면 안 된다. 채우면 조절하지 않는 판에도
   * 눈금을 표시한다.
   *
   * 「あなたの実力」과 별개다. 이 값이 아는 것은 이 판에서 사람이 헤맨 정도뿐이다.
   */
  opponentStrength?: number;
  /**
   * 사람의 남은 무르기 횟수다(待った). 0이면 그 판에서 다 썼다.
   *
   * 이 값은 예산이라 상대 차례에도 유지된다. `canUndo` 는 「지금 이 순간 누를 수 있나」를
   * 나타낸다.
   */
  undoLeft: number;
  /**
   * 지금 무르기 버튼을 누를 수 있는지다. 사람 차례인지, 예산이 남았는지, 되돌릴 사람 수가
   * 있는지를 서버가 이미 확인했다.
   */
  canUndo: boolean;

  /** 사람의 남은 최선수 힌트 횟수다. `undoLeft` 와 같은 규약이다. */
  hintLeft: number;
  /**
   * 지금 힌트 버튼을 누를 수 있는지다. `canUndo` 와 달리 국면마다 달라진다. 예산이 남아도
   * 같은 자리에서 세 번째로 요청하면 false 다.
   */
  canHint: boolean;
}

/**
 * 段級 하나다. 이름은 서버가 보낸다.
 *
 * 화면이 `step` 으로 이름을 만들면 척도를 늘릴 때 한쪽만 늘어난다(`skill.Rank`).
 */
export interface SkillRank {
  /** 범위는 0..max 이고 클수록 강하다. */
  step: number;
  max: number;
  /** 「8級」·「初段」 같은 이름이다. */
  nameJa: string;
}

/**
 * 대국이 끝난 뒤 한 번 오는 총평이다. 스냅샷과 따로 온다.
 *
 * 기록이 끝나기를 기다리므로 결과 문구보다 늦게 도착한다(서버의 `dbRecorder.done`).
 *
 * `body` 는 手数와 개입 횟수를 말하지 않고, 숫자는 `stats` 에 담는다(서버의
 * `explain.GameFacts`).
 */
export interface GameSummary {
  /** 화면에 그대로 표시하는 일본어다. 비어 있는 일은 없고, 말할 사실이 없으면 짧아진다. */
  body: string;
  /**
   * 이 판에서의 段級 변동이다. 판정할 표본이 모자라면 오지 않는다. `before` 가 없으면 첫
   * 판이거나 익명이다.
   *
   * 되짚기로 지난 판을 열면 항상 오지 않는다. 추정치는 사람 단위 값이라 그 판을 판정한 당시의
   * 값을 저장해 두지 않는다.
   */
  skill?: { before?: SkillRank; after: SkillRank };
  /**
   * 이 판의 기록 번호다. 되짚기로 가는 링크를 만드는 데 쓴다.
   *
   * 기록은 WS 밖에서 비동기로 쓰므로 대국 화면은 이 시점까지 자기 판 번호를 모른다.
   * 되짚기에서 부르는 총평에는 오지 않는다. 그 화면은 이미 그 판을 열고 있다.
   */
  gameId?: number;
  stats: {
    /** 사람이 확정한 수의 개수다. 물러진 수는 기보에 없으므로 세지 않는다. */
    playerMoves: number;
    /** 물러진 횟수다. 같은 국면에서 거듭 물러지면 그만큼 센다. */
    interventions: number;
    /** 서버가 많은 순으로 정렬해 보낸다. 화면은 그 순서대로 표시한다. */
    categories?: { code: string; nameJa: string; count: number }[];
    /**
     * 「이 국면을 다시 봐라」를 가리킨다. 그 판에서 낙폭이 가장 큰 개입이고, 개입이 없으면
     * 오지 않는다.
     *
     * `ply` 는 물러진 수의 手数다. 되짚기는 한 수 앞(`ply - 1`)을 연다. 물러진 수는 기보에
     * 없으므로 그 자리가 「다시 생각할 국면」이다.
     */
    focus?: { ply: number; category: string; nameJa: string };
  };
}

export type ServerMessage =
  | { type: 'snapshot'; snapshot: Snapshot }
  | { type: 'summary'; summary: GameSummary }
  | { type: 'error'; reason: string; message: string }
  // 가정 수순의 노드 하나다. 스냅샷과 따로 보낸다. 합치면 화면이 두 판을 같은 것으로
  // 표시한다.
  | { type: 'whatif'; whatif: WhatIfNode }
  | { type: 'whatif_error'; reason: string; message: string };

export type ClientMessage =
  | { type: 'move'; usi: string }
  | { type: 'resign' }
  // 手数를 보내지 않는다. 되돌릴 수는 서버가 자기 기보로 정한다.
  //
  // 화면이 지정하면 그 사이 도착한 스냅샷과 어긋난 자리를 되돌린다.
  | { type: 'undo' }
  // 국면을 보내지 않는다. 대상 국면은 서버가 자기 판으로 정한다. `undo` 와 같은 규약이다.
  | { type: 'hint' }
  // 판(SFEN)을 보내지 않는다. 시작 국면은 서버가 직전에 보낸 스냅샷에서 만든다.
  | { type: 'whatif'; ply: number; moves: string[] };
