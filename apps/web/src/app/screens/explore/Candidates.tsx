import type { CSSProperties } from 'react';

import { evalTone, playerCp, rowScoreJa } from '@/libs/whatif/branch';
import { sideJa } from '@/libs/explore/text';
import type { ExploreNode } from '@/protocol/explore';

/**
 * 지금 국면의 최선수 Top 3. 누르면 그 수를 둔다.
 *
 * 되짚기의 `MoveOptions` 와 갈라 둔다. 저쪽은 후보에 「실제로 둔 수」와 「물러진 수」를
 * 한 줄로 세우는 목록이고(그것이 그 화면의 내용이다), 검토에는 그 둘이 아예 없다 —
 * 확정된 기보가 없기 때문이다. 같은 컴포넌트에 넣으면 절반이 언제나 빈 채로 도는
 * 분기가 되고, 그 분기가 두 화면을 동시에 잡는다.
 *
 * 판 위의 초록 화살표가 첫 줄과 같은 수다(03-frontend.md §2). 여기서 색을 새로 꺼내지
 * 않고 판이 이미 쓰는 토큰을 그대로 쓴다(`evalTone`).
 */
interface CandidatesProps {
  node: ExploreNode | null;
  /** 다른 국면의 값을 그리고 있는가. 흐리게 하고 누를 수 없게 한다. */
  stale: boolean;
  onPick: (usi: string) => void;
}

export function Candidates({ node, stale, onPick }: CandidatesProps) {
  const handicap = !!node?.handicapJa;
  /**
   * 색은 下手 관점이다. 파랑·빨강이 이 앱 어디서나 「나에게 좋은가」인데
   * (`evalTone`) 검토의 「나」는 下手(SFEN의 `b` 쪽)로 못박혀 있다(서버의 `exploreRoot`) —
   * 목록의 숫자는 둔 쪽 관점이라 여기서 뒤집어서 넘긴다.
   */
  const byOpponent = node?.turn === 'w';

  return (
    <section className="review-panel explore-options" aria-label="最善手">
      <h2 className="panel-title">
        {node ? `${node.ply + 1}手目 · ${sideJa(node.turn, handicap)}の最善手` : '最善手'}
      </h2>

      {/* 자리를 지키고 내용만 기다린다. 통째로 다른 것으로 바꾸면 한 수 둘 때마다 옆
          열이 무너지고 다시 서서, 「3개 보였다가 0개 보였다가」로 보인다. */}
      {!node || node.candidates.length === 0 ? (
        <p className="review-empty">{node ? 'この局面に指す手はありません。' : '読み込むと、ここに三つ出ます。'}</p>
      ) : (
        <ol className="explore-option-list" data-stale={stale || undefined}>
          {node.candidates.map((c, i) => {
            // 후보의 값을 목록 한 줄의 자로 옮긴다. `evalCp` 는 둔 쪽 관점이고, 그것이
            // 후보끼리 견주는 자다(`MoverScore` — 되짚기의 목록과 같은 규약).
            const score = { cp: c.evalCp, mateIn: c.mateIn };
            return (
              // 열쇠는 순위다. 수(`usi`)로 걸면 서버가 같은 수를 두 번 보낸 순간 열쇠가
              // 겹치고, React는 그때 그 줄을 지우지 못한다 — 국면을 옮겨도 옛 수가 1위
              // 자리에 남아 「1위가 둘인 목록」이 새로고침 전까지 안 없어졌다(journal §87).
              // 이 목록의 정체는 애초에 순위 1·2·3이라 자리가 곧 그 줄이다.
              <li key={i}>
                <button
                  type="button"
                  className="explore-option"
                  // 첫 줄이 판 위의 초록 화살표다. 그 사실을 한 번 더 색으로 말하지 않고
                  // 순위 숫자로만 짚는다 — 판과 목록이 같은 것을 두 채널로 말하지 않는다.
                  data-best={i === 0 || undefined}
                  disabled={stale}
                  onClick={() => onPick(c.usi)}
                  style={{ '--tone': evalTone(playerCp(score, byOpponent), node.baselineCp ?? 0) } as CSSProperties}
                >
                  <span className="explore-option-rank">{i + 1}</span>
                  <span className="explore-option-move">{c.ja || c.usi}</span>
                  <span className="explore-option-score">{rowScoreJa(score, byOpponent)}</span>
                  {/* 낙폭은 최선수 대비다. 첫 줄에는 없다(기준이라 0) — 서버가 그 자리를
                      비워서 보내므로 화면이 뺄셈을 하지 않는다(whatifCandidate 의 LossCp). */}
                  <span className="explore-option-loss">{c.lossCp ? `−${c.lossCp}` : ''}</span>
                </button>
              </li>
            );
          })}
        </ol>
      )}
    </section>
  );
}
