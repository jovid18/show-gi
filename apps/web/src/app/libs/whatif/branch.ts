// 가정 수순과 되짚기가 함께 쓰는 순수 계산들. 합법수도 국면도 서버가 주고, 여기는 좌표와
// 문구다.

import { parseUsi } from '@/libs/game/moves';
import type { WhatIfNode } from '@/protocol/whatif';
import { fromUsi, toIndex, type Motion } from '@/models/square';

/** 手数 하나를 넘어갈 때 볼 수 있는 것. USI 하나면 되고, 기보든 분기든 같다. */
interface Played {
  usi: string;
}

/**
 * 한 수의 움직임. `undo`면 되감는 쪽이라 방향이 뒤집힌다.
 *
 * 읽을 수 없는 좌표면 그리지 않는다. 엉뚱한 칸에서 駒가 날아오면 「무엇이 어디서 왔나」를
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
 * 뛰어넘으면 없다. 슬라이더로 40手를 건너뛰는 것은 여러 手를 한 번에 지나는 일이라, 거기에
 * 움직임을 그리면 있지도 않았던 한 수를 그리게 된다. 뒤로 한 칸이면 그 수를 되감는다.
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

/** 분기가 방금 둔 수의 움직임. 분기는 앞으로만 가고, 물리는 것은 판을 새로 받는다. */
export function branchMotion(node: WhatIfNode, id: number): Motion | null {
  const usi = node.line.at(-1)?.usi;
  return usi ? motionOf(usi, false, id) : null;
}

/** 평가치를 부호까지 읽히게 적는다. */
export function evalText(cp: number): string {
  return cp > 0 ? `+${cp}` : `${cp}`;
}

/**
 * 사람이 판 위에서 직접 둬 본 수 하나. 후보 셋 밖의 수다.
 *
 * 값은 그 수를 둔 뒤의 국면을 서버가 잰 것이고, 그 수를 둔 쪽 관점으로 뒤집혀 있다.
 * 후보(`WhatIfCandidate.evalCp`)와 같은 자를 써야 한 줄에 함께 설 수 있다.
 *
 * 들고 있는 것은 화면뿐이라 새로고침하면 사라진다. 잰 값 자체는 서버가 `positions` 에
 * 남겼다(internal/archive).
 */
export interface ExploredMove {
  usi: string;
  ja: string;
  cp: number | undefined;
  mateIn: number | undefined;
}

/**
 * 한 줄에 세우기 위한 순서값. 詰み이 cp 보다 언제나 바깥이고, 빨리 죽는 쪽이 더 나쁘다.
 * 부호만 보고 자르면 그 순서가 뒤집힌다.
 *
 * 서버도 같은 규칙으로 센다(`eval.Compare`, journal §131). 갈리면 목록의 1위와 판 위의
 * 초록 화살표가 다른 수를 가리킨다.
 */
export function rankOf(r: { cp: number | undefined; mateIn: number | undefined }): number {
  if (r.mateIn) return r.mateIn > 0 ? 1e6 - r.mateIn : -1e6 - r.mateIn;
  return r.cp ?? -1e9;
}

/** 색이 양 끝에 닿는 평가치. */
const TONE_FULL = 800;

/**
 * 색은 평가치가 정한다. 파랑이 좋고 빨강이 나쁘며, `TONE_FULL` 에서 양 끝에 닿는다.
 *
 * 판에서 색을 넷으로 제한한 것과 어긋나지 않는다. 여기는 목록이라 파랑·빨강이 판에서
 * 뜻하는 것(힌트·王手)과 자리가 겹치지 않고, 쓰는 토큰은 판의 것 그대로다.
 *
 * 넣는 값은 플레이어 관점이어야 한다. 목록의 숫자는 둔 쪽 관점이라, 그대로 칠하면 상대의
 * 결정타가 가장 파랗게 나온다. 부르는 쪽이 뒤집어서 넘긴다.
 *
 * `base` 는 그 판의 「형세 0」이다(`Snapshot.baselineCp` · `GameDetail.baselineCp`). 빼지
 * 않으면 六枚落ち(+2003)에서 모든 후보가 ±800 을 넘어 최대 파랑이 된다(journal §84).
 */
