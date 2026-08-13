// 가정 수순과 되짚기가 함께 쓰는 순수 계산들. **여기에 규칙 판단이 없다** —
// 합법수도 국면도 서버가 주고, 이 파일이 하는 것은 좌표와 문구뿐이다.

import { parseUsi } from '@/libs/game/moves';
import type { WhatIfNode } from '@/protocol/whatif';
import { fromUsi, toIndex, type Motion } from '@/models/square';

/** 手数 하나를 넘어갈 때 볼 수 있는 것 — USI 하나면 된다. 기보든 분기든 같다. */
interface Played {
  usi: string;
}

/**
 * 한 수의 움직임. `undo`면 **되감는 쪽**이라 방향이 뒤집힌다.
 *
 * 읽을 수 없는 좌표면 안 그린다 — 엉뚱한 칸에서 駒가 날아오면 「무엇이 어디서 왔나」를
 * 틀리게 가르친다.
 */
function motionOf(usi: string, undo: boolean, id: number): Motion | null {
  const move = parseUsi(usi);
  if (!move) return null;
  try {
    const to = toIndex(fromUsi(move.to));
    if (move.kind === 'drop') {
      // 되감으면 駒가 판 밖(駒台)으로 돌아간다. 판 위에 그릴 움직임이 없다.
      return undo ? null : { from: null, to, id };
    }
    const from = toIndex(fromUsi(move.from));
    return undo ? { from: to, to: from, id } : { from, to, id };
  } catch {
    return null;
  }
}

/**
 * 手数를 하나 넘어갈 때의 움직임.
 *
 * **뛰어넘으면 없다.** 슬라이더로 40手를 건너뛰는 것은 한 수가 아니고, 거기에 움직임을
 * 그리면 있지도 않았던 한 수를 그리는 것이 된다. 뒤로 한 칸이면 그 수를 **되감는다** —
 * 판이 그 방향으로 돌아가는 것이 사실이다.
 */
export function stepMotion(moves: readonly Played[], from: number, to: number, id: number): Motion | null {
  if (to === from + 1) {
    const usi = moves[to - 1]?.usi;
    return usi ? motionOf(usi, false, id) : null;
  }
  if (to === from - 1) {
    const usi = moves[from - 1]?.usi;
    return usi ? motionOf(usi, true, id) : null;
  }
  return null;
}

/** 분기가 방금 둔 수의 움직임. 분기는 앞으로만 간다 — 물리는 것은 판을 새로 받는다. */
export function branchMotion(node: WhatIfNode, id: number): Motion | null {
  const usi = node.line.at(-1)?.usi;
  return usi ? motionOf(usi, false, id) : null;
}

/** 평가치를 부호까지 읽히게 적는다. */
export function evalText(cp: number): string {
  return cp > 0 ? `+${cp}` : `${cp}`;
}

/**
 * 사람이 판 위에서 직접 둬 본 수 하나.
 *
 * **후보 셋 밖의 수다.** 값은 그 수를 둔 뒤의 국면을 서버가 재서 온 것이고, 여기서는
 * **그 수를 둔 쪽 관점**으로 뒤집혀 있다 — 후보(`WhatIfCandidate.evalCp`)와 같은 자를
 * 써야 한 줄에 나란히 설 수 있다.
 *
 * **들고 있는 것은 화면뿐이다.** 새로고침하면 사라진다 — 대국의 사실이 아니라 그 사람이
 * 지금 무엇을 궁금해했는가라서, 남길 자리가 없다. 잰 값 자체는 서버가 이미 `positions` 에
 * 남겼다(internal/archive).
 */
export interface ExploredMove {
  usi: string;
  ja: string;
  cp: number | undefined;
  mateIn: number | undefined;
}

