import { branchStatusJa, scoreJa } from '@/libs/whatif/branch';
import type { WhatIfNode } from '@/protocol/whatif';

/**
 * 「そのとき、こう指していたら」 — 분기 하나를 옆에서 읽는 패널.
 *
 * **여기는 판을 안 그린다.** 판은 되짚기와 같은 `Board` 하나가 그리고, 이 패널은 그 판이
 * 무엇인지(어디서 갈라졌나 · 지금 값이 얼마인가 · 무엇을 둬 볼 수 있나)를 말한다.
 *
 * **되짚는 화면 전용이다.** 한때 대국 중의 개입 카드 자리에도 섰는데, 카드를 읽다가 판을
 * 만지면 이 패널이 그 자리를 빼앗아 설명이 사라졌다. 대국 중에는 판이 잠기고 둬 보는 길이
 * 없다(GameScreen). 최선수 목록을 그릴 수 있는 것도 **끝난 판이라서**다 — 살아 있는 판에서
 * 그것을 적으면 「지금 어떻게 둬야 하나」의 답이 되고, 그건 안 하기로 한 것이다(01-core.md §7).
 */
interface WhatIfPanelProps {
  node: WhatIfNode;
  pending: boolean;
  error: string | null;
  /** 줄이 그 길이였을 때의 값. 수마다의 cp가 여기서 나온다(`useWhatIf.evalOf`). */
  evalOf: (lineLength: number) => { cp: number | undefined; mateIn: number | undefined } | null;
  onPlay: (usi: string) => void;
  onBack: () => void;
  onRoot: () => void;
}

export function WhatIfPanel({ node, pending, error, evalOf, onPlay, onBack, onRoot }: WhatIfPanelProps) {
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
          {node.line.map((move, i) => {
            // cp는 **그 수를 둔 뒤**의 값이다. 지나온 자리는 이미 받아 뒀으므로 다시 묻지
            // 않고 꺼내 온다 — 그래서 「어디서 무너지는가」가 줄을 따라 읽힌다.
            const at = evalOf(i + 1);
            return (
              <li key={move.ply} data-by={move.by}>
                <span className="review-kifu-number">{move.ply}</span>
                <span className="review-kifu-move">{move.ja || move.usi}</span>
                <span className="review-kifu-eval">{at ? scoreJa(at.cp, at.mateIn) : ''}</span>
              </li>
            );
          })}
        </ol>
      )}

      {node.candidates.length > 0 && (
        <div className="review-whatif-best">
          <h3 className="review-whatif-sub">{node.yourTurn ? 'この局面の最善手' : '相手の最善手'}</h3>
          <ul>
            {node.candidates.map((c) => (
              <li key={c.usi}>
                <button type="button" className="review-whatif-move" disabled={pending} onClick={() => onPlay(c.usi)}>
                  <span className="review-kifu-move">{c.ja || c.usi}</span>
                  {/* **숫자는 하나다.** 1위 대비 낙폭(`lossCp`)도 서버가 주지만 안 그린다 —
                      절대 평가치에서 뺄셈으로 나오는 같은 사실이고, 1위 줄만 빈칸이 되어
                      「0」이 아니라 「값이 없다」로 읽혔다. 「이 수가 더 나쁘다」는 순서가 말한다.
                      그리고 이 앱의 다른 숫자가 전부 이 자다(개입 카드·반박 수순·기보). */}
                  <span className="review-whatif-score">{scoreJa(c.evalCp, c.mateIn)}</span>
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
