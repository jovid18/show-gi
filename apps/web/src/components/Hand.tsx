import { Koma } from './Koma';
import { HAND_ORDER, nameOf, type Side } from '@/shogi/piece';

interface HandProps {
  side: Side;
  /** kind → 개수 */
  pieces: Record<string, number>;
  label: string;
  /** 지금 고른 持ち駒. `P*` 모양. */
  selected: string | null;
  /** 둘 수 있는 持ち駒의 출발점 집합. `P*` 모양. */
  playable: ReadonlySet<string>;
  onPick: (origin: string) => void;
}

/**
 * 駒台. 실제 대국에서 잡은 말이 놓이는 받침이다.
 *
 * 빈 받침도 자리를 지킨다 — 말이 늘고 줄 때마다 판이 위아래로 흔들리면
 * 초심자는 무엇이 변했는지 못 본다.
 */
export function Hand({ side, pieces, label, selected, playable, onPick }: HandProps) {
  const held = HAND_ORDER.filter((kind) => (pieces[kind] ?? 0) > 0);

  return (
    <div className="hand" data-side={side}>
      <span className="hand-label">{label}</span>

      <div className="hand-pieces">
        {held.length === 0 && <span className="hand-empty">なし</span>}

        {held.map((kind) => {
          const origin = `${kind}*`;
          const count = pieces[kind] ?? 0;
          const canDrop = playable.has(origin);

          return (
            <button
              key={kind}
              type="button"
              className="hand-piece"
              data-selected={selected === origin || undefined}
              disabled={!canDrop}
              aria-label={`${nameOf(kind)} ${count}枚`}
              onClick={() => onPick(origin)}
            >
              {/* 駒台에서는 움직임 표식을 끈다. 아직 판 위가 아니라 방향이 뜻을 갖지 않고,
                  持ち駒는 성하지 않은 것뿐이라 붉은 글자도 나오지 않는다. */}
              <Koma kind={kind} side={side} marks={false} />
              {count > 1 && <span className="hand-count">{count}</span>}
            </button>
          );
        })}
      </div>
    </div>
  );
}
