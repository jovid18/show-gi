import { branchStatusJa, lossJa, scoreJa } from '@/whatif/branch';
import type { WhatIfNode } from '@/whatif/protocol';

/**
 * 「そのとき、こう指していたら」 — 분기 하나를 옆에서 읽는 패널.
 *
 * **여기는 판을 안 그린다.** 판은 되짚기·대국과 같은 `Board` 하나가 그리고, 이 패널은 그
 * 판이 무엇인지(어디서 갈라졌나 · 지금 값이 얼마인가 · 무엇을 둬 볼 수 있나)를 말한다.
 *
 * **두 화면이 이것을 그대로 같이 쓴다.** 되짚는 판에서는 手数 옆에, 대국 중에는 개입 카드
 * 자리에 선다 — 같은 장치가 두 자리에서 다르게 자라지 않게 하는 유일한 방법이다.
 */
interface WhatIfPanelProps {
  node: WhatIfNode;
  pending: boolean;
  error: string | null;
  /**
   * 최선수 목록을 내놓는가.
   *
   * **되짚는 판에서만 켠다.** 대국 중에는 그 목록이 곧 「지금 어떻게 둬야 하나」의 답이 되고
   * (분기를 물리면 그 자리가 다시 서는 국면이다), 그건 이 제품이 안 하기로 한 것이다
   * (01-core.md §7). 화살표는 양쪽에 다 뜬다 — 그건 **상대**가 어떻게 오는가다.
   */
  candidates: boolean;
  onPlay: (usi: string) => void;
  onBack: () => void;
  onRoot: () => void;
}

export function WhatIfPanel({ node, pending, error, candidates, onPlay, onBack, onRoot }: WhatIfPanelProps) {
  const score = scoreJa(node.evalCp, node.mateIn);
  const branching = node.line.length > 0;

  return (
    <section className="review-panel review-whatif" aria-label="もしもの手順">
      <div className="review-whatif-head">
        <h2 className="panel-title">もしも — {node.basePly}手目から</h2>
        {/* 값은 **끝난 국면에는 없다.** 0으로 채우면 호각과 구별이 안 된다(리뷰 전체가 그렇다). */}
        {score && <span className="review-whatif-score">{score}</span>}
      </div>

      <p className="review-whatif-status" data-tone={node.status === 'playing' ? 'turn' : 'result'}>
        {branchStatusJa(node, pending)}
      </p>

      {error && (
        <p className="rejection" role="alert">
          {error}
        </p>
      )}

      {/* 분기의 수순. 실제 기보와 **같은 어휘로 같은 모양**으로 선다 — 手数 · 수 · cp.
          갈리는 것은 왼쪽의 얇은 선 하나뿐이고, 그건 「여기서 갈라졌다」다. */}
      {branching && (
        <ol className="review-whatif-line">
          {node.line.map((move, i) => (
            <li key={move.ply} data-by={move.by}>
              <span className="review-kifu-number">{move.ply}</span>
              <span className="review-kifu-move">{move.ja || move.usi}</span>
              {/* cp는 **그 수를 둔 뒤**의 값이다. 마지막 수의 값이 곧 지금 국면의 값이라
                  위에 이미 있고, 여기서는 앞선 수들이 어떻게 흘렀는지가 보인다. */}
              <span className="review-kifu-eval">{i === node.line.length - 1 ? score : ''}</span>
            </li>
          ))}
        </ol>
      )}

      {candidates && node.candidates.length > 0 && (
        <div className="review-whatif-best">
          <h3 className="review-whatif-sub">{node.yourTurn ? 'この局面の最善手' : '相手の最善手'}</h3>
          <ul>
            {node.candidates.map((c) => (
              <li key={c.usi}>
                <button type="button" className="review-whatif-move" disabled={pending} onClick={() => onPlay(c.usi)}>
                  <span className="review-kifu-move">{c.ja || c.usi}</span>
                  <span className="review-whatif-score">{scoreJa(c.evalCp, c.mateIn)}</span>
                  <span className="review-whatif-loss">{lossJa(c.lossCp)}</span>
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}

      {/* 되돌아가는 길이 둘이다. **한 수씩 물리는 것과 분기를 접는 것은 다른 일이다** —
          몇 수를 들어간 뒤에 원래 판으로 돌아가려고 「一手戻る」를 다섯 번 누르게 두지 않는다. */}
      {branching && (
        <div className="review-whatif-actions">
          <button type="button" className="btn" disabled={pending} onClick={onBack}>
            一手戻る
          </button>
          <button type="button" className="btn" disabled={pending} onClick={onRoot}>
            分岐の前へ
          </button>
        </div>
      )}
    </section>
  );
}
