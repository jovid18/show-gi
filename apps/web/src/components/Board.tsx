import type { Board as BoardModel } from '@/shogi/sfen';
import { isWideKanji, kanjiOf } from '@/shogi/piece';
import { fromIndex, toUsi } from '@/shogi/square';

const FILES = [9, 8, 7, 6, 5, 4, 3, 2, 1];
const RANKS = ['一', '二', '三', '四', '五', '六', '七', '八', '九'];

interface BoardProps {
  board: BoardModel;
  /** 지금 빛나는 칸(착수 가능). USI 좌표. */
  lit: ReadonlySet<string>;
  /** 고른 기물이 서 있는 칸. */
  selected: string | null;
  /** 직전 수가 도착한 칸. */
  lastMove: string | null;
  /** 王手를 받고 있는 玉의 칸. */
  checked: string | null;
  interactive: boolean;
  onSquare: (usi: string) => void;
}

export function Board({ board, lit, selected, lastMove, checked, interactive, onSquare }: BoardProps) {
  return (
    <div className="board-frame">
      <div className="board-files" aria-hidden="true">
        {FILES.map((f) => (
          <span key={f}>{f}</span>
        ))}
      </div>

      <div className="board">
        {board.squares.map((piece, index) => {
          const usi = toUsi(fromIndex(index));
          const label = `${FILES[index % 9]}${RANKS[Math.floor(index / 9)]}`;

          return (
            <button
              key={usi}
              type="button"
              className="square"
              data-lit={lit.has(usi) || undefined}
              data-occupied={piece ? true : undefined}
              data-selected={selected === usi || undefined}
              data-last={lastMove === usi || undefined}
              data-check={checked === usi || undefined}
              disabled={!interactive}
              aria-label={piece ? `${label} ${kanjiOf(piece.kind)}` : label}
              onClick={() => onSquare(usi)}
            >
              {piece && (
                <span className="koma" data-side={piece.side}>
                  <span className="koma-kanji" data-wide={isWideKanji(piece.kind) || undefined}>
                    {kanjiOf(piece.kind)}
                  </span>
                </span>
              )}
            </button>
          );
        })}
      </div>

      <div className="board-ranks" aria-hidden="true">
        {RANKS.map((r) => (
          <span key={r}>{r}</span>
        ))}
      </div>
    </div>
  );
}
