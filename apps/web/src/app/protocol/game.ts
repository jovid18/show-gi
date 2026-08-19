// `/ws/game` 의 계약. 서버의 `internal/game/session.go` · `internal/server/ws.go` 와 짝이다.
//
// Go 의 nil 슬라이스는 `[]` 가 아니라 `null` 로 직렬화된다. 대국 시작 직후의 `moves`,
// 엔진 차례의 `legalMoves` 가 실제로 그렇게 온다 — 타입에서 숨기면 첫 렌더에서 터진다.

import type { WhatIfNode } from '@/protocol/whatif';

/**
 * `aborted` 는 상대의 수를 못 얻어 판을 접은 것이다. 승패가 없어 `winner` 가 안 온다.
 *
 * `resigned` 와 갈라 둔다 — 엔진이 답하지 않은 것을 「相手が投了しました」로 그리면
 * 지고 있던 판이 화면에서 이긴 판이 된다. 기록에는 `abandoned` 로 남아 이어하기
 * 목록에 그대로 올라온다.
 */
export type Status = 'playing' | 'checkmate' | 'stalemate' | 'resigned' | 'repetition' | 'aborted';
export type Player = 'human' | 'engine';

export interface KifuMove {
  usi: string;
  /** 棋譜 표기(▲7六歩). 서버가 일본어로 주므로 그대로 그린다. */
  ja: string;
  by: Player;
}

/**
 * 제지형 개입 하나. 직전 수가 물러졌을 때만 실려 오고, 다음 착수에서 서버가 지운다.
 *
 * 판단은 서버에서 이미 끝나 있고 화면은 그리기만 한다. 최선수는 여기 없다 — 어느 수를
 * 뒀어야 했는지는 알려주지 않는다(docs/01-core.md §1).
 */
export interface Intervention {
  kind: 'blunder';
  /**
   * 왜 나쁜가. 서버가 결정적 룰로 정한다(docs/01-core.md §3).
   *
   * 화면은 이걸로 문장을 짓지 않는다 — 문구는 `message` 에 이미 있고, 두 벌이 되면
   * 어긋났을 때 어느 쪽이 맞는지 알 수 없다. 연출을 카테고리별로 가를 때 쓰는 자리라
   * 서버가 종류를 늘려도 안 깨지게 문자열로 열어 뒀다.
   */
  category: string;
  /** 물러진 수의 USI. 판 위에서 어느 칸에서 어느 칸이었는지를 되짚는 데 쓴다. */
  retractedUsi: string;
  /** 그 수의 棋譜 표기(▲3三角成). 서버가 만든 것을 그대로 그린다. */
  retractedJa: string;
  /** 승률 낙폭(0~1). */
  deltaWin: number;
  /** 詰み을 놓쳐서 걸렸는가. */
  lostMate: boolean;
  /** 화면에 그대로 나가는 일본어 문구. */
  message: string;
  /** 물러진 수를 둔 직후의 국면. 되돌아온 지금 판(`Snapshot.sfen`)과는 다르다. */
  retractedSfen: string;
  /** 물러진 수가 王手였다면 그것을 거는 말들. */
  retractedChecks?: Attack[];
  /**
   * 「상대는 이렇게 詰ませてくる」. 첫 수가 상대의 수다.
   *
   * 증명된 詰み 수순일 때만 온다(서버의 analyst.go). 자를 필요가 없는 유일한 수순이라
   * 그렇고, 「그때 어떻게 뒀어야 했나」는 후보 셋을 직접 둬 보는 쪽이 맡는다.
   *
   * 최선수가 아니다. 이 수순이 시작하는 국면은 되물러서 이미 사라졌다.
   */
  refutation?: RefutationMove[];
}

/**
 * 반박 수순의 한 수.
 *
 * `sfen` 이 있어서 화면은 수를 두지 않는다. 한 수씩 넘겨 볼 때 판은 이 값을 그대로
 * 그리면 된다 — 클라이언트가 스스로 두면 규칙 엔진을 한 벌 더 갖게 된다.
 */
