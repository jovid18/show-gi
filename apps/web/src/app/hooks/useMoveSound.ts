import { useCallback, useEffect, useRef, useState } from 'react';

import { clack } from '@/libs/sound/clack';

const STORAGE_KEY = 'showgi.sound';

/**
 * 저장된 착수음 설정을 읽는다.
 *
 * `off`만 꺼짐으로 처리한다. 설정이 없거나 `localStorage`를 읽지 못하면 켜짐을 기본값으로 쓴다.
 */
export function soundOnFrom(raw: string | null): boolean {
  return raw !== 'off';
}

function stored(): boolean {
  try {
    return soundOnFrom(window.localStorage.getItem(STORAGE_KEY));
  } catch {
    return true;
  }
}

/**
 * 이전 렌더보다 手数가 늘었을 때만 착수음을 재생한다.
 *
 * 되무르기로 手数가 줄거나 `before`가 null인 첫 렌더에서는 재생하지 않는다.
 * 이어하기는 手数가 0보다 큰 상태로 시작하므로 첫 렌더를 새 착수로 처리하면 안 된다.
 */
export function shouldRing(before: number | null, now: number): boolean {
  return before !== null && now > before;
}

/**
 * 착수음 설정을 관리하고 `shouldRing`의 결과에 따라 소리를 재생한다.
 *
 * 같은 칸에 같은 駒를 다시 놓을 수 있으므로 수의 내용 대신 手数로 새 착수를 구분한다.
 * 手数는 되무르지 않는 한 증가한다.
 */
export function useMoveSound(ply: number): [boolean, () => void] {
  const [on, setOn] = useState(stored);
  const seen = useRef<number | null>(null);

  useEffect(() => {
    const before = seen.current;
    seen.current = ply;
    if (on && shouldRing(before, ply)) clack();
  }, [ply, on]);

  const toggle = useCallback(() => {
    setOn((was) => {
      const next = !was;
      try {
        window.localStorage.setItem(STORAGE_KEY, next ? 'on' : 'off');
      } catch {
        // 저장에 실패해도 현재 세션에는 변경한 설정을 적용한다.
      }
      // 켜는 클릭에서 미리듣기를 재생한다. 사용자 조작 중이므로 브라우저의 오디오도 활성화할 수 있다.
      if (next) clack();
      return next;
    });
  }, []);

  return [on, toggle];
}
