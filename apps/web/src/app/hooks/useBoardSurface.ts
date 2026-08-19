// 판 표면을 판에 붙인다 — 재고, 채우고, 밀어 넣는다.

import { useEffect, useLayoutEffect, useRef, useState } from 'react';

// 값이 아니라 타입만 가져온다. 여기서 `three` 를 정적으로 들여오면 아래의 늦은
// import가 아무 값도 못 한다 — 번들러가 이미 첫 덩어리에 넣어 버린다.
import type { BoardSurface, Layout } from '@/libs/three/surface';
import type { Board } from '@/models/sfen';
import type { Side } from '@/models/piece';
import { exposure, influenceOf } from '@/models/influence';

const BOARD_SIZE = 9;

/**
 * 그늘이 켜지고 꺼지는 데 걸리는 시간. 넘겨 보는 장면 사이 간격보다 짧아야 한다.
 *
 * 켤 때와 끌 때가 같은 값이다. 켜질 때만 620ms 로 길게 끌었던 것은 그늘이 판 끝에
 * 닿는 순간을 보여 주려던 것이고, 쓸어 들어오는 연출이 없어진 지금은 그 시간이 그냥
 * 늦게 켜지는 것이 된다(§69).
 */
const FADE_MS = 240;

/**
 * 그늘이 제일 깊을 때 나무를 얼마나 어둡게 하는가.
 *
 * 두 값인 이유는 판이 두 가지 밝기이기 때문이다. 평시에는 밝은 판 위라 이만큼이면
 * 충분한데, 회상 중에는 `.board-tint` 가 판을 통째로 낮춰 둬서(saturate 0.18 ·
 * brightness 0.55) 같은 세기가 낮아진 판에 묻힌다 — 브라우저에서 실제로 안 보였다.
 * 회상 쪽은 대신 그 한 칸 둘레에만 고이므로 판이 통째로 어두워지지 않는다.
 */
const PLAIN_DEPTH = 0.52;
const RECALL_DEPTH = 0.82;

interface Options {
  /** 판. `.board` 자신이고, 캔버스는 여기에 붙는다. */
  boardRef: React.RefObject<HTMLDivElement | null>;
  board: Board;
  /** 그늘을 그리는가. 평시엔 토글, 회상 중에는 켜진 채로 온다. */
  active: boolean;
  /** 그늘이 퍼져 나가는 칸(판 배열 인덱스). null이면 판 한가운데다. */
  from: number | null;
  /**
   * 사람이 잡은 쪽. 그늘은 이 쪽이 받고 있는 자리를 말한다(`相手の利き`).
   *
   * `black` 으로 박으면 안 된다. 後手로 두는 판에서 상대가 받는 자리를 그리게 되어,
   * 판이 정반대를 같은 그림으로 말한다.
   */
  me: Side;
  /**
   * 판을 뒤집어 그리는가(사람이 後手).
   *
   * 셰이더의 격자는 화면 왼쪽 위가 0이므로, 뒤집힌 판에서는 넘기는 배열도 같이 뒤집어야
   * 그늘이 제 칸에 앉는다.
   */
  flipped: boolean;
}

/**
 * 판의 자를 재서 표면에 넘긴다.
 *
 * CSS에서 베끼지 않고 판과 칸의 실제 크기에서 끌어낸다 — 베끼면 여백을 고치는 날
 * 셰이더의 격자만 옛 자리에 남고, 어느 쪽이 맞는지 화면을 띄워야만 알게 된다.
 */
function measure(boardEl: HTMLElement, surface: BoardSurface, layoutRef: React.RefObject<Layout | null>): void {
  const square = boardEl.firstElementChild;
  if (!(square instanceof HTMLElement)) return;
  const cell = square.getBoundingClientRect().width;
  // 캔버스는 판의 테두리 안쪽을 채운다. 테두리 폭을 여기서 빼는 대신 판의
  // clientWidth 를 쓴다 — 그것이 정확히 캔버스가 깔리는 상자다.
  const width = boardEl.clientWidth;
  const height = boardEl.clientHeight;
  if (cell < 1 || width < 1) return;

  layoutRef.current = {
    width,
    height,
    cell,
    // 판 안쪽 여백 하나 + 칸 사이 여덟 + 반대쪽 여백 하나 = 열이다.
    gap: (width - cell * BOARD_SIZE) / (BOARD_SIZE + 1),
  };
  surface.setLayout(layoutRef.current);
  surface.render();
}

/**
 * @returns WebGL이 실제로 잡혔는가. 안 잡히면 지금까지의 CSS 판 그대로 둔다 —
 * 판이 안 보이느니 나뭇결과 그늘이 없는 편이 낫다.
 */