export interface RefutationMove extends KifuMove {
  /** 이 수를 둔 뒤의 국면. 持ち駒까지 들어 있어 駒台도 이 값으로 맞는다. */
  sfen: string;
  /**
   * 그 수 뒤에 玉을 잡으러 오는 말들. 王手가 아니면 오지 않는다.
   *
   * 「王手다」까지는 국면만 봐도 알지만 어느 말이 걸고 있는지는 규칙을 알아야 하고,
   * 화면은 규칙을 갖지 않는다. 両王手는 여기서 둘로 오고, 그것이 곧 「먹어서 풀 수
   * 없다」의 이유다.
   */
  checks?: Attack[];
}

/** 판 위에 그을 한 줄. 칸은 USI 좌표(`4i`). */
export interface Attack {
  from: string;
  to: string;
}

/**
 * 같은 국면에서 여러 번 물러졌을 때 열리는 계단식 안내.
 *
 * 단계는 서버가 자른다. 3회에서 `usi` 는 아예 오지 않는다 — 답이 페이로드에 그대로
 * 있으면 화면이 안 그려도 이미 알려준 것이 된다.
 *
 * 개입(`intervention`)과 수명은 같고 방향이 반대다. 저쪽은 방금 둔 수를, 이쪽은 지금
 * 둘 수를 말한다 — 그래서 판 위의 색도 갈린다(상대 쪽 초록·빨강, 이쪽 파랑).
 */
export interface Hint {
  /** 움직일 駒가 서 있는 칸(`5d`). 打이면 오지 않는다. */
  square?: string;
  /** 駒台에서 집을 駒(`B`). 판 위의 수면 오지 않는다. */
  drop?: string;
  /** 그 수 전체. 마지막 단계에서만 온다. */
  usi?: string;
}

/**
 * 국면에 붙은 이름 하나 — 囲い나 전법.
 *
 * `nameJa` 를 그대로 그린다. 화면이 코드를 일본어로 바꾸면 이름이 두 벌이 된다.
 * `code` 는 화면에 안 나가고 검색 키로만 쓴다.
 */
export interface StyleTag {
  code: string;
  nameJa: string;
  /** 축. 서버의 `tag.Kind` 와 같은 값이어야 한다 — 뒤처지면 모르는 종류를 받는다. */
  kind: 'castle' | 'formation' | 'opening' | 'tesuji';
}

/**
 * 대국은 그대로 도는데 서버가 못 해준 것 하나. 다음 착수에서 서버가 지운다.
 *
 * 개입(`intervention`)과 갈라 온다. 저쪽은 판에 대한 판단이고 이쪽은 서버 사정이라,
 * 섞으면 「시한을 넘겨 확인 못 했다」가 「이 수는 괜찮았다」로 읽힌다.
 */
export interface Notice {
  /** 기계용 코드. 화면은 이걸로 문장을 짓지 않고 `message` 를 그대로 그린다. */
  code: string;
  /** 화면에 그대로 나가는 일본어. */
  message: string;
}

/** 어느 쪽을 잡았나. 서버의 `games.my_color` 와 같은 값이다. */
export type Color = 'b' | 'w';

