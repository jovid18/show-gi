import { useEffect, useRef } from 'react';

import type { Intervention as InterventionData } from '@/game/protocol';

interface InterventionProps {
  intervention: InterventionData;
  /** 지금 보고 있는 회상 장면. 0이 물러진 수 자체이고, null이면 넘겨 볼 것이 없다. */
  scene: number | null;
  /** 장면이 모두 몇 개인가. */
  scenes: number;
  /** 수순에서 지금 짚고 있는 수. 판의 화살표가 가리키는 것과 같은 자리다. */
  highlight: number;
  /**
   * 장면마다의 평가치. 길이는 **물러진 수부터 센다**(1이 물러진 수 직후).
   *
   * **안 가 본 장면은 빈칸이다.** 값은 그 국면을 실제로 재야 나오고, 재는 것은 사람이
   * 그 장면을 볼 때다 — 없는 것을 지어내지 않는다.
   */
  evalAt: (sceneLength: number) => string;
  /** 이 장면의 판이 살아 있는가 — 그 자리에서 직접 둬 볼 수 있다는 뜻이다. */
  canPlay: boolean;
  onStep: (scene: number) => void;
  onDismiss: () => void;
}

/**
 * 제지형 개입의 문구 쪽.
 *
 * **최선수를 말하지 않는다**(docs/01-core.md §1). 여기 나오는 것은 「무엇을 물렀는가」와
 * 「왜 나쁜가」까지이고, 어느 수를 뒀어야 했는지는 없다 — 짚어주는 순간 플레이어가
 * 생각을 멈춘다. 그래서 이 컴포넌트는 서버가 준 것만 그린다. 문장을 짓지 않는다.
 *
 * 판 위의 연출(빛·유령 駒·광선·기운 시점)은 `Board`와 `index.css`에 있다. 여기는 그 위에
 * 얹히는 마지막 한 겹이라 판을 가리지 않는 자리에 뜬다.
 */
export function Intervention({
  intervention,
  scene,
  scenes,
  highlight,
  evalAt,
  canPlay,
  onStep,
  onDismiss,
}: InterventionProps) {
  const dismissRef = useRef<HTMLButtonElement>(null);

  // 입력이 잠겨 있는 동안이라 초점이 갈 곳은 여기 하나뿐이다. 키보드·스크린리더
  // 사용자가 판을 더듬어 「指し直す」를 찾게 두지 않는다.
  useEffect(() => {
    dismissRef.current?.focus();
  }, []);

  // 서버가 못 구했으면 아예 오지 않는다. 그때는 이 절이 통째로 빠지고 나머지는 그대로다 —
  // 반박 수순은 개입의 조건이 아니라 개입에 얹히는 재료다.
  const refutation = intervention.refutation ?? [];
  const walking = scene !== null;

  // 낙폭은 서버가 준 0~1이다. 여기서 다시 계산하지 않고 보이는 단위로만 바꾼다.
  // 막대가 넘치지 않게만 자른다 — 숫자를 손보기 시작하면 화면과 판정이 갈라진다.
  const drop = Math.min(100, Math.max(0, Math.round(intervention.deltaWin * 100)));

  const step = (delta: number): void => {
    if (scene === null) return;
    onStep(Math.min(scenes - 1, Math.max(0, scene + delta)));
  };

  return (
    <div
      className="intervention"
      role="alert"
      // 판을 보면서 넘기는 화면이라 손이 버튼에 가 있을 이유가 없다. 초점이 이 안에
      // 있는 동안 좌우 키가 그대로 前へ·次へ다.
      onKeyDown={(e) => {
        if (e.key === 'ArrowLeft') step(-1);
        else if (e.key === 'ArrowRight') step(1);
        else return;
        e.preventDefault();
      }}
    >
      <p className="intervention-label">待った</p>

      <p className="intervention-move">
        <span className="intervention-move-ja">{intervention.retractedJa}</span>
        <span className="intervention-move-tail">を戻しました</span>
        {/* **그 수를 두면 얼마가 되나.** 낙폭(%)은 판정의 값이고 이건 국면의 값이다 —
            아래 수순의 cp와 같은 자를 쓰므로 「거기서 더 나빠지는가」가 견줘진다. */}
        {evalAt(1) && <span className="intervention-move-eval">{evalAt(1)}</span>}
      </p>

      <p className="intervention-message">{intervention.message}</p>

      {refutation.length > 0 && (
        // 카테고리가 이유를 못 대는 국면이 3분의 2다(docs/06-status.md §17). 그때 위의
        // 문구는 「형세를 손해본다」까지밖에 못 말하고, **여기가 그 빈자리를 메운다.**
        <div className="refutation">
          <p className="refutation-label">相手はこう咎めてきます</p>
          {/* **한 줄로 붙여 쓰지 않는다.** 수마다 평가치가 붙으면서 줄마다 한 수가 되고,
              그러면 「어디서 무너지는가」가 숫자로 보인다 — 棋譜와 같은 모양이다. */}
          <ol className="refutation-line">
            {refutation.map((move, i) => (
              // 같은 수가 수순에 두 번 나올 수 있다(千日手·왕복). 자리까지 키에 넣는다.
              <li
                key={`${i}-${move.usi}`}
                data-by={move.by}
                data-current={highlight === i || undefined}
                aria-current={highlight === i || undefined}
              >
                <span className="refutation-move">{move.ja}</span>
                <span className="refutation-eval">{evalAt(i + 2)}</span>
              </li>
            ))}
          </ol>
        </div>
      )}

      <p className="intervention-delta">
        <span className="intervention-delta-text">勝率 −{drop}%</span>
        <span className="intervention-delta-track" aria-hidden="true">
          <span className="intervention-delta-fill" style={{ width: `${drop}%` }} />
        </span>
      </p>

      {/* **버튼이 아니라 한 줄이다.** 판이 이미 살아 있으므로 누를 것이 없고, 여기에
          「최선수를 둔다」 같은 버튼을 두면 그 순간 답을 알려주는 것이 된다(01-core.md §7).
          물러진 수는 고정이라 분기는 언제나 그 수 **뒤**에서 시작한다(06-status.md §25). */}
      {canPlay && <p className="intervention-try">盤の駒を動かすと、この局面から指し直せます。</p>}

      <div className="intervention-actions">
        {walking && (
          // 앞뒤로 넘기며 판이 어떻게 되는지 본다. 넘기지 않고 「指し直す」만 눌러도
          // 되는 것이 요점이다 — 넘겨 보는 것은 궁금한 사람의 몫이지 통과 의례가 아니다.
          <>
            <button
              type="button"
              className="btn btn--step"
              disabled={scene === 0}
              aria-label="前の手へ"
              onClick={() => step(-1)}
            >
              前へ
            </button>
            <button
              type="button"
              className="btn btn--step"
              disabled={scene >= scenes - 1}
              aria-label="次の手へ"
              onClick={() => step(1)}
            >
              次へ
            </button>
          </>
        )}
        <button ref={dismissRef} type="button" className="btn btn--primary" onClick={onDismiss}>
          指し直す
        </button>
      </div>
    </div>
  );
}
