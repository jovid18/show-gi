import { useMemo } from 'react';

import { evalText, evalTone } from '@/libs/whatif/branch';
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
 * 후보들과 같은 자가 아니다(같은 수가 +100과 +41로 나온 것을 봤다 — 06-status.md §34 ②).
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
  /** 물러진 수의 평가치. 저장돼 있지 않은 판에서는 다시 재서 채운다(`useMoveEvals`). */
  measured: Map<string, number>;
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
  /** 지금 잰 평가치. 못 잰 줄은 undefined — 자리는 지키고 값만 비운다. */
  cp: number | undefined;
  best: boolean;
  /** 실제로 이 국면에서 둔 수인가. 누가 뒀는지까지 — 상대 차례면 컴퓨터가 둔 것이다. */
  played: 'human' | 'engine' | null;
  /** 물러진 수라면 그 카테고리. 몇 번 시도했는지도 센다. */
  retracted: { categoryJa: string; message: string; tries: number } | null;
}

export function MoveOptions({ game, ply, node, measured, chosen, onPick }: MoveOptionsProps) {
  /** 뿌리에 서 있는가. 여기서만 「실제로 둔 수」와 「물러진 수」가 사실이다. */
  const atRoot = (node?.line.length ?? 0) === 0;
  /** 지금 국면의 다음 手数. 제목이 이걸 든다 — 분기로 들어가면 따라 움직인다. */
  const here = node ? node.basePly + node.line.length : ply;
  const options = useMemo<Option[]>(() => {
    const byUsi = new Map<string, Option>();

    const put = (usi: string, ja: string, patch: Partial<Option>) => {
      const at = byUsi.get(usi) ?? { usi, ja, cp: undefined, best: false, played: null, retracted: null };
      byUsi.set(usi, { ...at, ...patch, ja: at.ja || ja });
    };

    for (const c of node?.candidates ?? []) put(c.usi, c.ja || c.usi, { cp: c.evalCp, best: true });

    // 이 국면에서 실제로 둔 수 — 다음 手数의 것이다. **누가 뒀는지까지 적는다.**
    // 뿌리에서만이다: 가정으로 들어간 국면에는 「실제로 둔 수」가 없다.
    const next: ReviewMove | undefined = atRoot ? game.moves[ply] : undefined;
    if (next) put(next.usi, next.ja || next.usi, { played: next.by });

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
      put(usi, iv.retractedJa || usi, {
        retracted: { categoryJa: iv.categoryJa ?? '', message: iv.message ?? '', tries },
        // 저장된 것이 있으면 그것, 없으면 다시 잰 것. 둘 다 「그 수를 두면 얼마」다.
        cp: iv.afterCp ?? measured.get(usi),
      });
    }

    // 후보의 값도 다시 잰 것으로 덮지 않는다 — 후보는 이미 이 국면의 탐색에서 나왔다.
    return [...byUsi.values()].toSorted((a, b) => (b.cp ?? -Infinity) - (a.cp ?? -Infinity));
  }, [game.moves, game.interventions, ply, node, measured, atRoot]);

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
              style={{ '--tone': evalTone(o.cp) } as React.CSSProperties}
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
              </span>

              <span className="review-options-cp">{o.cp === undefined ? '' : evalText(o.cp)}</span>
            </button>

            {/* 왜 나빴는지는 **고른 줄에만.** 넷을 한꺼번에 펼치면 목록이 글이 된다. */}
            {chosen === o.usi && o.retracted?.message && <p className="review-iv-note">{o.retracted.message}</p>}
          </li>
        ))}
      </ul>
    </section>
  );
}