export interface Snapshot {
  sfen: string;
  ply: number;
  turn: 'b' | 'w';
  /**
   * 사람이 잡은 쪽. 한 판에서 변하지 않는다.
   *
   * `turn` 으로 되짚으면 안 된다 — 저건 지금 누구 차례냐라, 그것으로 판 방향을 정하면
   * 상대가 생각하는 동안 판이 뒤집힌다.
   */
  yourColor: Color;
  /**
   * 상대가 따르는 진형의 일본어 이름. 「おまかせ」로 시작했으면 오지 않는다.
   *
   * 시작 화면에서 사람이 고른 값을 되비추는 것이다. 상대의 형태를 알려주지 않는다는
   * 규칙(docs/01-core.md §7)과 어긋나지 않는 이유는 서버의 `Snapshot.OpponentOpening`.
   */
  opponentOpening?: string;
  /**
   * 이 판의 「형세 0」(플레이어 관점 cp). 平手면 오지 않는다.
   *
   * 개입 카드의 후보 줄 색이 이 값을 뺀다(`evalTone`) — 안 빼면 駒落ち에서 모든 줄이
   * 최대 파랑이 된다. 줄에 적히는 숫자는 원본 그대로다.
   */
  baselineCp?: number;
  /**
   * 이 판의 手合割 이름(二枚落ち). 平手면 오지 않는다.
   *
   * 서버가 시작 국면에서 파생한다(`handicap.NameOf`) — 화면이 SFEN 을 보고 이름을 만들면
   * 표가 두 벌이 된다.
   *
   * 이름에는 `Ja` 가 붙는다. `handicap` 이라는 칸은 어디서든 id 이고(`GameSetup` ·
   * `ResumableGame`), 둘을 같은 이름으로 부르면 자리를 바꿔 넣어도 타입이 통과한다.
   */
  handicapJa?: string;
  yourTurn: boolean;
  inCheck: boolean;
  thinking: boolean;
  legalMoves: string[] | null;
  moves: KifuMove[] | null;
  status: Status;
  winner?: Player;
  /** 방금 둔 수를 판정하는 중. 이 동안은 `yourTurn`이 내려가 입력이 잠긴다. */
  judging: boolean;
  intervention?: Intervention;
  hint?: Hint;
  /** 대국은 계속되는데 서버가 못 해준 것. 다음 착수에서 사라진다. */
  notice?: Notice;
  /**
   * 플레이어가 지금 짜고 있는 囲い·전법. 상대 쪽은 서버가 안 보낸다.
   *
   * 初期配置에서는 비어 있다 — 첫 수 전에 이름을 그리면 아직 하지 않은 선택에 이름을
   * 붙이는 것이 된다.
   */
  styleTags?: StyleTag[];
  /**
   * 이 국면에서 둘 수 있는 수 중 새 이름을 만드는 것이 있으면 그 이름.
   *
   * 제안형 힌트의 데이터다. 수를 짚지 않는다 — 「여기에 이름 있는 수가 있다」까지만
   * 보여주고 어디에 두라는 것은 알려주지 않는다(docs/01-core.md §7.1).
   */
  tagHints?: StyleTag[];
  /**
   * 詰み 게이지의 세기(1~5). 0이거나 오지 않으면 꺼져 있다.
   *
   * 상대 玉 쪽 하나뿐이라, 불이 붙으면 언제나 「내가 詰み에 가까워졌다」는 뜻이다. 내 玉이
   * 위험한 것은 제지형 개입이 이미 막고 있고(docs/01-core.md §7), 같은 테두리에 둘을
   * 그리면 이기는 중인지 지는 중인지가 반대로 읽힌다.
   *
   * 手数가 아니라 세기다. 手数가 여기 있으면 화면이 안 그려도 이미 알려준 것이 된다.
   */
  mateHeat?: number;
  /**
   * 상대가 지금 겨냥하는 강함(1~5). 5가 최선수 쪽이다.
   *
   * 안 오면 조절이 꺼져 있다는 뜻이다 — 0이나 3으로 메우지 않는다. 조절하지 않는 판에
   * 눈금을 그리면 없는 기능을 말하게 된다.
   *
   * 「あなたの実力」이 아니다. 아는 것은 이 판에서 얼마나 헤맸는가뿐이다.
   */
  opponentStrength?: number;
  /**
   * 사람이 아직 무를 수 있는 횟수(待った). 0이면 그 판에서 다 썼다.
   *
   * `canUndo` 와 뜻이 다르다 — 이쪽은 예산이라 상대 차례에도 그대로 참이고, 저쪽은
   * 「지금 이 순간 누를 수 있나」다.
   */
  undoLeft: number;
  /**
   * 지금 무르기를 누를 수 있는가. 화면이 조건을 다시 짓지 않는다 — 사람 차례인지,
   * 예산이 남았는지, 되돌릴 사람의 수가 있는지를 서버가 이미 봤다.
   */
  canUndo: boolean;

  /** 사람이 아직 부를 수 있는 최선수 힌트 횟수. `undoLeft` 와 같은 규약이다. */
  hintLeft: number;
  /**
   * 지금 힌트를 누를 수 있는가. `canUndo` 와 달리 국면마다 갈린다 — 예산이 남아도
   * 같은 자리를 세 번째로 물으면 false 다.
   */
  canHint: boolean;
}

