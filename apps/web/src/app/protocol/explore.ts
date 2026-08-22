// 「検討」의 계약. 서버의 `internal/server/explore.go` 와 짝이다.
//
// 노드는 가정 수순의 것을 그대로 쓴다(`WhatIfNode`). 세 화면이 같은 장치를 쓰고
// (`useWhatIf`) 갈리는 것은 뿌리 하나뿐이라, 여기서 새로 정하는 것은 요청과 그 手合의
// 「형세 0」 두 칸이다.

import type { WhatIfNode } from '@/protocol/whatif';

/**
 * 「이 手合割의 0手目에서 이 수순을 뒀다면」.
 *
 * 판을 보내지 않는다. 되짚기와 같은 규약이고(whatif.ts) 여기서 더 중요하다 — 뿌리가
 * 0手目라, SFEN을 받는 표면이었다면 그건 아무 국면이나 깊이 12로 재 주는 공개 엔진이
 * 된다. 보내는 것은 手合割 id와 수순뿐이고 서버가 한 수씩 되짚어 검증한다.
 *
 * 手数가 없다. 뿌리가 언제나 0手目라 되짚기의 `ply` 에 해당하는 값이 상수다.
 */
export interface ExploreRequest {
  /** 手合割 id. 빈 값이 平手다(`/api/handicaps` 목록에 平手가 없는 것과 같은 규약). */
  handicap: string;
  /** 양쪽 수가 전부 들어 있는 한 줄. 서버는 한 수도 대신 두지 않는다. */
  moves: string[];
}

/**
 * 검토의 한 국면. 가정 수순의 노드에 그 手合의 기준점이 얹힌다.
 *
 * 화면이 그 값을 만들 수 없다. 二枚落ち의 0手目가 +1386인데 그것을 모르면 화면이
 * 「압승 중」이라고 말하게 되고, 후보 목록의 색도 한 줄도 빠짐없이 최대 파랑이 된다
 * (`evalTone` 의 base). 대국·되짚기가 같은 값을 스냅샷과 상세에 실어 보내는 것과 같은
 * 자리다(`Snapshot.baselineCp` · `GameDetail.baselineCp`).
 *
 * 부호를 뒤집을 일이 없다. 되짚기는 사람이 上手일 수 있어 기준점이 플레이어 관점으로
 * 뒤집혀 오는데, 검토의 관점은 언제나 下手다(서버의 `exploreRoot`) — 駒落ち의 0手目는
 * 上手 차례이지만(journal §88) 관점은 그것과 무관하게 못박혀 있다.
 */
export interface ExploreNode extends WhatIfNode {
  /** 그 手合割의 이름(二枚落ち). 平手면 안 온다 — 화면이 이름을 만들지 않는다. */
  handicapJa?: string;
  /** 그 手合의 「형세 0」, 下手 관점 cp. 平手면 안 온다(0이라 뺄 것이 없다). */
  baselineCp?: number;
}

/**
 * 검토 화면에서 이름을 붙여 저장한 국면. 서버의 `internal/server/explore_snapshots.go` 와 짝이다.
 *
 * 국면(SFEN)이 아니라 手合割 하나와 수순 한 줄이다. 불러오기가 이 두 칸을 그대로 주소에
 * 실어 `/api/explore` 로 다시 묻기 때문이고(`routeExplore`), 그래서 저장·불러오기가 지금까지의
 * 길에 아무것도 더하지 않는다 — SFEN을 저장하면 그 값이 곧 요청 본문이 되어 「아무 국면이나
 * 재 주는 자리」가 이쪽으로 열린다.
 *
 * 手数가 없다. `moves.length` 가 그 값이라 실어 보내면 두 칸이 어긋날 자리가 하나 생긴다.
 */
export interface ExploreSnapshot {
  id: number;
  name: string;
  /** 手合割 id. 平手면 안 온다 — 빈 값이 平手라는 규약을 그대로 쓴다. */
  handicap?: string;
  /** 그 手合割의 일본어 이름. 平手면 안 온다 — 화면이 id로 이름을 만들지 않는다. */
  handicapJa?: string;
  moves: string[];
  /** 저장한 시각(ISO). */
  savedAt: string;
}

export interface ExploreSnapshotListResponse {
  snapshots: ExploreSnapshot[];
}
