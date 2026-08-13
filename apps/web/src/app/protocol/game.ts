// `/ws/game`의 계약. 서버의 `internal/game/session.go`·`internal/server/ws.go`와 짝이다.
//
// Go의 nil 슬라이스는 `[]`가 아니라 **`null`로 직렬화된다.** 대국 시작 직후의 `moves`,
// 엔진 차례의 `legalMoves`가 실제로 그렇게 온다. 타입에서 그걸 숨기면 첫 렌더에서 터진다.

import type { WhatIfNode } from '@/protocol/whatif';

/**
 * `aborted` 는 **상대의 수를 못 얻어서 판을 접은 것**이다. 승패가 없으므로 `winner` 가 안 온다.
 *
 * `resigned` 와 갈라져 있는 것이 요점이다 — 엔진이 답하지 않은 것을 「相手が投了しました」로
 * 그리면 지고 있던 판이 화면에서 이긴 판이 된다. 기록에서는 `abandoned` 로 남아 **이어하기
 * 목록에 그대로 올라온다.**
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
 * 제지형 개입 하나. **직전 수가 물러졌을 때만** 실려 오고, 다음 착수에서 서버가 지운다.
 *
 * 판단은 이미 서버에서 끝나 있다. 화면이 하는 일은 이걸 그리는 것뿐이고,
 * **최선수는 여기 없다** — 어느 수를 뒀어야 했는지는 알려주지 않는 것이 설계다
 * (docs/01-core.md §1).
 */
export interface Intervention {
  kind: 'blunder';
  /**
   * **왜** 나쁜가. 서버가 결정적 룰로 정한다(docs/01-core.md §3).
   *
   * 화면은 이걸로 문장을 짓지 않는다 — 문구는 `message`에 이미 들어 있고, 두 벌이
   * 되면 어긋났을 때 어느 쪽이 맞는지 알 수 없다. 연출을 카테고리별로 갈라야 할 때
   * 쓰는 자리다. 서버가 새 카테고리를 늘려도 화면이 안 깨지게 문자열로 열어 둔다.
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
  /** 물러진 수를 **둔 직후**의 국면. 되돌아온 지금 판(`Snapshot.sfen`)과는 다르다. */
  retractedSfen: string;
  /** 물러진 수가 王手였다면 그것을 거는 말들. */
  retractedChecks?: Attack[];
  /**
   * 「상대는 이렇게 詰ませてくる」. 첫 수가 상대의 수다.
   *
   * **증명된 詰み 수순일 때만 온다**(서버의 analyst.go). 한때 탐색 PV를 잘라 보내던
   * 자리인데, 어디서 자를지가 국면마다 달라 두 수에서 끊기기도 했고 읽는 사람에게
   * 「그래서 뭐」로 남았다 — 그 자리는 이제 **후보 셋을 직접 둬 보는 쪽**이 맡는다.
   * 詰み 수순만 남은 것은 그것이 자를 필요가 없는 유일한 수순이기 때문이다.
   *
   * **최선수가 아니다.** 이 수순이 시작하는 국면은 되물러서 이미 사라졌으므로 여기
   * 있는 어느 수도 「지금 이렇게 두라」가 되지 않는다.
   */
  refutation?: RefutationMove[];
}

/**
 * 반박 수순의 한 수.
 *
 * `sfen`이 있어서 **화면은 수를 두지 않는다.** 한 수씩 넘겨 볼 때 판은 이 값을 그대로
 * 그리면 된다 — 클라이언트가 스스로 두면 규칙 엔진을 한 벌 더 갖는 것이고, 그건
 * D2에서 「클라이언트는 규칙을 모른다」로 정해둔 자리다.
 */
