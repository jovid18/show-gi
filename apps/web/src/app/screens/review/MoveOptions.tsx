import { useMemo } from 'react';

import { evalTone, playerCp, rankOf, rowScoreJa } from '@/libs/whatif/branch';
import type { MoveEval } from '@/hooks/useMoveEvals';
import type { GameDetail, ReviewIntervention, ReviewMove } from '@/protocol/review';
import type { WhatIfNode } from '@/protocol/whatif';

/**
 * 그 국면에서 **둘 수 있었던 수들**을 한 줄로 세운다.
 *
 * 한때 목록이 둘이었다 — 「この局面の最善手」(엔진이 고른 것)와 「戻した手」(사람이 시도했다
 * 물러진 것). 둘은 **같은 국면의 같은 종류의 사실**인데 따로 서 있어서, 「내가 둔 것이 최선수
 * 몇 위쯤인가」를 눈으로 견줄 수가 없었다. 실제로 둔 수는 어느 쪽에도 없었다.
 *
 * 합쳐서 **평가치 하나로 정렬**하면 그 물음이 그림이 된다 — 위가 좋고 아래가 나쁘고, 내가
 * 어디 있었는지가 그 사이의 한 줄이다.
 *
 * **값은 전부 지금 잰 것이다.** 저장된 `moves[].evalCp` 는 대국 중 다른 k로 잰 값이라
 * 후보들과 같은 자가 아니다(같은 수가 +100과 +41로 나온 것을 봤다 — journal §34 ②).
 * 한 줄에 두 자를 섞지 않는다.
 */
interface MoveOptionsProps {
  game: GameDetail;
  /** 확정된 手数(분기의 뿌리). 실제로 둔 수와 물러진 수가 이 자리의 사실이다. */
  ply: number;
  /**
   * **서 있는 국면**. 후보가 여기서 나온다 — 분기로 한 수 두면 그 다음 국면의 후보로 갱신된다.
   *
   * 실제로 둔 수·물러진 수는 **뿌리에서만** 나온다(`line.length === 0`). 그 둘은 확정된
   * 기보의 사실이라, 가정으로 몇 수 들어간 국면에 얹으면 그 자리에서 거짓이 된다.
   */
  node: WhatIfNode | null;
  /**
   * 후보 목록 밖의 수를 다시 잰 값(`useMoveEvals`). **플레이어 관점이다** — 아래
   * `moverScore` 가 이 열의 자(둔 쪽 관점)로 옮긴다.
   *
   * 두 자리가 이걸 쓴다: 값이 저장돼 있지 않은 물러진 수와, **실제로 둔 수**다. 뒤엣것은
   * 후보 셋 밖이면 서버가 값을 준 적이 없어서, 재지 않으면 그 줄만 빈칸으로 남는다
   * (2026-08-14-human-2.md §6 #7).
   */
  measured: Map<string, MoveEval>;
  /** 지금 고른 수. 그 줄이 열려 문구를 보여준다. */
  chosen: string | null;
  /**
   * 줄을 눌렀을 때.
   *
   * **실제로 둔 수는 「가정」이 아니다.** 그 줄을 분기로 열면 판은 같은 국면에 서는데 화면이
   * 「もしも」라고 말하게 된다 — 실제로 벌어진 일에 그 말을 붙이면 거짓이다. 그래서 그 줄은
   * **한 수 진행**으로 처리한다(`played`).
   */
  onPick: (usi: string, played: boolean) => void;
}

/** 한 줄이 무엇인가. 한 수가 둘을 겸할 수 있다 — 최선수이면서 실제로 둔 수. */
interface Option {
  usi: string;
  ja: string;
  /** 지금 잰 평가치, **둔 쪽 관점**. 못 잰 줄은 undefined — 자리는 지키고 값만 비운다. */
  cp: number | undefined;
  /** 詰み까지의 手数, **둔 쪽 관점**. 있으면 cp가 아니라 이것으로 말한다(`scoreJa`). */
  mateIn: number | undefined;
  best: boolean;
  /** 실제로 이 국면에서 둔 수인가. 누가 뒀는지까지 — 상대 차례면 컴퓨터가 둔 것이다. */
  played: 'human' | 'engine' | null;
  /** 물러진 수라면 그 카테고리. 몇 번 시도했는지도 센다. */
  retracted: { categoryJa: string; message: string; tries: number } | null;
  /**
   * 사람이 **스스로** 무른 수라면 그 횟수.
   *
   * **`retracted` 와 갈라 둔다.** 한 수가 둘을 겸할 수도 있다 — 두었다 물러지고, 다른
   * 수를 두었다가 그것도 스스로 무른 자리에서 같은 수가 다시 올라온다. 한 칸으로
   * 합치면 「AI가 막았다」와 「내가 되돌렸다」가 같은 표식이 된다.
   */
  undone: { tries: number } | null;
}

