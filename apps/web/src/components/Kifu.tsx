import { useEffect, useRef, useState } from 'react';

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
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    endRef.current?.scrollIntoView({ block: 'nearest' });
  }, [moves.length]);

  /**
   * 棋譜를 그대로 클립보드에 넣는다.
   *
   * **개발 도구다.** 이 문자열 하나로 국면이 완전히 복원되므로(서버가 합법수마다
   * 표기를 돌려 USI로 되돌린다), 「이 국면에서 무엇을 둬도 물러졌다」 같은 보고를
   * 그대로 조사에 넣을 수 있다. 실제로 한 번 84수를 손으로 옮겨야 했다.
   *
   * 번호를 빼고 표기만 공백으로 잇는다 — 되돌리는 쪽이 필요로 하는 것이 그것뿐이다.
   */
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(moves.map((m) => m.ja).join(' '));
      setCopied(true);
      setTimeout(() => setCopied(false), 1600);
    } catch {
      // 클립보드는 권한과 보안 컨텍스트를 탄다. 실패해도 대국에는 영향이 없다.
      setCopied(false);
    }
  };

  return (
    <section className="kifu" aria-label="棋譜">
      <div className="kifu-head">
        <h2 className="panel-title">棋譜</h2>
        {moves.length > 0 && (
          <button type="button" className="kifu-copy" onClick={copy}>
            {copied ? 'コピーしました' : 'コピー'}
          </button>
        )}
      </div>

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
