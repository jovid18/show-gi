import { useCallback, useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react';

import type { DropFrom } from '@/components/Board';
import { offsetWithin } from '@/libs/game/board-view';
import type { Side } from '@/models/piece';

/** 화살표가 駒台에서 출발하는가. 그렇다면 어느 쪽의 무슨 駒인가. */
export interface DropPiece {
  side: Side;
  kind: string;
}

export interface DropAnchor {
  /** `Board` 의 `dropFrom`. 아직 재지 못했으면 null이고, 그때 打 화살표는 그려지지 않는다. */
  dropFrom: DropFrom | null;
  /** 판 격자. `Board` 의 `boardRef` 에 그대로 넘긴다. */
  boardRef: RefObject<HTMLDivElement | null>;
  /** 그 駒의 DOM. `Hand` 의 `droppingRef` 에 그대로 넘긴다. */
  pieceRef: (el: HTMLButtonElement | null) => void;
}

/**
 * 打 화살표의 출발점을 재는 훅. 대국·되짚기·검토가 같이 쓴다(journal §99).
 *
 * 칸 산수로는 나오지 않는다. 駒台는 판 밖의 형제 요소이고 그 안에서 持ち駒가 몇 종류인지와
 * 라벨이 얼마나 넓은지에 따라 자리가 달라진다.
 *
 * 인자의 identity 는 보지 않는다. 부르는 쪽이 매 렌더마다 새 객체를 줘도 되고, 여기는
 * `side`·`kind` 와 값 비교만으로 돈다. 그것을 의존성으로 걸면 효과가 다시 돌고
 * `setDropFrom` 이 또 새 객체를 넣어 화면이 하얘진다.
 */
export function useDropAnchor(dropping: DropPiece | null): DropAnchor {
  const [dropFrom, setDropFrom] = useState<DropFrom | null>(null);
  const boardRef = useRef<HTMLDivElement>(null);
  const piece = useRef<HTMLButtonElement | null>(null);
  // 폭이 바뀌는 것을 지켜보는 쪽. 판이 늦게 그려지는 화면이 있어서 「누구를 보고 있나」를 기억해 둔다.
  const observer = useRef<ResizeObserver | null>(null);
  const watched = useRef<HTMLElement | null>(null);

  const pieceRef = useCallback((el: HTMLButtonElement | null) => {
    piece.current = el;
  }, []);

  const measure = useCallback(() => {
    const grid = boardRef.current;
    const from = piece.current;
    const stage = grid?.closest('.game-board');
    if (!grid || !from || !(stage instanceof HTMLElement)) {
      setDropFrom(null);
      return;
    }
    const pieceAt = offsetWithin(from, stage);
    const gridAt = offsetWithin(grid, stage);
    // 칸을 클래스로 찾는다. `firstElementChild` 로 잡으면 `useBoardSurface` 가 붙인 캔버스가
    // 걸리고, 그때 한 칸 크기가 판 전체 폭이 되어 화살표가 판 밖까지 뻗는다(journal §127).
    const square = grid.querySelector('.square');
    if (!pieceAt || !gridAt || !(square instanceof HTMLElement)) {
      setDropFrom(null);
      return;
    }
    // 판의 테두리 안쪽이 기준이다. 화살표가 그 안에 놓인다.
    const next = {
      x: pieceAt.x + from.offsetWidth / 2 - (gridAt.x + grid.clientLeft),
      y: pieceAt.y + from.offsetHeight / 2 - (gridAt.y + grid.clientTop),
      sq: square.offsetWidth,
    };
    // 같은 값이면 상태를 건드리지 않는다. 재는 일이 리렌더를 부르고 리렌더가 다시 재는 고리를 끊는다.
    setDropFrom((prev) => (prev && prev.x === next.x && prev.y === next.y && prev.sq === next.sq ? prev : next));
  }, []);

  const side = dropping?.side ?? null;
  const kind = dropping?.kind ?? null;

  // 렌더마다 다시 잰다. 의존성 목록이 없는 것이 의도다. 駒台 駒의 자리는 판을 뒤집는 것,
  // 持ち駒가 한 종류 늘거나 주는 것, 옆 패널이 생기는 것으로 다 옮겨 가는데 그 셋 중 어느
  // 것도 `side`·`kind` 를 바꾸지 않는다.
  //
  // 값이 같으면 상태를 건드리지 않으므로 고리가 생기지 않는다(`measure`).
  useLayoutEffect(() => {
    if (!side || !kind) {
      setDropFrom(null);
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

  return { dropFrom, boardRef, pieceRef };
}
