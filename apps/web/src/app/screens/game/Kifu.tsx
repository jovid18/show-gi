import { useEffect, useRef, useState } from 'react';

import type { KifuMove } from '@/protocol/game';
import type { MatchMove } from '@/protocol/match';

interface KifuProps {
  /**
   * 두 어휘를 다 받는다. 엔진 대국은 `human`/`engine` 이고 대인전은 `you`/`opponent` 인데,
   * 여기서 하는 일은 그 값을 `data-by` 에 얹는 것뿐이다. 색은 CSS 가 네 값 다 안다.
   */
  moves: readonly (KifuMove | MatchMove)[];
}

/**
 * 棋譜. 지면의 棋譜用紙처럼 번호와 표기를 罫線 위에 얹는다.
 *
 * 표기는 서버가 만든 것을 그대로 쓴다. 클라이언트가 USI에서 다시 만들면 두 벌이 되고,
 * 어긋났을 때 어느 쪽이 맞는지 알 수 없다.
 */
export function Kifu({ moves }: KifuProps) {
  const listRef = useRef<HTMLOListElement>(null);
  const [copied, setCopied] = useState(false);

  /**
   * 새 수가 오면 목록을 끝까지 내린다.
   *
   * `scrollIntoView` 를 쓰지 않는다. 그쪽은 목록이 아직 넘치지 않으면 페이지를 스크롤하고,
   * 좁은 화면에서는 棋譜가 판 아래 화면 밖이라 매 수마다 판이 시야에서 사라진다. 목록의
   * `scrollTop` 을 직접 움직이면 페이지는 건드리지 않는다.
   */
  useEffect(() => {
    const list = listRef.current;
    if (list) list.scrollTop = list.scrollHeight;
  }, [moves.length]);

  /**
   * 棋譜를 그대로 클립보드에 넣는다.
   *
   * 개발 도구다. 이 문자열 하나로 국면이 복원되므로(서버가 합법수마다 표기를 돌려 USI로
   * 되돌린다) 사람이 남긴 보고를 그대로 조사에 넣을 수 있다. 한 번 84수를 손으로 옮겨야 했다.
   *
   * 되돌리는 쪽이 표기만 있으면 되므로 번호를 빼고 공백으로 잇는다.
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
        <ol className="kifu-list" ref={listRef}>
          {moves.map((move, i) => (
            <li key={`${i}-${move.usi}`} className="kifu-row" data-by={move.by}>
              <span className="kifu-number">{i + 1}</span>
              <span className="kifu-move">{move.ja}</span>
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}