/**
 * 한 줄에 세우기 위한 순서값. **詰み이 cp보다 언제나 바깥이다.**
 *
 * 詰み을 환산값(30000)으로 섞으면 「3手で詰み」과 「+2900」이 이웃으로 서고, 그 둘은
 * 이웃이 아니다. 그리고 **빨리 죽는 쪽이 더 나쁘다** — 부호만 보고 자르면 그 순서가 뒤집힌다.
 */
export function rankOf(r: { cp: number | undefined; mateIn: number | undefined }): number {
  if (r.mateIn) return r.mateIn > 0 ? 1e6 - r.mateIn : -1e6 - r.mateIn;
  return r.cp ?? -1e9;
}

/**
 * 색은 **평가치가 정한다.** 파랑이 좋고 빨강이 나쁘다.
 *
 * 판 위에서 색을 넷으로 제한한 것과 어긋나지 않는다 — 여기는 판이 아니라 목록이고, 판에서
 * 파랑·빨강이 뜻하는 것(힌트·王手)과 자리가 겹치지 않는다. 대신 **판에 쓰는 그 토큰을
 * 그대로 쓴다**: 새 색을 꺼내면 팔레트가 넷이 아니게 된다.
 *
 * `±800cp` 에서 양 끝에 닿는다. 그 폭이면 駒 하나 반쯤이고, 대부분의 국면이 그 안에서 갈린다.
 *
 * **넣는 값은 플레이어 관점이어야 한다.** 파랑·빨강은 이 앱 어디서나 「나에게 좋은가」이고,
 * 「그 수를 둔 쪽에게 좋은가」를 그대로 칠하면 **상대의 결정타가 가장 파랗게** 나온다.
 * 목록의 숫자는 둔 쪽 관점이라(후보끼리 견주는 자이므로) 부르는 쪽이 뒤집어서 넘긴다.
 */
const TONE_FULL = 800;

export function evalTone(cp: number | undefined): string {
  if (cp === undefined) return 'transparent';
  const t = Math.max(-1, Math.min(1, cp / TONE_FULL));
  const token = t >= 0 ? '--hint' : '--ray-check';
  return `rgb(var(${token}) / ${(Math.abs(t) * 0.5).toFixed(2)})`;
}

/**
 * 그 자리의 값 한 줄.
 *
 * **詰み은 cp로 말하지 않는다.** 30000은 평가치가 아니라 환산값이고, 초심자에게
 * 「+30000」은 아무것도 아니다 — 「몇 手で詰み」이 그 자리에서 유일하게 뜻이 있는 말이다.
 */
export function scoreJa(cp: number | undefined, mateIn?: number): string {
  if (mateIn) {
    return mateIn > 0 ? `${mateIn}手で詰み` : `${-mateIn}手で詰まされる`;
  }
  return cp === undefined ? '' : evalText(cp);
}

/**
 * 지금 분기가 어떤 상태인지 한 줄로.
 *
 * **판정을 여기서 하지 않는다.** 상태도 차례도 서버가 정한 것을 말로 옮길 뿐이다.
 */
export function branchStatusJa(node: WhatIfNode, pending: boolean): string {
  if (pending) return '読んでいます…';
  switch (node.status) {
    case 'checkmate':
      return node.yourTurn ? '詰みです。あなたの負けでした。' : '詰みです。あなたの勝ちでした。';
    case 'stalemate':
      // 쇼기에서 手詰まり는 무승부가 아니라 패배다.
      return node.yourTurn ? '手詰まりです。あなたの負けでした。' : '手詰まりです。あなたの勝ちでした。';
    case 'resigned':
      return '相手が投了しました。';
    default:
      // **어느 쪽이든 사람이 둔다.** 상대 차례면 「상대라면 어떻게 둘까」를 직접 둬 보는 것이
      // 이 화면의 내용이고, 그때 초록 화살표가 엔진의 답을 짚고 있다.
      return node.yourTurn ? 'あなたの番。盤の上で指してみてください。' : '相手の番。相手の手も指してみられます。';
  }
}