export function useBoardSurface({ boardRef, board, active, from, me, flipped }: Options): boolean {
  const [ready, setReady] = useState(false);
  const surfaceRef = useRef<BoardSurface | null>(null);
  const layoutRef = useRef<Layout | null>(null);
  /** 지금 그늘이 얼마나 들어와 있나. 걷을 때 여기서 출발한다 — 1에서 시작하면
      켜진 적도 없는 그늘이 첫 렌더에 한 번 번쩍이고 사라진다. */
  const amountRef = useRef(0);

  // 캔버스는 손으로 붙인다. JSX로 두면 StrictMode의 두 번째 마운트가 같은 캔버스에
  // WebGL 컨텍스트를 또 잡는데, 캔버스 하나에 컨텍스트는 하나뿐이라 앞의 것을 정리하는
  // 순간 뒤의 것이 같이 죽는다. 붙는 자리는 맨 뒤다 — 첫 자식은 打 화살표가 칸 크기를
  // 재는 데 쓴다(`GameScreen.measureDrop`).
  //
  // three.js는 늦게 들여온다. 판이 처음 뜨는 데 필요한 것이 아니고(그 자리는 CSS 판이
  // 이미 채운다) 번들에서 제일 무거운 한 덩어리라, 첫 그림을 그것 때문에 기다리게 두지
  // 않는다. 들어오면 그때 표면이 켜지고, 못 들어오면 CSS 판 그대로 남는다.
  useEffect(() => {
    const boardEl = boardRef.current;
    if (!boardEl) return;

    let live = true;
    let started: { canvas: HTMLCanvasElement; surface: BoardSurface } | null = null;

    void import('@/libs/three/surface')
      .then(({ BoardSurface, paletteOf }) => {
        // 기다리는 사이에 판이 사라졌으면 캔버스를 붙이지 않는다 — 붙이고 나서
        // 지우면 StrictMode의 두 번 마운트에서 캔버스가 하나 남는다.
        if (!live) return;

        const canvas = document.createElement('canvas');
        canvas.className = 'board-surface';
        canvas.setAttribute('aria-hidden', 'true');
        boardEl.appendChild(canvas);

        try {
          started = { canvas, surface: new BoardSurface(canvas, paletteOf(boardEl)) };
        } catch {
          canvas.remove();
          return;
        }

        surfaceRef.current = started.surface;
        setReady(true);
      })
      .catch(() => {
        // 청크를 못 받았다. 판은 CSS 그대로 돌고 있으므로 여기서 할 일이 없다.
      });

    return () => {
      live = false;
      setReady(false);
      surfaceRef.current = null;
      layoutRef.current = null;
      started?.surface.dispose();
      started?.canvas.remove();
    };
  }, [boardRef]);

  // 판의 자. `--sq` 가 화면 폭을 따라 변하므로 재고, 바뀌면 다시 잰다.
  //
  // 그리기 전에 도는 효과여야 한다. `ready` 가 켜지는 순간 `data-surface` 가 붙어 칸이
  // 투명해지는데, 그 프레임에 캔버스가 아직 비어 있으면 판이 한 번 검게 번쩍인다.
  // `useEffect` 는 그린 뒤에 돌아서 그 한 프레임을 못 막는다 — 아래 그늘 쪽도 같다.
  useLayoutEffect(() => {
    const boardEl = boardRef.current;
    const surface = surfaceRef.current;
    if (!ready || !boardEl || !surface) return;

    measure(boardEl, surface, layoutRef);
    const observer = new ResizeObserver(() => measure(boardEl, surface, layoutRef));
    observer.observe(boardEl);
    return () => observer.disconnect();
  }, [boardRef, ready]);

  // 판이 바뀌면 그늘을 다시 센다. 회상에서는 장면마다 다시 센다 — 그 장면의 판에서
  // 나온 값이라 어느 장면에서도 그늘이 그 판의 사실이다.
  useLayoutEffect(() => {
    const surface = surfaceRef.current;
    if (!ready || !surface) return;
    const field = exposure(influenceOf(board), me);
    surface.setField(flipped ? field.toReversed() : field);
    surface.render();
  }, [board, me, flipped, ready]);

  // 밀려들고 물러난다. 움직임을 줄여 달라고 한 사용자에게는 그 자리에서 켜진다 —
  // 밀려드는 것은 분위기이고, 어느 칸이 깊은가가 정보다(詰み 게이지와 같은 기준).
  useEffect(() => {
    const surface = surfaceRef.current;
    const layout = layoutRef.current;
    if (!ready || !surface || !layout) return;

    const span = layout.cell + layout.gap;
    // 판 배열 인덱스를 화면 자리로 옮긴다. 뒤집힌 판에서는 180° 돌린 자리다.
    const at = from === null ? null : flipped ? 80 - from : from;
    const x = at === null ? layout.width / 2 : layout.gap + (at % BOARD_SIZE) * span + layout.cell / 2;
    const y = at === null ? layout.height / 2 : layout.gap + Math.floor(at / BOARD_SIZE) * span + layout.cell / 2;

    // 회상에서는 판을 다 덮지 않는다. 그때 판은 이미 탈색되어 낮아져 있고(`.board-tint`),
    // 그 위에 판 전체를 어둡게 하면 낮아진 판과 구별이 안 되어 아무것도 안 보인다 —
    // 브라우저에서 그렇게 나왔다. 짚어야 할 것도 판 전체가 아니라 그 한 칸이다.
    // 그래서 물러진 수가 간 칸 둘레에만, 대신 더 깊게 고인다.
    const full = at === null ? Math.hypot(layout.width, layout.height) + 64 : span * 3;
    surface.setDepth(at === null ? PLAIN_DEPTH : RECALL_DEPTH);

    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      amountRef.current = active ? 1 : 0;
      surface.setAmount(amountRef.current, { x, y, radius: full });
      surface.render();
      return;
    }

    let frame = 0;
    const start = amountRef.current;
    const began = performance.now();
    const step = (now: number) => {
      const t = Math.min((now - began) / FADE_MS, 1);
      // 켜질 때도 꺼질 때도 한꺼번에 옅어지고 짙어진다. 반지름을 0에서 키워 쓸어
      // 들어오게 하면 판 한가운데에서 퍼지는 고리가 정보로 읽힌다 — 그늘이 말하는 것은
      // 「어느 칸이 깊은가」뿐이고 퍼지는 순서는 아무 뜻도 없다(journal §69).
      amountRef.current = active ? start + (1 - start) * t : start * (1 - t);
      surface.setAmount(amountRef.current, { x, y, radius: full });
      surface.render();
      if (t < 1) frame = requestAnimationFrame(step);
    };
    frame = requestAnimationFrame(step);
    return () => cancelAnimationFrame(frame);
  }, [active, from, flipped, ready]);

  return ready;
}
