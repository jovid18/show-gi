import type { WhatIfNode } from '@/protocol/whatif';

/**
 * 뿌리는 `handicap` 과 `sfen` 중 하나다. `sfen` 을 보내면 `handicap` 은 비워야 한다.
 */
export interface ExploreRequest {
  /** 手合割 id. 빈 값이 平手다(`/api/handicaps` 목록에 平手가 없는 것과 같은 규약). */
  handicap: string;
  sfen?: string;
  /** 양쪽 수가 전부 들어 있는 한 줄. 서버는 한 수도 대신 두지 않는다. */
  moves: string[];
}

/**
 * `evalCp` 와 `baselineCp` 는 언제나 下手(`b` 쪽) 관점이다. 되짚기는 사람이 上手면 뒤집혀
 * 오지만, 검토에는 플레이어가 없어 뒤집지 않는다(서버의 `exploreRoot`). 0手目가 上手 차례여도 같다.
 */
export interface ExploreNode extends WhatIfNode {
  /**
   * 平手와 SFEN 뿌리에서는 오지 않는다. SFEN 뿌리에는 手合割이 없어 `baselineCp` 도 없고,
   * 화면이 「互角ライン」을 말하지 않는다(journal §129).
   */
  handicapJa?: string;
  /** 그 手合의 「형세 0」, 下手 관점 cp. 平手면 오지 않는다. */
  baselineCp?: number;
}

/**
 * 手合割 id 와 수순만 저장하고 SFEN 은 저장하지 않는다(journal §96). 불러오기는 이 두 칸으로
 * `/api/explore` 를 다시 묻는다(`routeExplore`).
 */
export interface ExploreSnapshot {
  id: number;
  name: string;
  handicap?: string;
  handicapJa?: string;
  moves: string[];
  savedAt: string;
}

export interface ExploreSnapshotListResponse {
  snapshots: ExploreSnapshot[];
}
