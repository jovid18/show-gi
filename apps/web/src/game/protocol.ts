// `/ws/game`의 계약. 서버의 `internal/game/session.go`·`internal/server/ws.go`와 짝이다.
//
// Go의 nil 슬라이스는 `[]`가 아니라 **`null`로 직렬화된다.** 대국 시작 직후의 `moves`,
// 엔진 차례의 `legalMoves`가 실제로 그렇게 온다. 타입에서 그걸 숨기면 첫 렌더에서 터진다.

import type { WhatIfNode } from '@/whatif/protocol';

export type Status = 'playing' | 'checkmate' | 'stalemate' | 'resigned' | 'repetition';
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
   * 「상대는 이렇게 벌한다」. 물러진 수를 그대로 뒀을 때의 수순이고 **첫 수가 상대의
   * 수**다. 서버가 못 구했으면 아예 오지 않는다.
   *
   * **최선수가 아니다.** 이 수순이 시작하는 국면은 되물러서 이미 사라졌으므로 여기
   * 있는 어느 수도 「지금 이렇게 두라」가 되지 않는다. 카테고리가 이유를 못 대는
   * 국면에서도 이쪽은 나오고, 그게 이 절이 있는 이유다(docs/06-status.md §17).
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

export interface Snapshot {
  sfen: string;
  ply: number;
  turn: 'b' | 'w';
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
  /**
   * 플레이어가 지금 짜고 있는 囲い·전법. **상대 쪽은 오지 않는다** — 서버가 안 보낸다.
   *
   * 初期配置에서는 비어 있다. 첫 수 전에 라벨을 그리면 플레이어가 아직 하지 않은
   * 선택에 이름을 붙이는 것이 된다.
   */
  styleTags?: StyleTag[];
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
}

export type ServerMessage =
  | { type: 'snapshot'; snapshot: Snapshot }
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
