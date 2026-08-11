import { useMemo } from 'react';
import { CartesianGrid, Line, LineChart, ReferenceLine, ResponsiveContainer, XAxis, YAxis } from 'recharts';

import type { GameDetail } from '@/protocol/review';
import type { WhatIf } from '@/hooks/useWhatIf';

/**
 * 한 판의 평가치 궤적. **이 그림이 곧 이동 장치다.**
 *
 * 세 가지가 한 자리에 겹친다 — 실제로 둔 판(검정), 지금 둬 보고 있는 분기(초록), 그리고
 * 물러진 수가 있던 자리(빨강). 「어디서 무너졌나」가 이 제품의 주장인데, 그 답을 목록 세 개로
 * 나눠 읽게 하는 대신 한 장으로 보여주고 **거기를 눌러 돌아가게 한다.**
 *
 * **점을 누르면 그 手数로 간다.** 手数를 고르는 길이 이것과 기보 목록 둘인데, 이쪽은 「어디가
 * 나빴나」로 고르고 저쪽은 「몇 手目」으로 고른다 — 되짚는 사람이 쓰는 것은 대개 앞쪽이다.
 */
interface EvalGraphProps {
  game: GameDetail;
  /** 지금 보고 있는 手数. 그 자리가 굵은 점으로 선다. */
  ply: number;
  /** 지금 서 있는 분기. 없으면 초록선이 없다. */
  whatif: WhatIf;
  onPick: (ply: number) => void;
}

/**
 * 세로축을 여기서 자른다.
 *
 * **詰み은 ±30000으로 온다.** 그 값을 그대로 그리면 나머지 100수가 0 근처에 눌려 한 줄이
 * 되고, 정작 「어디서 기울었나」가 안 보인다. 銀 하나가 대략 500이라, 이 폭이면 駒 하나
 * 손해가 눈에 보이는 크기가 된다.
 *
 * **자른 것은 자른 것으로 보여야 한다** — 위아래 끝에 닿은 선은 「그 이상」이라는 뜻이다.
 */
const CLAMP = 1200;

/**
 * 색을 새로 안 만들었다.
 *
 * 이 앱은 판 위에서 색을 넷만 쓰고 나머지는 빛의 세기로 구분한다(index.css). 그림에도 그
 * 넷 안에서 고른다 — 초록은 이미 「보여주는 수순」이고(`--ray`), 빨강은 「지금 위험한 것」이다
 * (`--ray-check`). 검정은 색이 아니라 글자색(`--fg`)이라 「실제로 벌어진 것」에 맞다.
 */

const clamp = (cp: number): number => Math.max(-CLAMP, Math.min(CLAMP, cp));

/** 手数 눈금 간격. */
const TICK_EVERY = 20;

/**
 * 세로축을 무엇으로 그리나. **눈으로 보고 정하려고 둔 손잡이이고, 아직 결정이 아니다.**
 *
 * `cp` — 실측 그대로. ±CLAMP 에서 자르므로 우세 구간이 천장에 붙는다.
 * `winrate` — 로지스틱으로 0~1. 자를 필요가 없고 포화가 뜻 그대로 보이지만, **`K` 에 매달린다.**
 *
 * `K` 를 여기 둔 것이 이 손잡이의 문제다 — 서버의 판정이 쓰는 값과 **두 벌**이 되고
 * (`intervene.K`), 그 값은 §39가 「기록으로는 못 정한다」로 열어 둔 미정 상수다. 승률로
 * 가기로 정하면 **서버가 계산해 내려주는 쪽으로 옮겨야 한다** — 그때 이 상수는 지운다.
 */
const AXIS: 'cp' | 'winrate' = 'winrate';

/** 서버의 `intervene.K` 와 같은 값. **두 벌이라 여기 남겨 두면 안 되는 값이다**(위). */
const K = 600;

/** cp → 승률. 서버의 `intervene.WinRate` 와 같은 식이다. */
const winRate = (cp: number): number => 1 / (1 + Math.exp(-cp / K));

/** 그 手数의 세로 위치. 축을 바꾸는 자리는 여기 하나다. */
const valueOf = (cp: number): number => (AXIS === 'winrate' ? winRate(cp) : clamp(cp));

