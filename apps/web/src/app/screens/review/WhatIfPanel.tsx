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
 *
 * **국면을 기다리는 동안에도 이 패널은 서 있는다.** `node` 가 `null` 이면 아직 못 받은 것이고,
 * 그때 통째로 다른 컴포넌트로 바꾸면 手数를 넘길 때마다 옆 열이 무너지고 다시 선다 — 그것이
 * 「3개 보였다가 0개 보였다가」로 보였다. 자리는 지키고 **내용만** 기다린다.
 */
interface WhatIfPanelProps {
  /** 지금 보고 있는 手数. **`node` 가 아니라 이쪽이 제목을 든다** — 기다리는 동안에도 맞아야 한다. */
  basePly: number;
  /**
   * 그릴 국면. **아직 이 手数의 것이 아닐 수 있다** — 기다리는 동안 직전 것을 그대로 두고,
   * 값이 오면 그 자리에서 갈아 끼운다(`stale`).
   */
  node: WhatIfNode | null;
  /**
   * 지금 그리고 있는 것이 **이 手数의 것이 아닌가.**
   *
   * 자리지킴 빈 줄로 두는 방법도 있었는데, 그 줄과 실제 줄의 높이를 계속 맞춰야 해서
   * 목록이 12~22px씩 들썩였다. 직전 값을 두고 갈아 끼우면 그 문제가 없다 — 대신 잠깐
   * **다른 국면의 숫자**가 서 있으므로, 흐리게 하고 **누를 수 없게** 한다.
   */
  stale: boolean;
  pending: boolean;
  error: string | null;
  /** 엔진이 떠 있는가. `false` 면 둬 볼 수가 없으므로 그렇게 말한다. */
  engineReady: boolean | null;
  /** 줄이 그 길이였을 때의 값. 수마다의 cp가 여기서 나온다(`useWhatIf.evalOf`). */
  evalOf: (lineLength: number) => { cp: number | undefined; mateIn: number | undefined } | null;
  onPlay: (usi: string) => void;
  onBack: () => void;
  onRoot: () => void;
}

export function WhatIfPanel({
  basePly,
  node,
  stale,
  pending,
  error,
  engineReady,
  evalOf,
  onPlay,
  onBack,
  onRoot,
}: WhatIfPanelProps) {
  const score = node ? scoreJa(node.evalCp, node.mateIn) : '';
  const branching = (node?.line.length ?? 0) > 0;
  const down = engineReady === false;

  return (
    <section className="review-panel review-whatif" aria-label="もしもの手順">
      <div className="review-whatif-head">
        <h2 className="panel-title">もしも — {basePly}手目から</h2>
        {/* 값은 **끝난 국면에는 없다.** 0으로 채우면 호각과 구별이 안 된다(리뷰 전체가 그렇다). */}
        {score && (
          <span className="review-whatif-score" data-stale={stale || undefined}>
            {score}
          </span>
        )}
      </div>

      {/* **기다리는 동안 글자를 바꾸지 않는다.** 한때 여기가 `読んでいます…` 로 바뀌었는데,
          手数를 넘길 때마다 500ms씩 문구가 번쩍여서 그것만 눈에 남았다. 「아직 이 국면의
          값이 아니다」는 **흐림**이 이미 말한다(`data-stale`) — 채널을 둘로 두지 않는다. */}
      <p
        className="review-whatif-status"
        data-tone={node?.status === 'playing' ? 'turn' : 'result'}
        data-stale={stale || undefined}
      >
        {down
          ? 'エンジンが動いていないため、この局面から指し直すことはできません。'
          : node
            ? branchStatusJa(node, false)
            : 'この局面の駒を動かすと、そこから指し直せます。'}
      </p>

      {error && (
        <p className="rejection" role="alert">
          {error}
        </p>
      )}

      {/* 분기의 수순. 실제 기보와 **같은 어휘로 같은 모양**으로 선다 — 手数 · 수 · cp.
          갈리는 것은 왼쪽의 얇은 선 하나뿐이고, 그건 「여기서 갈라졌다」다. */}
      {node && branching && (
        <ol className="review-whatif-line" data-stale={stale || undefined}>
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

      {/* 엔진이 죽었으면 최선수 자리를 아예 안 만든다 — 영원히 안 올 것에 자리를 비워 두면
          「고장 났다」로 읽힌다. 그 밖에는 **기다리는 동안에도 자리를 지킨다.** */}
      {!down && (
        <div className="review-whatif-best" data-stale={stale || undefined}>
          <h3 className="review-whatif-sub">{node && !node.yourTurn ? '相手の最善手' : 'この局面の最善手'}</h3>
          <ul>
            {node?.candidates.map((c) => (
              <li key={c.usi}>
                <button
                  type="button"
                  className="review-whatif-move"
                  // 흐린 줄은 다른 국면의 수다. 누르면 그 국면에 없는 수를 두려 하게 된다.
                  disabled={pending || stale}
                  onClick={() => onPlay(c.usi)}
                >
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
      {/* **기다리는 동안 사라지지 않는다.** 감추면 값이 올 때 패널이 두 번 움직인 것처럼
          보인다 — 누를 수 없게만 한다. */}
      {branching && (
        <div className="review-whatif-actions">
          <button type="button" className="btn" disabled={pending || stale} onClick={onBack}>
            一手戻る
          </button>
          <button type="button" className="btn" disabled={pending || stale} onClick={onRoot}>
            分岐の前へ
          </button>
        </div>
      )}
    </section>
  );
}