export function MoveOptions({ game, ply, node, measured, chosen, onPick }: MoveOptionsProps) {
  /** 뿌리에 서 있는가. 여기서만 「실제로 둔 수」와 「물러진 수」가 사실이다. */
  const atRoot = (node?.line.length ?? 0) === 0;
  /** 지금 국면의 다음 手数. 제목이 이걸 든다 — 분기로 들어가면 따라 움직인다. */
  const here = node ? node.basePly + node.line.length : ply;
  /**
   * 이 자리의 수를 두는 것이 상대인가. **값의 주인이 누구인지가 여기서 갈린다** —
   * 대국 중의 블런더 목록과 같은 판단이다(`Intervention` 의 `byOpponent`).
   */
  const byOpponent = !!node && !node.yourTurn;
  const options = useMemo<Option[]>(() => {
    const byUsi = new Map<string, Option>();

    /**
     * 한 줄을 겹쳐 쓴다. **먼저 들어온 값을 `undefined` 로 덮지 않는다** — 후보의 값은
     * 이 국면의 탐색에서 나온 것이고, 다시 잰 값은 그 자리가 빈 줄에만 필요하다.
     */
    const put = (usi: string, ja: string, patch: Partial<Option>) => {
      const at = byUsi.get(usi);
      byUsi.set(usi, {
        usi,
        ja: at?.ja || ja,
        cp: patch.cp ?? at?.cp,
        mateIn: patch.mateIn ?? at?.mateIn,
        best: patch.best ?? at?.best ?? false,
        played: patch.played ?? at?.played ?? null,
        retracted: patch.retracted ?? at?.retracted ?? null,
        undone: patch.undone ?? at?.undone ?? null,
      });
    };

    /**
     * 다시 잰 값을 이 열의 자로 옮긴다. `measured` 는 플레이어 관점이고 열은 둔 쪽 관점이다.
     *
     * **手数는 뒤집지 않는다** — 세는 값이라 관점을 바꿔도 자가 안 갈리고, 「누가 詰ますのか」는
     * 그리는 쪽이 정한다(`rowScoreJa`).
     */
    const moverScore = (at: MoveEval | undefined): Partial<Option> =>
      at === undefined ? {} : { cp: byOpponent ? -at.cp : at.cp, mateIn: at.mateIn };

    for (const c of node?.candidates ?? []) {
      put(c.usi, c.ja || c.usi, { cp: c.evalCp, mateIn: c.mateIn, best: true });
    }

    // 이 국면에서 실제로 둔 수 — 다음 手数의 것이다. **누가 뒀는지까지 적는다.**
    // 뿌리에서만이다: 가정으로 들어간 국면에는 「실제로 둔 수」가 없다.
    //
    // **값은 후보 셋 안에 있을 때만 이미 와 있다.** 밖이면 다시 잰 것으로 채운다 — 안 채우면
    // 그 줄만 빈칸이고, 정렬도 맨 아래로 보낸다(§6 #7). 물러진 수는 아래에서 같은 처리를
    // 받고 있었는데 둔 수만 빠져 있었다.
    const next: ReviewMove | undefined = atRoot ? game.moves[ply] : undefined;
    if (next) put(next.usi, next.ja || next.usi, { played: next.by, ...moverScore(measured.get(next.usi)) });

    // 물러진 수. 같은 수를 두 번 물린 일이 흔하므로(622의 77手) 줄을 겹치지 않고 **센다** —
    // 낙폭이 −36%/−32% 로 달랐던 것은 판정 당시의 흔들림이고, 나란히 적으면 없는 차이를
    // 가르치는 것이 된다.
    const tried = new Map<string, { iv: ReviewIntervention; tries: number }>();
    for (const iv of atRoot ? game.interventions : []) {
      if (iv.ply !== ply + 1 || !iv.retractedUsi) continue;
      const at = tried.get(iv.retractedUsi);
      tried.set(iv.retractedUsi, { iv: at?.iv ?? iv, tries: (at?.tries ?? 0) + 1 });
    }
    for (const [usi, { iv, tries }] of tried) {
      // 저장된 것이 있으면 그것, 없으면 다시 잰 것. 둘 다 「그 수를 두면 얼마」이고 둘 다
      // 플레이어 관점이라 같은 자로 옮긴다(`afterCp` 는 `moves[].evalCp` 와 같은 자다).
      const stored: Partial<Option> = iv.afterCp === undefined ? {} : { cp: byOpponent ? -iv.afterCp : iv.afterCp };
      put(usi, iv.retractedJa || usi, {
        retracted: { categoryJa: iv.categoryJa ?? '', message: iv.message ?? '', tries },
        ...moverScore(measured.get(usi)),
        ...stored,
      });
    }

    // 사람이 스스로 무른 수(待った). **물러진 수와 같은 자리에 같은 규약으로 선다** —
    // 둘 다 「이 국면에서 뒀는데 기보에 없는 수」라서 여기 말고는 보일 자리가 없다.
    // 갈리는 것은 표식 하나다: 저쪽은 AI가 막았고 이쪽은 사람이 되돌렸다(회차 1 #4).
    const undone = new Map<string, number>();
    for (const u of atRoot ? (game.undos ?? []) : []) {
      if (u.ply !== ply + 1) continue;
      undone.set(u.usi, (undone.get(u.usi) ?? 0) + 1);
    }
    for (const [usi, tries] of undone) {
      // 저장된 평가치는 `moves[].evalCp` 에서 옮겨 온 것이라 이미 플레이어 관점이다 —
      // 물러진 수의 `afterCp` 와 같은 자, 같은 변환이다.
      const first = game.undos.find((u) => u.usi === usi && u.ply === ply + 1);
      const stored: Partial<Option> =
        first?.evalCp === undefined ? {} : { cp: byOpponent ? -first.evalCp : first.evalCp };
      put(usi, first?.ja || usi, { undone: { tries }, ...moverScore(measured.get(usi)), ...stored });
    }

    // **詰み이 cp보다 언제나 바깥이다.** cp만으로 세우면 「3手で詰み」과 「+2900」이 이웃으로
    // 서는데 그 둘은 이웃이 아니다(`rankOf`).
    return [...byUsi.values()].toSorted((a, b) => rankOf(b) - rankOf(a));
  }, [game.moves, game.interventions, game.undos, ply, node, measured, atRoot, byOpponent]);

  if (!options.length) return null;

  return (
    <section className="review-panel review-options" aria-label="この局面で指せた手">
      {/* **어느 쪽 수인지를 양쪽 다 적는다.** 「相手の番」만 적고 내 차례에는 아무 말도 안 하면,
          말이 없는 것이 「내 차례」라는 뜻인지 「아직 모른다」는 뜻인지 갈리지 않는다 — 분기로
          몇 수 들어가면 차례가 한 수씩 바뀌므로 그 자리에서 특히 그렇다. */}
      <h2 className="panel-title">
        {here + 1}手目 — この局面で指せた手
        {node && <span className="review-options-turn">{node.yourTurn ? 'あなたの番' : '相手の番'}</span>}
      </h2>

      <ul>
        {options.map((o) => (
          <li key={o.usi}>
            <button
              type="button"
              className="review-options-row"
              data-chosen={chosen === o.usi || undefined}
              // 색이 값이라, 값이 없는 줄은 색도 없다. 0으로 채우면 호각으로 읽힌다.
              style={{ '--tone': evalTone(playerCp(o, byOpponent)) } as React.CSSProperties}
              onClick={() => onPick(o.usi, o.played !== null)}
            >
              <span className="review-options-move">{o.ja}</span>

              {/* 표식은 **사실만** 적는다. 최선수인지, 누가 실제로 뒀는지, 물러졌는지. */}
              <span className="review-options-tags">
                {o.played === 'human' && <span data-role="played">あなたが指した手</span>}
                {o.played === 'engine' && <span data-role="played">相手が指した手</span>}
                {o.retracted && (
                  <span data-role="retracted">
                    {o.retracted.categoryJa}
                    {o.retracted.tries > 1 && ` ×${o.retracted.tries}`}
                  </span>
                )}
                {/* **AI가 막은 것과 다른 표식이다.** 저 줄은 카테고리 이름(タダ捨て)을 들고
                    이 줄은 「待った」를 든다 — 같은 목록에 나란히 서므로 표식이 갈려야
                    「막힌 수」와 「내가 되돌린 수」를 구별할 수 있다. */}
                {o.undone && (
                  <span data-role="undone">
                    待った
                    {o.undone.tries > 1 && ` ×${o.undone.tries}`}
                  </span>
                )}
              </span>

              <span className="review-options-cp">{rowScoreJa(o, byOpponent)}</span>
            </button>

            {/* 왜 나빴는지는 **고른 줄에만.** 넷을 한꺼번에 펼치면 목록이 글이 된다. */}
            {chosen === o.usi && o.retracted?.message && <p className="review-iv-note">{o.retracted.message}</p>}
          </li>
        ))}
      </ul>
    </section>
  );
}