export function evalTone(cp: number | undefined, base = 0): string {
  if (cp === undefined) return 'transparent';
  const t = Math.max(-1, Math.min(1, (cp - base) / TONE_FULL));
  const token = t >= 0 ? '--hint' : '--ray-check';
  return `rgb(var(${token}) / ${(Math.abs(t) * 0.5).toFixed(2)})`;
}

/**
 * 그 자리의 값 한 줄.
 *
 * 詰み은 cp 로 말하지 않는다. 서버가 그때 `cp` 를 아예 보내지 않고, 초심자에게 큰 숫자는
 * 아무것도 아니다.
 */
export function scoreJa(cp: number | undefined, mateIn?: number): string {
  if (mateIn) {
    return mateIn > 0 ? `${mateIn}手で詰み` : `${-mateIn}手で詰まされる`;
  }
  return cp === undefined ? '' : evalText(cp);
}

/**
 * 목록 한 줄의 값. 둘 다 「그 수를 둔 쪽 관점」이라 위가 「그 쪽에게 좋은 수」가 된다.
 *
 * `ExploredMove` 와 `WhatIfCandidate` 가 이 모양을 만족한다. 두 화면이 같은 목록이라 부호
 * 규칙도 한 벌이어야 한다.
 */
export interface MoverScore {
  cp: number | undefined;
  mateIn: number | undefined;
}

/**
 * 그 줄의 값 한 칸. `byOpponent` 는 그 수를 두는 쪽이 상대인가다.
 *
 * 詰み의 手数만 플레이어 관점으로 옮긴다. 手数는 세는 값이라 관점을 바꿔도 자가 갈리지
 * 않고, 그대로 두면 상대의 詰み을 내 詰み으로 말하게 된다(`lets_mate` 카테고리 전체가 그
 * 자리다). cp 는 열의 자를 지켜 둔 쪽 관점으로 남는다.
 */
export function rowScoreJa(row: MoverScore, byOpponent: boolean): string {
  if (!row.mateIn) return scoreJa(row.cp, undefined);
  return scoreJa(undefined, byOpponent ? -row.mateIn : row.mateIn);
}

/**
 * 색에 넘길 값. 파랑·빨강은 이 앱 어디서나 「나에게 좋은가」라서 플레이어 관점이어야
 * 하고(`evalTone`), 열의 숫자는 둔 쪽 관점이라 여기서 뒤집는다. 서버의 `playerScore` 와
 * 같은 일이다(branch.go).
 *
 * 詰み은 색으로 말하지 않는다. 그 줄에는 얹을 cp 자체가 없다.
 */
export function playerCp(row: MoverScore, byOpponent: boolean): number | undefined {
  if (row.mateIn || row.cp === undefined) return undefined;
  return byOpponent ? -row.cp : row.cp;
}

/** 지금 분기가 어떤 상태인지 한 줄로. 상태도 차례도 서버가 정한 것을 말로 옮길 뿐이다. */
export function branchStatusJa(node: WhatIfNode, pending: boolean): string {
  if (pending) return '読んでいます…';
  switch (node.status) {
    case 'checkmate':
      return node.yourTurn ? '詰みです。あなたの負けでした。' : '詰みです。あなたの勝ちでした。';
    case 'stalemate':
      // 쇼기에서 手詰まり는 패배다(체스의 무승부와 다르다).
      return node.yourTurn ? '手詰まりです。あなたの負けでした。' : '手詰まりです。あなたの勝ちでした。';
    case 'resigned':
      return '相手が投了しました。';
    default:
      // 어느 쪽이든 사람이 둔다. 상대 차례면 「상대라면 어떻게 둘까」를 직접 둬 보는 자리다.
      return node.yourTurn ? 'あなたの番。盤の上で指してみてください。' : '相手の番。相手の手も指してみられます。';
  }
}
