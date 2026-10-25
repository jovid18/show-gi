// 검토 화면의 문구. 판정은 서버가 정한 것을 말로 옮길 뿐이다.

import type { Messages } from '@/i18n';
import { evalText } from '@/libs/whatif/branch';
import type { ExploreNode } from '@/protocol/explore';
import type { Turn } from '@/protocol/whatif';

/**
 * 手番의 이름.
 *
 * 駒落ち에서는 下手/上手다. 그것이 手合割의 말이고(journal §84), 平手에서 「下手」라고 쓰면
 * 접지도 않은 판에 없는 상하가 생긴다.
 */
export function sideJa(turn: Turn, handicap: boolean, t: Messages): string {
  const side = t.explore.side;
  if (handicap) return turn === 'b' ? side.shitate : side.uwate;
  return turn === 'b' ? side.sente : side.gote;
}

/**
 * 지금 판이 어떤 상태인지 한 줄로.
 *
 * 「あなた」라고 부르지 않는다. 검토에는 플레이어가 없어서 되짚기의 `branchStatusJa`
 * (「あなたの番」)를 그대로 쓸 수 없다.
 */
export function exploreStatusJa(node: ExploreNode | null, pending: boolean, t: Messages): string {
  const status = t.explore.status;
  if (pending && !node) return status.reading;
  if (!node) return status.empty;

  const side = sideJa(node.turn, !!node.handicapJa, t);
  switch (node.status) {
    case 'checkmate':
      // 詰み은 수번 쪽이 지는 것이다. 부호를 뒤집으면 이기는 판이 지는 판으로 읽힌다.
      return status.checkmate(side);
    case 'stalemate':
      // 쇼기에서 手詰まり는 패배다(체스의 무승부와 다르다).
      return status.stalemate(side);
    default:
      // 양쪽 다 둘 수 있다. 서버는 한 수도 대신 두지 않는다.
      return status.turn(side);
  }
}

/**
 * 그 手合의 「형세 0」을 말하는 한 줄. 平手면 빈 문자열이다.
 *
 * 이 줄이 없으면 二枚落ち의 0手目에 뜨는 `+1383` 이 「압승 중」으로 읽힌다. 화면에서는 값을
 * 빼는 대신 기준선을 말한다(journal §84). 숫자의 자가 되짚기 그래프와 같아야 한다.
 */
export function baselineNoteJa(node: ExploreNode | null, t: Messages): string {
  if (!node?.handicapJa || !node.baselineCp) return '';
  // 手合割 이름은 서버가 준 일본어 그대로다.
  return t.explore.baseline(node.handicapJa, evalText(node.baselineCp));
}
