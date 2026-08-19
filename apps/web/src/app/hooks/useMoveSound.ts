import { useCallback, useEffect, useRef, useState } from 'react';

import { clack } from '@/libs/sound/clack';

const STORAGE_KEY = 'showgi.sound';

/**
 * 저장된 것에서 「소리를 낼까」를 읽는다.
 *
 * 끈 것만 끈 것으로 센다. 기본이 켜짐이라 저장된 것이 없으면 켜짐이고, 그래야
 * `localStorage` 를 못 쓰는 브라우저에서도 기본 동작이 같다 — 「없음」과 「못 읽음」이
 * 같은 답이어야 한다.
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
 * 이 手数에서 울려야 하는가. 늘었을 때만이다.
 *
 * 되물러진 자리에서 手数가 뒤로 가는데, 그때 우는 것은 「두어졌다」는 거짓말이다 —
 * 그 수는 판에 안 남는다. `before` 가 null인 첫 렌더에서도 안 운다: 이어하기로 들어온
 * 판은 手数가 0이 아닌 채로 시작하고, 그것을 새 수로 세면 화면에 들어서자마자 울린다.
 */
export function shouldRing(before: number | null, now: number): boolean {
  return before !== null && now > before;
}

/**
 * 手数가 늘 때마다 한 번 운다. 울릴지 말지는 `shouldRing` 이 정한다.
 *
 * 手数를 세지 수를 보지 않는다. 같은 칸에 같은 駒가 다시 놓이는 판이 있어서 수의
 * 내용으로는 「새 수인가」를 못 가른다. 手数는 되물리지 않는 한 늘기만 한다.
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
        // 기억을 못 해도 이번 판에는 듣는다.
      }
      // 켜는 그 클릭에서 한 번 울린다. 눌러도 아무 일이 없으면 고장으로 읽히고,
      // 이 클릭이 곧 브라우저가 기다리던 사용자 조작이라 여기가 깨우기에도 맞다.
      if (next) clack();
      return next;
    });
  }, []);

  return [on, toggle];
}