const Y_DOMAIN: [number, number] = AXIS === 'winrate' ? [0, 1] : [-CLAMP, CLAMP];
const Y_TICKS = AXIS === 'winrate' ? [0, 0.25, 0.5, 0.75, 1] : [-CLAMP, -600, 0, 600, CLAMP];
/** 호각. 승률에서는 0.5이고 cp에서는 0이다. */
const Y_EVEN = AXIS === 'winrate' ? 0.5 : 0;

const yLabel = (v: number): string => (AXIS === 'winrate' ? `${Math.round(v * 100)}%` : String(v));

interface Point {
  ply: number;
  /** 실제로 둔 판. 평가치가 안 남은 手数는 `null` — 없는 값을 0으로 채우지 않는다. */
  main: number | null;
  /** 지금 둬 보고 있는 분기. 갈라지기 전 手数는 `null`이라 선이 그 자리에서 시작한다. */
  branch: number | null;
}

export function EvalGraph({ game, ply, whatif, onPick }: EvalGraphProps) {
  const { node, evalOf } = whatif;

  const data = useMemo<Point[]>(() => {
    const main = new Map<number, number>();
    for (const m of game.moves) {
      if (m.evalCp !== undefined) main.set(m.ply, valueOf(m.evalCp));
    }

    /**
     * 분기의 값. **지나온 자리만 있다** — 아직 안 가 본 곳은 재지 않았으므로 없다(`evalOf`).
     *
     * 뿌리(`basePly`)에는 검은선의 값을 그대로 넣는다. 그러면 초록선이 검은선에서 갈라져
     * 나오는 그림이 되고, 그게 사실이다 — 같은 국면에서 다른 수를 둔 것이다.
     */
    const branch = new Map<number, number>();
    if (node?.line.length) {
      const root = main.get(node.basePly);
      if (root !== undefined) branch.set(node.basePly, root);
      node.line.forEach((move, i) => {
        const at = evalOf(i + 1);
        if (at?.cp !== undefined) branch.set(move.ply, valueOf(at.cp));
      });
    }

    const plies = new Set<number>([...main.keys(), ...branch.keys()]);
    return [...plies]
      .toSorted((a, b) => a - b)
      .map((p) => ({ ply: p, main: main.get(p) ?? null, branch: branch.get(p) ?? null }));
  }, [game.moves, node, evalOf]);

  /**
   * 물러진 수가 있던 자리.
   *
   * **길이가 없다.** 사람이 그 수로 얼마를 잃었는지는 기록에 안 남아 있다 — `interventions`
   * 에는 Δ승률 하나뿐이고 cp가 없어서, 아래로 얼마나 그을지를 정직하게 정할 수가 없다
   * (06-status.md §39 ⑥). 그래서 「여기서 막혔다」까지만 말하는 점으로 둔다. 깊이까지 그리려면
   * `interventions` 에 `best_cp`·`after_cp` 두 칸이 필요하다.
   *
   * 자리는 **물러진 手数의 한 수 앞**이다. 그 수는 확정되지 않았으므로 사람이 서 있던 국면이
   * 거기다(protocol/review.ts).
   */
  const stops = useMemo(() => {
    const at = new Map<number, number>();
    for (const iv of game.interventions) {
      const x = Math.max(0, iv.ply - 1);
      at.set(x, (at.get(x) ?? 0) + 1);
    }
    return at;
  }, [game.interventions]);

  /** 20手마다 하나. `0`(開始局面)에서 시작한다. */
  const ticks = useMemo(() => {
    const out: number[] = [];
    for (let p = 0; p <= game.moves.length; p += TICK_EVERY) out.push(p);
    return out;
  }, [game.moves.length]);

  // 평가치가 한 수도 안 남은 판이 있다(`eval_cp` 는 뒤에 붙은 컬럼이다). 그때는 빈 상자를
  // 그리지 않고 왜 없는지를 말한다 — 빈 그래프는 「호각이었다」로 읽힌다.
  if (data.every((p) => p.main === null)) {
    return <p className="review-empty">この対局には評価値が残っていません。</p>;
  }

  return (
    <div className="review-graph">
      <ResponsiveContainer width="100%" height={180}>
        <LineChart
          data={data}
          margin={{ top: 6, right: 8, bottom: 0, left: 8 }}
          // 그림 아무 데나 눌러도 제일 가까운 手数로 간다. 점만 누르게 하면 109수 판에서
          // 점 하나가 3px이고, 그건 누를 수 없는 크기다.
          onClick={(state) => {
            const label = state?.activeLabel;
            if (typeof label === 'number') onPick(label);
          }}
        >
          <CartesianGrid stroke="rgb(255 255 255 / 0.06)" vertical={false} />
          {/* **手数를 읽을 수 있어야 한다.** 빨간 점이 「몇 手째」인지 모르면 이 그림으로
              어디를 볼지 고를 수 없다 — 눌러서 이동하는 장치인데 반쪽이 된다.

              20씩인 것은 20手가 쇼기에서 한 국면 덩어리(序盤·囲い가 짜이는 구간)에 가깝고,
              167手 판에서 눈금이 여덟 개쯤으로 앉아 서로 안 겹치기 때문이다. 마지막 手数는
              눈금으로 안 넣는다 — 총 手数는 아래 바의 제목(`棋譜 167手`)이 이미 든다. */}
          <XAxis
            dataKey="ply"
            type="number"
            domain={[0, game.moves.length]}
            ticks={ticks}
            tick={{ fill: 'var(--muted)', fontSize: 10 }}
            tickSize={3}
            tickLine={{ stroke: 'rgb(255 255 255 / 0.18)' }}
            axisLine={{ stroke: 'rgb(255 255 255 / 0.18)' }}
            height={16}
          />
          {/* **위쪽이 先手다** — `eval_cp` 가 先手 관점으로 저장된다(06-status.md §26).
              後手로 둔 판에서는 위가 상대가 되므로, 이 축은 아직 「나」를 말하지 못한다.
              그 자리는 서버가 관점을 뒤집어 주는 것으로 따로 닫는다. */}
          <YAxis
            domain={Y_DOMAIN}
            ticks={Y_TICKS}
            tickFormatter={yLabel}
            tick={{ fill: 'var(--muted)', fontSize: 10 }}
            tickSize={3}
            tickLine={{ stroke: 'rgb(255 255 255 / 0.18)' }}
            axisLine={false}
            width={30}
          />
          {/* 호각. 이 선을 넘나드는 것이 곧 「누가 이기고 있었나」가 바뀐 자리다 */}
          <ReferenceLine y={Y_EVEN} stroke="rgb(255 255 255 / 0.18)" />

          <Line
            type="linear"
            dataKey="main"
            stroke="var(--fg)"
            strokeWidth={1.5}
            // 값이 빠진 手数에서 선을 잇지 않는다. 이으면 없는 값을 지어낸 것이 된다.
            connectNulls={false}
            isAnimationActive={false}
            dot={(props) => <MainDot {...props} ply={ply} stops={stops} />}
            activeDot={false}
          />

          <Line
            type="linear"
            dataKey="branch"
            stroke="rgb(var(--ray))"
            strokeWidth={2}
            connectNulls={false}
            isAnimationActive={false}
            dot={false}
            activeDot={false}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}

/**
 * 검은선의 점 하나.
 *
 * 대부분은 안 그린다 — 109개를 다 찍으면 선이 점선이 된다. 그릴 이유가 있는 자리만 찍는다:
 * **지금 보고 있는 곳**과 **물러진 수가 있던 곳**이다.
 */
function MainDot(props: { cx?: number; cy?: number; payload?: Point; ply: number; stops: Map<number, number> }) {
  const { cx, cy, payload, ply, stops } = props;
  if (cx === undefined || cy === undefined || !payload) return null;

  const here = payload.ply === ply;
  const stopped = stops.get(payload.ply);
  if (!here && !stopped) return null;

  return (
    <g>
      {stopped && <circle cx={cx} cy={cy} r={3.5} fill="rgb(var(--ray-check))" />}
      {here && <circle cx={cx} cy={cy} r={stopped ? 6 : 4} fill="none" stroke="var(--fg)" strokeWidth={1.5} />}
    </g>
  );
}
