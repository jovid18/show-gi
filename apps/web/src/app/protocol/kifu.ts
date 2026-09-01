// `/api/kifu/*` 의 계약. 서버의 `internal/server/kifu_import.go` 와 짝이다.
//
// 두 단계다. 읽기(`/parse`)는 판을 안 만들고 즉시 답하며, 취해 오기(`/import`)가 판을
// 만들어 분석 줄에 세운다 — 잘못 읽은 기보에 엔진 몇 분을 쓰지 않기 위해서고, 그 사이에
// 사람이 手数와 앞뒤 수를 눈으로 확인한다.
//
// 원문을 두 번 보낸다. 서버에 중간 상태가 없기 때문이고(파싱이 결정적이다), 화면은
// 붙여 넣은 글을 그대로 들고 있다가 두 번째 요청에 다시 싣는다.

import type { MyColor } from '@/protocol/review';

/** 사람이 고른 결과. 기보가 결과를 안 말할 때만 보낸다. */
export type ChosenResult = 'win' | 'loss' | 'draw';

export interface KifuRequest {
  text: string;
  myColor?: MyColor;
  result?: ChosenResult;
}

/** 기보가 말한 결과. 안 말하면 아예 오지 않고, 그때 화면이 사람에게 묻는다. */
export type RecordedResult = 'sente' | 'gote' | 'draw';

export interface KifuPreview {
  plies: number;
  /** 手合割 이름(香落ち). 平手면 오지 않는다 — `GameSummary.handicapJa` 와 같은 규약이다. */
  handicapJa?: string;
  sente?: string;
  gote?: string;
  result?: RecordedResult;
  /**
   * 결정적 파서가 못 읽어 AI 가 서식을 옮겨 적었나.
   *
   * 참이면 화면이 한 줄 붙인다. 옮겨 적은 수도 전부 룰 엔진을 지나 왔지만(서버의
   * `kifu.ParseMoves`), 사람이 자기 기보인지 눈으로 확인할 수 있게 하는 것이
   * 지어내기에 대한 두 번째 방어다.
   */
  transcribed: boolean;
  /** 棋譜 표기의 앞쪽. 사람이 자기 판인지 알아보는 단서다. */
  head: string[];
  /** 뒤쪽. 짧은 판에는 오지 않는다 — 앞쪽이 이미 전부다. */
  tail?: string[];
}

export interface KifuImported {
  gameId: number;
}
