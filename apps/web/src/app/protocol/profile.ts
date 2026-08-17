import type { SkillRank, StyleTag } from './game';

/**
 * `GET /api/me/profile`. **로그인한 사람만 받는다** — 익명 판은 서로 구별할 수단이 없어서
 * (`002_anonymous_games.sql`) 「이 사람의 전적」에 답할 수가 없다.
 */
export interface Profile {
  name: string;
  /**
   * 지금의 段級. **없을 수 있다** — 판정이 표본 수를 못 채웠으면 이름을 안 붙인다.
   * 0으로 메우지 않는 이유는 그 값이 척도의 가장 낮은 이름이기 때문이다.
   */
  rank?: SkillRank;
  record: { games: number; win: number; loss: number; draw: number };
  /** 전체 개입 횟수. 아래 `share` 의 분모다 — 목록은 잘려 있으므로 더해서 구하면 틀린다. */
  interventions: number;
  /** 많은 순. 서버가 정한 순서를 그대로 그린다. 두 번 미만인 카테고리는 안 온다. */
  weaknesses?: { code: string; nameJa: string; count: number; share: number }[];
  /**
   * 지금까지 짠 囲い·戦法·戦型. 많은 순이고 **판 수**다 — 한 판에 같은 이름은 한 번만
   * 담기므로(009_game_style_tags.sql) 「回」가 아니라 「局」이다.
   *
   * **手筋은 안 온다.** 이름의 정확도가 아직 보류라(journal §45), 「당신이 쓴 手筋」로
   * 세우면 오진이 사람의 기록으로 굳는다.
   */
  styles?: { code: string; nameJa: string; kind: StyleTag['kind']; games: number }[];
}

export type ProfileState =
  | { status: 'loading' }
  // **로그인 안 함과 오류를 갈라 둔다** — 앞은 「ログインしてください」이고 뒤는 실패다.
  | { status: 'anonymous' }
  | { status: 'error' }
  | { status: 'ready'; profile: Profile };
