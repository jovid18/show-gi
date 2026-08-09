import { useEffect, useRef } from 'react';

import type { KifuMove } from '@/game/protocol';

interface KifuProps {
  moves: readonly KifuMove[];
}

/**
 * 棋譜. 지면의 棋譜用紙처럼 번호와 표기를 罫線 위에 얹는다.
 *
 * 표기는 서버가 만든 것을 그대로 쓴다. 클라이언트가 USI에서 다시 만들면 두 벌이 되고,
 * 어긋났을 때 어느 쪽이 맞는지 알 수 없다.
 */
export function Kifu({ moves }: KifuProps) {
  const endRef = useRef<HTMLLIElement>(null);

  useEffect(() => {
    endRef.current?.scrollIntoView({ block: 'nearest' });
  }, [moves.length]);

  return (
    <section className="kifu" aria-label="棋譜">
      <h2 className="panel-title">棋譜</h2>

      {moves.length === 0 ? (
        <p className="kifu-empty">最初の一手を指してください。</p>
      ) : (
        <ol className="kifu-list">
          {moves.map((move, i) => (
            <li
              key={`${i}-${move.usi}`}
              className="kifu-row"
              data-by={move.by}
              ref={i === moves.length - 1 ? endRef : undefined}
            >
              <span className="kifu-number">{i + 1}</span>
              <span className="kifu-move">{move.ja}</span>
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}
