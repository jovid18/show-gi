import type { SkillRank, StyleTag } from './game';

export interface Profile {
  name: string;
  /**
   * 판정이 표본 수를 채우지 못했으면 없다. `step` 0 으로 메우지 않는다. 0 은 척도의 가장 낮은 段級이다.
   */
  rank?: SkillRank;
  record: { games: number; win: number; loss: number; draw: number };
  /** 전체 개입 횟수. `share` 의 분모다. `weaknesses` 는 잘려 있으므로 더해서 구하면 틀린다. */
  interventions: number;
  weaknesses?: { code: string; nameJa: string; count: number; share: number }[];
  /**
   * `games` 는 판 수다. 한 판에 같은 이름은 한 번만 담기므로(009_game_style_tags.sql) 단위가
   * 「回」 대신 「局」이다.
   */
  styles?: { code: string; nameJa: string; kind: StyleTag['kind']; games: number }[];
}

export type ProfileState =
  | { status: 'loading' }
  // 로그인하지 않은 것과 오류를 따로 둔다. 앞은 「ログインしてください」이고 뒤는 실패다.
  | { status: 'anonymous' }
  | { status: 'error' }
  | { status: 'ready'; profile: Profile };
