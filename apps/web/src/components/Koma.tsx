import { kanjiOf, type Side } from '@/shogi/piece';
import { isPromoted, mobilityOf, type Direction } from '@/shogi/mobility';

/**
 * 駒 하나. 판 위·駒台·검토 페이지가 전부 이것을 쓴다.
 *
 * **성한 駒는 붉은 글자**이고, **움직임이 표식으로 새겨져 있다** — 한 칸 가는 곳은 점,
 * 쭉 가는 곳은 화살표. 실물 초심자용 駒가 하는 것과 같고, 「香車가 어떻게 가더라」를
 * 외우지 않아도 되게 하는 것이 이 앱이 하려는 일이다.
 *
 * 표식은 駒와 함께 돈다 — 後手 駒는 통째로 180° 뒤집히므로 화살표도 상대 쪽을 가리킨다.
 */

/**
 * 표식 자리. 100×100 좌표계의 **정사각 3×3 격자**이고 글자가 그 한가운데다.
 *
 * 방향마다 거리를 달리하면(둥글게 배치하면) 玉처럼 여덟 방향이 다 있는 駒에서
 * 점들이 원으로 보여서 **어느 칸을 가리키는지가 흐려진다.** 격자로 두면 점 하나가
 * 판의 한 칸에 그대로 대응한다 — 玉과 銀을 나란히 놓으면 그 차이가 바로 보인다.
 *
 * 위아래로 살짝 내려 잡은 것은 오각형이 위에서 좁아지기 때문이다. 간격은 두 축이 같다.
 */
const COL = { west: 19, center: 50, east: 81 };
const ROW = { north: 22, middle: 53, south: 84 };

const SPOT: Record<Direction, [number, number]> = {
  n: [COL.center, ROW.north],
  ne: [COL.east, ROW.north],
  e: [COL.east, ROW.middle],
  se: [COL.east, ROW.south],
  s: [COL.center, ROW.south],
  sw: [COL.west, ROW.south],
  w: [COL.west, ROW.middle],
  nw: [COL.west, ROW.north],
  // 桂가 뛰는 두 자리. 격자 밖이지만 「앞의 좌우」라는 뜻은 같은 칸에 실린다.
  nne: [COL.east, ROW.north],
  nnw: [COL.west, ROW.north],
};

/** 화살표가 향하는 각도. `n` 이 0이고 시계 방향이다. */
const ANGLE: Record<Direction, number> = {
  n: 0,
  ne: 45,
  e: 90,
  se: 135,
  s: 180,
  sw: 225,
  w: 270,
  nw: 315,
  nne: 0,
  nnw: 0,
};

interface KomaProps {
  /** 승격을 포함한 종류. `P`, `+R` … */
  kind: string;
  side: Side;
  /** 움직임 표식을 새길지. 駒台에서는 끈다 — 어디로 가는지가 아직 의미가 없다. */
  marks?: boolean;
}

export function Koma({ kind, side, marks = true }: KomaProps) {
  const mobility = marks ? mobilityOf(kind) : [];

  return (
    <span className="koma" data-side={side}>
      {mobility.length > 0 && (
        <svg className="koma-marks" viewBox="0 0 100 100" aria-hidden="true">
          {mobility.map(({ direction, slide }) => {
            const [x, y] = SPOT[direction];
            // 화살표는 점과 같은 무게로 보여야 한다. 크게 그리면 글자를 덮는다.
            return slide ? (
              <path
                key={direction}
                className="koma-mark"
                d="M0,-6.5 L5,4 L-5,4 Z"
                transform={`translate(${x} ${y}) rotate(${ANGLE[direction]})`}
              />
            ) : (
              <circle key={direction} className="koma-mark" cx={x} cy={y} r="5" />
            );
          })}
        </svg>
      )}

      <span className="koma-kanji" data-promoted={isPromoted(kind) || undefined}>
        {kanjiOf(kind)}
      </span>
    </span>
  );
}