export interface RefutationMove extends KifuMove {
  /** 이 수를 둔 **뒤**의 국면. 持ち駒까지 들어 있어 駒台도 이 값으로 맞는다. */
  sfen: string;
  /**
   * 그 수 뒤에 玉을 잡으러 오는 말들. 王手가 아니면 오지 않는다.
   *
   * 「王手다」까지는 국면만 봐도 알지만 **어느 말이 걸고 있는지**는 규칙을 알아야 하고,
   * 그건 화면이 갖지 않기로 한 것이다. 両王手가 여기서 둘로 오고, 그 둘이 곧
   * 「먹어서 풀 수 없다」의 이유다.
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
 * **단계는 서버가 자른다.** 3회에서 `usi` 는 아예 오지 않는다 — 화면이 출발 칸만 그리는
 * 것으로는 계단이 안 되고, 답이 페이로드에 그대로 있으면 힌트를 아낀 의미가 없다.
 *
 * 개입(`intervention`)과 수명은 같지만 **방향이 반대**다. 저쪽은 방금 둔 수를 말하고
 * 이쪽은 지금 둘 수를 말한다. 그래서 판 위에서도 색이 갈린다 — 상대 쪽이 초록·빨강,
 * 이쪽이 파랑이다.
 */
export interface Hint {
  /** 움직일 駒가 서 있는 칸(`5d`). 打이면 오지 않는다. */
  square?: string;
  /** 駒台에서 집을 駒(`B`). 판 위의 수면 오지 않는다. */
  drop?: string;
  /** 그 수 전체. **마지막 단계에서만 온다.** */
  usi?: string;
}

/**
 * 국면에 붙은 이름 하나 — 囲い나 전법.
 *
 * `nameJa`를 그대로 그린다. **화면이 코드를 문자열로 바꾸지 않는다** — 그러면 이름이
 * 두 벌이 되고, 어긋났을 때 어느 쪽이 맞는지 알 수 없다(棋譜 표기를 서버에서만
 * 만드는 것과 같은 자리다). `code`는 화면에 안 나가고, 나중에 그 항목의 해설을
 * 꺼내오는 검색 키로만 쓴다.
 */
export interface StyleTag {
  code: string;
  nameJa: string;
  /**
   * 축. **서버의 `tag.Kind` 와 같은 값이어야 한다** — 여기가 뒤처지면 서버가 보낸 태그를
   * 화면이 모르는 종류로 받는다. 실제로 `opening`·`tesuji` 를 서버에 추가하고 이쪽을
   * 안 고쳤다가 타입 검사가 잡았다.
   */
  kind: 'castle' | 'formation' | 'opening' | 'tesuji';
}

/**
 * 대국은 그대로 도는데 **서버가 못 해준 것** 하나.
 *
 * 개입(`intervention`)과 갈라져 온다. 저쪽은 판에 대한 판단이고 이쪽은 서버 사정이라,
 * 섞으면 「시한을 넘겨 확인 못 했다」가 화면에서 「이 수는 괜찮았다」로 읽힌다 — 초심자는
 * 아무 말이 없으면 통과한 것으로 읽으므로 그 둘이 정반대다.
 *
 * 개입과 수명이 같다. 다음 착수에서 서버가 지운다.
 */
export interface Notice {
  /** 기계용 코드. **화면은 이걸로 문장을 짓지 않는다** — `message` 를 그대로 그린다. */
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
   * 사람이 잡은 쪽.
   *
   * **`turn` 으로 되짚으면 안 된다** — 저건 지금 누구 차례냐이므로, 그것으로 판 방향을
   * 정하면 상대가 생각하는 동안 판이 뒤집힌다. 이 값은 한 판에서 변하지 않는다.
   */
  yourColor: Color;
  /**
   * 상대가 따르는 진형의 일본어 이름. 「おまかせ」로 시작했으면 오지 않는다.
   *
   * **시작 화면에서 사람이 고른 값을 되비추는 것**이다. 상대의 형태를 알려주지 않는다는
   * 것(docs/01-core.md §7)과 어긋나지 않는 이유는 서버의 `Snapshot.OpponentOpening` 주석.
   */
  opponentOpening?: string;
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
   * 플레이어가 지금 짜고 있는 囲い·전법. **상대 쪽은 오지 않는다** — 서버가 안 보낸다.
   *
   * 初期配置에서는 비어 있다. 첫 수 전에 라벨을 그리면 플레이어가 아직 하지 않은
   * 선택에 이름을 붙이는 것이 된다.
   */
  styleTags?: StyleTag[];
  /**
   * 이 국면에서 둘 수 있는 수 중 **새 이름을 만드는 것**이 있으면 그 이름.
   *
   * 제안형 힌트의 데이터다. 수를 짚지 않는다 — 화면은 「여기에 이름 있는 수가 있다」까지만
   * 보여주고, 어디에 두라는 것은 알려주지 않는다(docs/01-core.md §7.1).
   */
  tagHints?: StyleTag[];
  /**
   * 詰み 게이지의 세기(1~5). 0이거나 오지 않으면 꺼져 있다.
   *
   * **상대 玉 쪽 하나뿐이다** — 불이 붙으면 언제나 「내가 詰み에 가까워졌다」는 뜻이다.
   * 내 玉이 위험한 것은 그리지 않는다. 그쪽은 제지형 개입이 이미 막고 있고(docs/01-core.md §7),
   * 같은 테두리에 둘을 그리면 이기는 중인지 지는 중인지가 반대로 읽힌다.
   *
   * **手数가 아니라 세기다.** 서버가 잘라서 준다 — 게이지는 「얼마나 가까운가」만 말하고
   * 수순도 手数도 알려주지 않는 것이 설계라, 手数가 여기 있으면 그리지 않아도 이미
   * 알려준 것이 된다. 갇힘 힌트의 단계를 서버가 자르는 것과 같은 자리다.
   */
  mateHeat?: number;
  /**
   * 상대가 지금 겨냥하는 강함(1~5). 5가 최선수 쪽이다.
   *
   * **안 오면 조절이 꺼져 있다는 뜻이다** — 0이나 3으로 메우지 않는다. 조절하지 않는 판에
   * 눈금을 그리면 화면이 없는 기능을 말하게 된다.
   *
   * 「あなたの実力」이 아니다. 우리가 아는 것은 **이 판에서 얼마나 헤맸는가**이고, 그것도
   * 판마다 초기화된다.
   */
  opponentStrength?: number;
}

/**
 * 대국이 끝난 뒤 **한 번** 오는 총평.
 *
 * 스냅샷과 갈라져 온다 — 이건 국면의 상태가 아니라 판 전체에 대한 이야기이고, LLM을
 * 기다리므로 결과 문구보다 몇 초 늦게 도착한다.
 *
 * **숫자와 문장이 갈려 있다.** `body` 는 手数도 개입 횟수도 말하지 않고, 그 숫자는
 * `stats` 에 있다 — 같은 수를 두 곳에 두면 어긋났을 때 어느 쪽이 맞는지 알 수 없다
 * (서버의 `explain.GameFacts` 주석).
 */
export interface GameSummary {
  /** 화면에 그대로 나가는 일본어. **절대 비지 않는다** — LLM이 죽으면 결정적 문구가 온다. */
  body: string;
  /** 문장이 어디서 왔나. 0=캐시, 1=LLM, -1=결정적 문구. 화면에는 안 그린다. */
  tier: number;
  stats: {
    /** 사람이 **확정한** 수. 물러진 수는 기보에 없으므로 여기에도 없다. */
    playerMoves: number;
    /** 물러진 횟수. 같은 국면에서 여러 번 물러지면 그만큼 센다. */
    interventions: number;
    /** 많은 순. 서버가 정한 순서를 그대로 그린다. */
    categories?: { code: string; nameJa: string; count: number }[];
  };
}

export type ServerMessage =
  | { type: 'snapshot'; snapshot: Snapshot }
  | { type: 'summary'; summary: GameSummary }
  | { type: 'error'; reason: string; message: string }
  // 가정 수순의 한 자리. **스냅샷과 갈라져 온다** — 이건 대국의 상태가 아니라
  // 「안 벌어진 일」이고, 하나로 합치면 화면이 두 판을 같은 것으로 그린다.
  | { type: 'whatif'; whatif: WhatIfNode }
  | { type: 'whatif_error'; reason: string; message: string };

export type ClientMessage =
  | { type: 'move'; usi: string }
  | { type: 'resign' }
  // **판(SFEN)을 보내지 않는다.** 뿌리는 서버가 자기가 방금 보낸 스냅샷에서 만든다.
  | { type: 'whatif'; ply: number; moves: string[] };
