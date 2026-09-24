// `/parse` 는 판을 만들지 않고, `/import` 가 판을 만든다. 서버가 `/parse` 의 결과를 들고 있지
// 않으므로 `/import` 에도 원문을 다시 싣는다.

import type { MyColor } from '@/protocol/review';

/** 사람 관점의 결과. 기보가 결과를 말하지 않을 때만 보낸다. */
export type ChosenResult = 'win' | 'loss' | 'draw';

export interface KifuRequest {
  text: string;
  myColor?: MyColor;
  result?: ChosenResult;
}

/** 기보가 말한 결과. 先手/後手 기준이다. 말하지 않으면 오지 않고, 그때 화면이 사람에게 묻는다. */
export type RecordedResult = 'sente' | 'gote' | 'draw';

export interface KifuPreview {
  plies: number;
  handicapJa?: string;
  sente?: string;
  gote?: string;
  result?: RecordedResult;
  /**
   * 결정적 파서가 읽지 못해 AI 가 서식을 옮겨 적었나. 옮겨 적은 수도 룰 엔진을 지나 왔지만
   * (서버의 `kifu.ParseMoves`), 참이면 사람이 수순을 눈으로 확인하게 한 줄 붙인다.
   */
  transcribed: boolean;
  head: string[];
  tail?: string[];
}

export interface KifuImported {
  gameId: number;
}
