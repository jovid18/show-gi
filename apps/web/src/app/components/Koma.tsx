import { kanjiOf, type Side } from '@/models/piece';
import { isPromoted, mobilityOf, type GridDirection, type JumpDirection } from '@/models/mobility';

/**
 * 駒 하나. 판 위와 駒台가 같은 것을 쓴다.
 *
 * **성한 駒는 붉은 글자**이고, **움직임이 표식으로 새겨져 있다** — 한 칸 가는 곳은 점,
 * 쭉 가는 곳은 화살표, 뛰어넘는 곳은 점선. 실물 초심자용 駒가 하는 것과 같고,
 * 「香車가 어떻게 가더라」를 외우지 않아도 되게 하는 것이 이 앱이 하려는 일이다.
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

/** 화살표가 향하는 각도. `n` 이 0이고 시계 방향이다. */
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
 * 桂가 뛰는 두 자리와 **거기까지 가는 길.**
 *
 * 두 칸 앞의 좌우는 격자 밖이다. 그렇다고 `ne`·`nw` 자리에 점을 찍으면 대각선 한 칸으로
 * 읽혀 銀과 같은 그림이 된다 — 규칙을 틀리게 가르친다. 실제로 그렇게 그려져 있었다.
 *
 * **도착 칸은 다른 駒와 똑같은 점이다.** 「점 = 그 칸」이라는 어휘를 桂에서만 버릴 이유가
 * 없다. 격자보다 바깥에 놓일 뿐이다.
 *
 * 다른 것은 **길이 함께 그려진다**는 점이다. 잔 점이 駒에서 그 점까지 이어지고, 굵기가
 * 도착점의 1/3이라 「여기가 도착, 저건 지나가는 길」로 읽힌다. **길이 끊겨 있는 것**이
 * 사이 칸을 밟지 않는다는 뜻이고, 그것이 桂를 桂로 만드는 전부다.
 * 将棋ウォーズ가 桂에만 쓰는 표식이 이것이다.
 *
 * **뿌리는 하나다.** 줄기가 갈래에서 둘로 나뉘어 Y가 된다 — 「한 駒가 두 곳으로 뛴다」가
 * 그 모양에 실려 있다. 두 길을 각자 긋으면 V가 되고, 그러면 표식 둘이 우연히 같은 곳을
 * 향하는 것처럼 보인다. 그래서 줄기(`STEM`)는 갈래까지 **한 번만** 그린다.
 *
 * 자리는 **재서 잡았다.** 줄기의 시작은 글자 상자가 끝나는 곳(viewBox 30)이다 — 더 내리면
 * 글자가 첫 점을 덮는다. 도착점은 오각형 안에 반지름 5가 온전히 들어가는 한계 근처다.
 * 점 간격은 줄기와 갈래가 **똑같이 3**이라 한 줄기에서 갈라진 것으로 읽힌다.
 *
 * 이 자를 대려면 **letterbox를 빼먹으면 안 된다.** 駒는 0.8×0.84라 세로가 길고, SVG가
 * `meet` 로 맞추면서 viewBox 100칸이 가로에만 꽉 찬다 — `clip-path` 의 %는 駒 상자 기준이라
 * 오각형의 어깨가 viewBox 자로는 y=13.3에 있지 15가 아니다.
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
  /** 움직임 표식을 새길지. 駒台에서는 끈다 — 어디로 가는지가 아직 의미가 없다. */
  marks?: boolean;
}

export function Koma({ kind, side, marks = true }: KomaProps) {
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
}
