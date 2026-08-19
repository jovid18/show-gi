import { useEffect } from 'react';

/**
 * 두는 중에 탭을 닫거나 새로고침하려 하면 브라우저에 물어보게 한다.
 *
 * 회차 2 #1. 판은 연결 하나에 매여 있어서(`ws.go`) 문서가 떠나는 순간 그 판이 끝난다 —
 * 로그인한 사람은 이어하기가 되받지만(§51) 익명은 그대로 잃는다.
 *
 * 문구를 정할 수 없다. 브라우저가 자기 문장을 쓴다 — 여기서 할 수 있는 것은 물어보게
 * 하는 것뿐이고, `returnValue` 는 그 규약을 켜는 스위치다.
 *
 * 화면 안의 이동에는 안 걸린다. 두는 중에는 다른 주소가 판으로 되돌아와(`App.tsx`)
 * 나갈 수 있는 자리가 아니고, 문서를 떠나는 이 길이 판을 잃는 유일한 길이다.
 */
export function useUnloadGuard(active: boolean): void {
  useEffect(() => {
    if (!active) return;

    const onBeforeUnload = (e: BeforeUnloadEvent): void => {
      e.preventDefault();
      // 낡은 브라우저는 `preventDefault` 만으로는 안 묻는다. 빈 문자열이면 충분하고,
      // 넣은 문장은 어차피 안 보인다.
      e.returnValue = '';
    };

    window.addEventListener('beforeunload', onBeforeUnload);
    return () => window.removeEventListener('beforeunload', onBeforeUnload);
  }, [active]);
}
