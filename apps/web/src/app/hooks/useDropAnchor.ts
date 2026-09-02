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
  /** `Board` 의 `dropFrom`. 아직 못 쟀으면 null이고, 그때 打 화살표는 안 그려진다. */
  dropFrom: DropFrom | null;
  /** 판 격자. `Board` 의 `boardRef` 에 그대로 넘긴다. */
  boardRef: RefObject<HTMLDivElement | null>;
  /** 그 駒의 DOM. `Hand` 의 `droppingRef` 에 그대로 넘긴다. */
  pieceRef: (el: HTMLButtonElement | null) => void;
}

/**
 * 打 화살표의 출발점을 재는 훅. 대국·되짚기·검토가 같이 쓴다.
 *
 * 칸 산수로는 안 나온다. 駒台는 판 밖의 형제 요소이고 그 안에서 持ち駒가 몇 종류인지·
 * 라벨이 얼마나 넓은지에 따라 자리가 달라진다 — 그래서 재야 하고, 재면 된다.
 *
 * 세 화면이 같은 계산을 두 벌로 들고 있었고, 그래서 검토에는 아예 안 붙어 있었다
 * (journal §99). 재는 자리를 하나로 모으는 것이 세 번째를 붙이는 값이다.
 *
 * 인자의 identity 는 안 본다. 부르는 쪽이 매 렌더마다 새 객체를 줘도 된다 — 그것을
 * 의존성으로 걸었을 때 효과가 다시 돌고 `setDropFrom` 이 또 새 객체를 넣어 화면이
 * 하얘진 적이 있고, 그래서 여기는 `side`·`kind` 와 값 비교만으로 돈다.
 */
export function useDropAnchor(dropping: DropPiece | null): DropAnchor {
  const [dropFrom, setDropFrom] = useState<DropFrom | null>(null);
  const boardRef = useRef<HTMLDivElement>(null);
  const piece = useRef<HTMLButtonElement | null>(null);
  // 폭이 바뀌는 것을 지켜보는 쪽. 판이 늦게 서는 화면이 있어서 「누구를 보고 있나」를 들고 있는다.
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
    // 칸을 클래스로 찾는다. `firstElementChild` 로 잡으면 three.js 판에서 **캔버스가 걸린다** —
    // `useBoardSurface` 가 `appendChild` 로 붙이는 요소라 React 가 칸을 다시 세우는 렌더에서
    // 앞으로 올라오고, 그때 한 칸 크기(62px)가 판 전체 폭(568px)이 된다. 打 화살표가
    // 589px 대신 2999px 로 판 밖까지 뻗은 자리다(journal §127).
    const square = grid.querySelector('.square');
    if (!pieceAt || !gridAt || !(square instanceof HTMLElement)) {
      setDropFrom(null);
      return;
    }
    // 판의 테두리 안쪽이 기준이다 — 화살표가 그 안에 놓이므로.
    const next = {
      x: pieceAt.x + from.offsetWidth / 2 - (gridAt.x + grid.clientLeft),
      y: pieceAt.y + from.offsetHeight / 2 - (gridAt.y + grid.clientTop),
      sq: square.offsetWidth,
    };
    // 같은 값이면 상태를 안 건드린다 — 재는 일이 리렌더를 부르고 리렌더가 다시 재는 고리를 끊는다.
    setDropFrom((prev) => (prev && prev.x === next.x && prev.y === next.y && prev.sq === next.sq ? prev : next));
  }, []);

  const side = dropping?.side ?? null;
  const kind = dropping?.kind ?? null;

  // 렌더마다 다시 잰다. 의존성 목록이 없는 것이 의도다 — 駒台 駒의 자리는 판을 뒤집는
  // 것, 持ち駒가 한 종류 늘거나 줄는 것, 옆 패널이 서는 것으로 다 옮겨 가고, 그 셋 중
  // 어느 것도 `side`·`kind` 를 바꾸지 않는다. 판을 뒤집었을 때 화살표가 반대쪽 駒台에서
  // 뻗어 있던 것이 그 자리다.
  //
  // 값이 같으면 상태를 안 건드리므로 고리가 안 생긴다(`measure`).
  useLayoutEffect(() => {
    if (!side || !kind) {
      setDropFrom(null);
      return;
    }
    measure();

    // 화면 폭이 바뀌면 `--sq` 가 따라 변하는데 그때는 렌더가 없다. 붙이는 것도 렌더마다
    // 다시 본다 — 판은 국면을 못 읽으면 안 그려지므로(되짚기·검토) 화살표가 살아 있는
    // 동안에 늦게 설 수 있고, 한 번만 시도하면 그 판에는 관찰자가 영원히 안 붙는다.
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
