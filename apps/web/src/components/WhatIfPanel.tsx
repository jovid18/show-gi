import { branchStatusJa, scoreJa } from '@/review/branch';
import type { WhatIfNode } from '@/review/protocol';

/**
 * 「そのとき、こう指していたら」 — 분기 하나를 옆에서 읽는 패널.
 *
 * **여기는 판을 안 그린다.** 판은 되짚기와 같은 `Board` 하나가 그리고, 이 패널은 그
 * 판이 무엇인지(몇 手目에서 갈라졌나 · 지금 값이 얼마인가 · 무엇을 둬 볼 수 있나)를 말한다.
 * 화면을 둘로 나눈 것이 아니라, 원래 하나였던 판에 옆말이 붙은 것이다.
 *
 * **최선수를 여기서 보여주는 것이 대국의 규칙과 어긋나지 않는다**(01-core.md §7).
 * 저쪽은 **지금 둘 수**를 알려주지 않는 것이고, 여기는 이미 끝난 판에서 「그때 무엇이
 * 있었나」다 — 그 구별이 없으면 리뷰가 존재할 이유도 없다.
 */
interface WhatIfPanelProps {
  node: WhatIfNode;
  pending: boolean;
  error: string | null;
  canBack: boolean;
  onPlay: (usi: string) => void;
  onBack: () => void;
  onClose: () => void;
}

export function WhatIfPanel({ node, pending, error, canBack, onPlay, onBack, onClose }: WhatIfPanelProps) {
  const score = scoreJa(node.evalCp, node.mateIn);

  return (
    <section className="review-panel review-whatif" aria-label="もしもの手順">
      <div className="review-whatif-head">
        <h2 className="panel-title">もしも — {node.basePly}手目から</h2>
        <button type="button" className="review-back" onClick={onClose}>
          実際の対局へ
        </button>
      </div>

      <p className="review-whatif-status" data-tone={node.status === 'playing' ? 'turn' : 'result'}>
        {branchStatusJa(node, pending)}
        {/* 값은 **끝난 국면에는 없다.** 0으로 채우면 호각과 구별이 안 된다(리뷰 전체가 그렇다). */}
        {score && <span className="review-whatif-score">{score}</span>}
      </p>

      {error && (
        <p className="rejection" role="alert">
          {error}
        </p>
      )}

      {/* 분기의 수순. 실제 기보와 **같은 어휘로 같은 자리에** 선다 — 다른 모양으로 그리면
          어느 것이 실제로 둔 수인지가 흐려진다. 갈린 것은 제목 하나뿐이다. */}
      {node.line.length > 0 && (
        <ol className="review-whatif-line">
          {node.line.map((move) => (
            <li key={move.ply} data-by={move.by}>
              <span className="review-kifu-number">{move.ply}</span>
              <span className="review-kifu-move">{move.ja || move.usi}</span>
            </li>
          ))}
        </ol>
      )}

      {node.candidates.length > 0 && (
        <div className="review-whatif-best">
          <h3 className="review-whatif-sub">この局面の最善手</h3>
          <ul>
            {node.candidates.map((c) => (
              <li key={c.usi}>
                <button type="button" className="review-whatif-move" disabled={pending} onClick={() => onPlay(c.usi)}>
                  <span className="review-kifu-move">{c.ja || c.usi}</span>
                  <span className="review-whatif-score">{scoreJa(c.evalCp, c.mateIn)}</span>
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}

      {/* **되돌아가면 분기가 다시 보인다**(03-frontend.md §3). 물리는 것은 사람의 수
          한 수이고, 그에 대한 상대의 응수도 함께 사라진다 — 남겨 두면 물린 것이 아니다. */}
      {canBack && (
        <button type="button" className="btn" disabled={pending} onClick={onBack}>
          一手戻る
        </button>
      )}
    </section>
  );
}
