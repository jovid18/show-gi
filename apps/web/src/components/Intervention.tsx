import { useEffect, useRef } from 'react';

import type { Intervention as InterventionData } from '@/game/protocol';

interface InterventionProps {
  intervention: InterventionData;
  onDismiss: () => void;
}

/**
 * 제지형 개입의 문구 쪽.
 *
 * **최선수를 말하지 않는다**(docs/01-core.md §1). 여기 나오는 것은 「무엇을 물렀는가」와
 * 「왜 나쁜가」까지이고, 어느 수를 뒀어야 했는지는 없다 — 짚어주는 순간 플레이어가
 * 생각을 멈춘다. 그래서 이 컴포넌트는 서버가 준 것만 그린다. 문장을 짓지 않는다.
 *
 * 판 위의 연출(빛·유령 駒·기운 시점)은 `Board`와 `index.css`에 있다. 여기는 그 위에
 * 얹히는 마지막 한 겹이라 판을 가리지 않는 자리에 뜬다.
 */
export function Intervention({ intervention, onDismiss }: InterventionProps) {
  const dismissRef = useRef<HTMLButtonElement>(null);

  // 입력이 잠겨 있는 동안이라 초점이 갈 곳은 여기 하나뿐이다. 키보드·스크린리더
  // 사용자가 판을 더듬어 「わかった」를 찾게 두지 않는다.
  useEffect(() => {
    dismissRef.current?.focus();
  }, []);

  // 낙폭은 서버가 준 0~1이다. 여기서 다시 계산하지 않고 보이는 단위로만 바꾼다.
  // 막대가 넘치지 않게만 자른다 — 숫자를 손보기 시작하면 화면과 판정이 갈라진다.
  const drop = Math.min(100, Math.max(0, Math.round(intervention.deltaWin * 100)));

  return (
    <div className="intervention" role="alert">
      <p className="intervention-label">待った</p>

      <p className="intervention-move">
        <span className="intervention-move-ja">{intervention.retractedJa}</span>
        <span className="intervention-move-tail">を戻しました</span>
      </p>

      <p className="intervention-message">{intervention.message}</p>

      <p className="intervention-delta">
        <span className="intervention-delta-text">勝率 −{drop}%</span>
        <span className="intervention-delta-track" aria-hidden="true">
          <span className="intervention-delta-fill" style={{ width: `${drop}%` }} />
        </span>
      </p>

      <button ref={dismissRef} type="button" className="btn btn--primary" onClick={onDismiss}>
        わかった
      </button>
    </div>
  );
}
