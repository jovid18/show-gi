import { useCallback, useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react';

import type { DropFrom } from '@/components/Board';
import { offsetWithin } from '@/libs/game/board-view';
import type { Side } from '@/models/piece';

export interface DropAnchor {
  /** `Board` 의 `dropFrom`. 駒 종류로 찾고, 아직 재지 못한 駒는 빠져 있다. 그 打 화살표는 그려지지 않는다. */
  dropFrom: Readonly<Record<string, DropFrom>>;
  /** 판 격자. `Board` 의 `boardRef` 에 그대로 넘긴다. */
  boardRef: RefObject<HTMLDivElement | null>;
}

const NONE: Readonly<Record<string, DropFrom>> = {};

/**
 * 打 화살표의 출발점을 재는 훅. 대국·되짚기·검토가 같이 쓴다(journal §99).
 *
 * 칸 산수로는 나오지 않는다. 駒台는 판 밖의 형제 요소이고 그 안에서 持ち駒가 몇 종류인지와
 * 라벨이 얼마나 넓은지에 따라 자리가 달라진다.
 *
 * 駒는 DOM 에서 찾는다(`.hand[data-side] .hand-piece[data-kind]`). 후보 셋이 서로 다른 駒를
 * 打할 수 있어서 ref 하나로는 모자란다.
 *
 * 인자의 identity 는 보지 않는다. 부르는 쪽이 매 렌더마다 새 배열을 줘도 되고, 여기는 값
 * 비교만으로 돈다. 그것을 의존성으로 걸면 효과가 다시 돌고 `setDropFrom` 이 또 새 객체를
 * 넣어 화면이 하얘진다.
 */
export function useDropAnchor(side: Side | null, kinds: readonly string[]): DropAnchor {
  const [dropFrom, setDropFrom] = useState<Readonly<Record<string, DropFrom>>>(NONE);
  const boardRef = useRef<HTMLDivElement>(null);
  // 폭이 바뀌는 것을 지켜보는 쪽. 판이 늦게 그려지는 화면이 있어서 「누구를 보고 있나」를 기억해 둔다.
  const observer = useRef<ResizeObserver | null>(null);
  const watched = useRef<HTMLElement | null>(null);
  // ResizeObserver 가 부르는 `measure` 는 한 번 만든 것이다. 지금 재야 할 駒를 여기서 읽는다.
  const want = useRef<{ hand: Side | null; pieces: readonly string[] }>({ hand: side, pieces: kinds });

  const measure = useCallback(() => {
    const { hand, pieces } = want.current;
    const grid = boardRef.current;
    const stage = grid?.closest('.game-board');
    // 칸을 클래스로 찾는다. `firstElementChild` 로 잡으면 `useBoardSurface` 가 붙인 캔버스가
    // 걸리고, 그때 한 칸 크기가 판 전체 폭이 되어 화살표가 판 밖까지 뻗는다(journal §127).
    const square = grid?.querySelector('.square');
    const gridAt = grid && stage instanceof HTMLElement ? offsetWithin(grid, stage) : null;
    if (!hand || !grid || !(stage instanceof HTMLElement) || !gridAt || !(square instanceof HTMLElement)) {
      setDropFrom(NONE);
      return;
    }
    const next: Record<string, DropFrom> = {};
    for (const kind of pieces) {
      const from = stage.querySelector(`.hand[data-side="${hand}"] .hand-piece[data-kind="${kind}"]`);
      const pieceAt = from instanceof HTMLElement ? offsetWithin(from, stage) : null;
      if (!(from instanceof HTMLElement) || !pieceAt) continue;
      // 판의 테두리 안쪽이 기준이다. 화살표가 그 안에 놓인다.
      next[kind] = {
        x: pieceAt.x + from.offsetWidth / 2 - (gridAt.x + grid.clientLeft),
        y: pieceAt.y + from.offsetHeight / 2 - (gridAt.y + grid.clientTop),
        sq: square.offsetWidth,
      };
    }
    // 같은 값이면 상태를 건드리지 않는다. 재는 일이 리렌더를 부르고 리렌더가 다시 재는 고리를 끊는다.
    setDropFrom((prev) => (sameAnchors(prev, next) ? prev : next));
  }, []);

  // 렌더마다 다시 잰다. 의존성 목록이 없는 것이 의도다. 駒台 駒의 자리는 판을 뒤집는 것,
  // 持ち駒가 한 종류 늘거나 주는 것, 옆 패널이 생기는 것으로 다 옮겨 가는데 그 셋 중 어느
  // 것도 `side`·`kinds` 를 바꾸지 않는다.
  //
  // 값이 같으면 상태를 건드리지 않으므로 고리가 생기지 않는다(`measure`).
  useLayoutEffect(() => {
    want.current = { hand: side, pieces: kinds };
    if (!side || kinds.length === 0) {
      setDropFrom(NONE);
      return;
    }
    measure();

    // 화면 폭이 바뀌면 `--sq` 가 따라 변하는데 그때는 렌더가 없다. 붙이는 것도 렌더마다 다시
    // 본다. 판은 국면을 읽지 못하면 그려지지 않으므로(되짚기·검토) 화살표가 살아 있는 동안에
    // 늦게 그려질 수 있고, 한 번만 시도하면 그 판에는 관찰자가 영원히 붙지 않는다.
    const stage = boardRef.current?.closest('.game-board');
    if (!(stage instanceof HTMLElement) || stage === watched.current) return;
    observer.current?.disconnect();
    observer.current = new ResizeObserver(measure);
    observer.current.observe(stage);
    watched.current = stage;
  });

  useEffect(
    () => () => {
      observer.current?.disconnect();
      observer.current = null;
      watched.current = null;
    },
    [],
  );

  return { dropFrom, boardRef };
}

function sameAnchors(a: Readonly<Record<string, DropFrom>>, b: Readonly<Record<string, DropFrom>>): boolean {
  const keys = Object.keys(a);
  if (keys.length !== Object.keys(b).length) return false;
  return keys.every((k) => {
    const p = a[k];
    const q = b[k];
    return !!q && p!.x === q.x && p!.y === q.y && p!.sq === q.sq;
  });
}