/**
 * 段級 하나. 이름을 서버가 준다 — 화면이 `step` 에서 이름을 만들면 어휘가 두 벌이 되고,
 * 척도를 늘리는 날 한쪽만 늘어난다(`skill.Rank`).
 */
export interface SkillRank {
  /** 0..max, 클수록 세다. */
  step: number;
  max: number;
  /** 「8級」·「初段」. */
  nameJa: string;
}

/**
 * 대국이 끝난 뒤 한 번 오는 총평.
 *
 * 스냅샷과 갈라 온다 — 국면의 상태가 아니라 판 전체에 대한 이야기이고, 기록이 다
 * 쓰이기를 기다리므로 결과 문구보다 늦게 도착한다(서버의 `dbRecorder.done`).
 *
 * 숫자와 문장이 갈려 있다. `body` 는 手数도 개입 횟수도 말하지 않고, 그 숫자는 `stats`
 * 에 있다(서버의 `explain.GameFacts`).
 */
export interface GameSummary {
  /** 화면에 그대로 나가는 일본어. 비는 일이 없다 — 사실이 없으면 짧아질 뿐이다. */
  body: string;
  /**
   * 이 판에서 段級이 어떻게 움직였나. 판정이 표본 수를 못 채우면 없고, `before` 는
   * 첫 판이거나 익명이면 없다.
   *
   * 되짚기로 지난 판을 열 때는 언제나 없다 — 추정치는 사람에게 붙는 값이라 그때의
   * 값을 판마다 남겨 두지 않았다.
   */
  skill?: { before?: SkillRank; after: SkillRank };
  /**
   * 이 판이 기록에 남은 번호. 되짚기로 건너가는 링크가 이 값으로 선다 — 대국 화면은
   * 그때까지 자기 판의 번호를 모른다(기록이 WS 밖에서 비동기로 쓰인다).
   *
   * 되짚기가 부르는 총평에는 없다 — 이미 그 판을 열고 있어서 쓸 데가 없다.
   */
  gameId?: number;
  stats: {
    /** 사람이 확정한 수. 물러진 수는 기보에 없으므로 여기에도 없다. */
    playerMoves: number;
    /** 물러진 횟수. 같은 국면에서 여러 번 물러지면 그만큼 센다. */
    interventions: number;
    /** 많은 순. 서버가 정한 순서를 그대로 그린다. */
    categories?: { code: string; nameJa: string; count: number }[];
    /**
     * 「이 국면을 다시 봐라」. 그 판에서 낙폭이 가장 컸던 개입이다. 개입이 없으면 없다.
     *
     * `ply` 는 물러진 수의 手数라, 되짚기는 그 한 수 앞(`ply - 1`)을 연다 — 물러진 수는
     * 기보에 없으므로 그 자리가 「다시 생각할 국면」이다.
     */
    focus?: { ply: number; category: string; nameJa: string };
  };
}

export type ServerMessage =
  | { type: 'snapshot'; snapshot: Snapshot }
  | { type: 'summary'; summary: GameSummary }
  | { type: 'error'; reason: string; message: string }
  // 가정 수순의 한 자리. 스냅샷과 갈라 온다 — 대국의 상태가 아니라 「안 벌어진 일」이고,
  // 하나로 합치면 화면이 두 판을 같은 것으로 그린다.
  | { type: 'whatif'; whatif: WhatIfNode }
  | { type: 'whatif_error'; reason: string; message: string };

export type ClientMessage =
  | { type: 'move'; usi: string }
  | { type: 'resign' }
  // 手数를 안 보낸다. 무엇을 되돌릴지는 서버가 자기 기보에서 정한다 — 화면이 짚으면
  // 그 사이에 도착한 스냅샷과 어긋난 자리를 되돌리게 된다.
  | { type: 'undo' }
  // 국면을 안 보낸다. 어느 국면의 힌트인지는 서버가 자기 판에서 정한다(`undo` 와 같은 규약).
  | { type: 'hint' }
  // 판(SFEN)을 보내지 않는다. 뿌리는 서버가 자기가 방금 보낸 스냅샷에서 만든다.
  | { type: 'whatif'; ply: number; moves: string[] };
