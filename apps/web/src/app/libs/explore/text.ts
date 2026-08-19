// 검토 화면의 문구. **판정을 여기서 하지 않는다** — 서버가 정한 것을 말로 옮길 뿐이다
// (libs/whatif/branch.ts 의 `branchStatusJa` 와 같은 규약).

import { evalText } from '@/libs/whatif/branch';
import type { ExploreNode } from '@/protocol/explore';
import type { Turn } from '@/protocol/whatif';

/**
 * 手番의 이름.
 *
 * **駒落ち에서는 下手/上手다.** 그것이 手合割의 말이고(internal/handicap · journal §84),
 * 平手에서 「下手」라고 쓰면 접지도 않은 판에 없는 상하가 생긴다. 반대로 駒落ち를
 * 先手/後手로만 부르면 정석서와 대조가 안 된다.
 */
export function sideJa(turn: Turn, handicap: boolean): string {
  if (handicap) return turn === 'b' ? '下手' : '上手';
  return turn === 'b' ? '先手' : '後手';
}

/**
 * 지금 판이 어떤 상태인지 한 줄로.
 *
 * **「あなた」라고 안 부른다.** 검토에는 플레이어가 없다 — 양쪽 다 사람이 두고, 그래서
 * 되짚기의 `branchStatusJa`(「あなたの番」)를 여기 그대로 쓸 수 없다.
 */
export function exploreStatusJa(node: ExploreNode | null, pending: boolean): string {
  if (pending && !node) return '読んでいます…';
  if (!node) return '手合割をえらんで、盤の上で指してみてください。';

  const side = sideJa(node.turn, !!node.handicapJa);
  switch (node.status) {
    case 'checkmate':
      // 詰み은 **수번 쪽이** 지는 것이다. 부호를 뒤집으면 이기는 판이 지는 판으로 읽힌다.
      return `詰みです。${side}の負けです。`;
    case 'stalemate':
      // 쇼기에서 手詰まり는 무승부가 아니라 패배다.
      return `手詰まりです。${side}の負けです。`;
    default:
      // **양쪽 다 둘 수 있다.** 상대의 응수를 직접 둬 보는 것이 이 화면의 내용이고,
      // 서버는 한 수도 대신 두지 않는다.
      return `${side}の番。どちらの駒も動かせます。`;
  }
}

/**
 * 그 手合의 「형세 0」을 말하는 한 줄. **平手면 빈 문자열**이다.
 *
 * 이 줄이 없으면 二枚落ち의 0手目에 뜨는 `+1383` 이 「압승 중」으로 읽힌다 — 판정식이
 * 그 값을 빼고 도는 것과 같은 이유이고(journal §84), 화면에서는 빼는 대신 **기준선을
 * 말한다**: 숫자의 자를 되짚기 그래프와 같게 두려면 값을 옮길 수가 없다.
 */
export function baselineNoteJa(node: ExploreNode | null): string {
  if (!node?.handicapJa || !node.baselineCp) return '';
  return `${node.handicapJa}の互角は ${evalText(node.baselineCp)} あたりです。`;
}
