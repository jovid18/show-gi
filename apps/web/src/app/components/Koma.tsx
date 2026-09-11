import { memo } from 'react';

import { kanjiOf, type Side } from '@/models/piece';
import { isPromoted, mobilityOf, type GridDirection, type JumpDirection } from '@/models/mobility';

/**
 * 駒 하나. 판 위와 駒台가 같은 것을 쓴다.
 *
 * 성한 駒는 붉은 글자이고 움직임이 표식으로 새겨진다. 점·화살표·점선이 무엇을 뜻하는지는
 * journal §22.
 */

/**
 * 표식 자리. 100×100 좌표계의 정사각 3×3 격자이고 글자가 그 한가운데다.
 *
 * 세로 축을 살짝 내려 잡은 것은 오각형이 위에서 좁아지기 때문이다. 간격은 두 축이 같다.
 */
const COL = { west: 19, center: 50, east: 81 };
const ROW = { north: 22, middle: 53, south: 84 };

const SPOT: Record<GridDirection, [number, number]> = {
  n: [COL.center, ROW.north],
  ne: [COL.east, ROW.north],
  e: [COL.east, ROW.middle],
  se: [COL.east, ROW.south],
  s: [COL.center, ROW.south],
  sw: [COL.west, ROW.south],
  w: [COL.west, ROW.middle],
  nw: [COL.west, ROW.north],
};

/** 화살표가 향하는 각도. `n` 이 0이고 시계 방향이다. 표식이 駒와 함께 도니 後手도 이 값이다. */
const ANGLE: Record<GridDirection, number> = {
  n: 0,
  ne: 45,
  e: 90,
  se: 135,
  s: 180,
  sw: 225,
  w: 270,
  nw: 315,
};

/**
 * 桂가 뛰는 두 자리와 거기까지 가는 길. 도착 칸은 다른 駒와 똑같은 점이고, 격자보다
 * 바깥에 놓일 뿐이다.
 *
 * 자리는 브라우저에서 재서 잡았다. 고칠 때 무엇에 물리는지는 journal §22.
 */
const STEM: [number, number, number, number] = [50, 22, 50, 30.2];

const JUMP: Record<JumpDirection, { spot: [number, number]; arm: [number, number, number, number] }> = {
  nne: { spot: [68, 13], arm: [53.58, 20.21, 61, 16.5] },
  nnw: { spot: [32, 13], arm: [46.42, 20.21, 39, 16.5] },
};

interface KomaProps {
  /** 승격을 포함한 종류. `P`, `+R` … */
  kind: string;
  side: Side;
  /** 움직임 표식을 새길지. 駒台에서는 끈다. 어디로 가는지가 아직 의미가 없다. */
  marks?: boolean;
}

/**
 * `memo` 다. 판이 한 手 넘어가면 81칸이 전부 다시 렌더되는데 실제로 달라지는 칸은 두세
 * 개다. 나머지 40개쯤에서 `mobilityOf` 와 표식 SVG 를 다시 만들면 手数 넘기기에 80ms 가
 * 든다.
 */
export const Koma = memo(function Koma({ kind, side, marks = true }: KomaProps) {
  const mobility = marks ? mobilityOf(kind) : [];

  return (
    <span className="koma" data-side={side}>
      {mobility.length > 0 && (
        <svg className="koma-marks" viewBox="0 0 100 100" aria-hidden="true">
          {/* 두 갈래가 나눠 쓰는 줄기. 방향마다 그리면 겹쳐서 그 자리만 진해진다 */}
          {mobility.some((mark) => mark.reach === 'jump') && (
            <line className="koma-trail" x1={STEM[0]} y1={STEM[1]} x2={STEM[2]} y2={STEM[3]} />
          )}

          {mobility.map((mark) => {
            if (mark.reach === 'jump') {
              const { spot, arm } = JUMP[mark.direction];
              return (
                <g key={mark.direction}>
                  <line className="koma-trail" x1={arm[0]} y1={arm[1]} x2={arm[2]} y2={arm[3]} />
                  <circle className="koma-mark" cx={spot[0]} cy={spot[1]} r="5" />
                </g>
              );
            }

            const [x, y] = SPOT[mark.direction];
            // 화살표는 점과 같은 무게로 보여야 한다. 크게 그리면 글자를 덮는다.
            return mark.reach === 'slide' ? (
              <path
                key={mark.direction}
                className="koma-mark"
                d="M0,-6.5 L5,4 L-5,4 Z"
                transform={`translate(${x} ${y}) rotate(${ANGLE[mark.direction]})`}
              />
            ) : (
              <circle key={mark.direction} className="koma-mark" cx={x} cy={y} r="5" />
            );
          })}
        </svg>
      )}

      <span className="koma-kanji" data-promoted={isPromoted(kind) || undefined}>
        {kanjiOf(kind)}
      </span>
    </span>
  );
});
