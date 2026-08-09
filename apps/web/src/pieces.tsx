// 駒 디자인 검토용 페이지. `pnpm dev` 후 `/pieces.html`.
//
// **본 앱의 `index.css`를 그대로 쓴다.** 스타일을 여기 베껴 오면 그 순간 실물과 갈라지고,
// 검토한 것과 배포되는 것이 달라진다. 그래서 마크업도 판과 같은 클래스를 쓴다.
//
// 프로덕션 번들에는 안 들어간다 — vite 는 기본적으로 `index.html` 하나만 빌드한다.

import type { CSSProperties } from 'react';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { Koma } from './components/Koma';
import { nameOf, type Side } from './shogi/piece';

import './index.css';
import './pieces.css';

/** 성하지 않은 駒. 판에서 玉이 먼저 눈에 들어오는 순서로 둔다. */
const PLAIN = ['K', 'R', 'B', 'G', 'S', 'N', 'L', 'P'];
/** 성한 駒. 玉은 성하지 않으므로 하나 적다. */
const PROMOTED = ['+R', '+B', '+S', '+N', '+L', '+P'];

/** `--sq` 는 화면 폭에 따라 32~56px 사이에서 정해진다. 양 끝과 가운데를 함께 본다. */
const SIZES = [32, 44, 56];

interface CellProps {
  kind?: string;
  side?: Side;
  label: string;
  /** 판의 상태를 그대로 흉내낸다. 실제 `Board`가 붙이는 것과 같은 속성이다. */
  lit?: boolean;
  selected?: boolean;
  last?: 'from' | 'to';
  check?: boolean;
  blunder?: 'from' | 'to';
}

function Cell({ kind, side = 'black', label, lit, selected, last, check, blunder }: CellProps) {
  return (
    <figure className="specimen">
      <span
        className="square"
        data-lit={lit || undefined}
        data-occupied={kind ? true : undefined}
        data-selected={selected || undefined}
        data-last={last}
        data-check={check || undefined}
      >
        {kind && <Koma kind={kind} side={side} />}
        {blunder && <span className="blunder-mark" data-role={blunder} />}
      </span>
      <figcaption>{label}</figcaption>
    </figure>
  );
}

function Row({ kinds, side }: { kinds: string[]; side: Side }) {
  return (
    <div className="specimen-row">
      {kinds.map((kind) => (
        <Cell key={kind} kind={kind} side={side} label={nameOf(kind)} />
      ))}
    </div>
  );
}

function Pieces() {
  return (
    <div className="review">
      <header className="review-head">
        <h1>駒</h1>
        <p>本番の index.css をそのまま読み込んでいます。</p>
      </header>

      <section>
        <h2>先手</h2>
        <Row kinds={PLAIN} side="black" />
        <Row kinds={PROMOTED} side="black" />
      </section>

      <section>
        <h2>後手</h2>
        <p className="review-note">向きだけで先後を分けます。色は変えません。</p>
        <Row kinds={PLAIN} side="white" />
        <Row kinds={PROMOTED} side="white" />
      </section>

      <section>
        <h2>大きさ</h2>
        <p className="review-note">
          画面幅で 32〜56px の間を動きます。狭い画面では漢字の下限のほうが効きます。成った銀・桂・香は
          実物の駒と同じ一文字（全・圭・杏）です。
        </p>
        {SIZES.map((size) => (
          <div key={size} className="review-size" style={{ '--sq': `${size}px` } as CSSProperties}>
            <span className="review-size-label">{size}px</span>
            <div className="specimen-row">
              {['K', 'R', 'G', 'P', '+R', '+S', '+N', '+L'].map((kind) => (
                <Cell key={kind} kind={kind} label={nameOf(kind)} />
              ))}
            </div>
          </div>
        ))}
      </section>

      <section>
        <h2>盤の上の状態</h2>
        <p className="review-note">光は「いまできること・いま起きていること」、色は「たったいま起きたこと」です。</p>
        <div className="specimen-row">
          <Cell kind="S" label="通常" />
          <Cell kind="S" label="選んだ駒" selected />
          <Cell label="着手候補" lit />
          <Cell kind="S" side="white" label="取る手" lit />
          <Cell kind="S" label="直前の手" last="to" />
          <Cell label="その出発点" last="from" />
          <Cell kind="K" label="王手" check />
          <Cell kind="B" label="戻された手" blunder="from" />
          <Cell label="その行き先" blunder="to" />
        </div>
      </section>

      <section>
        <h2>駒台</h2>
        <div className="hand">
          <span className="hand-label">あなた</span>
          <div className="hand-pieces">
            {['R', 'B', 'G', 'S', 'N', 'L', 'P'].map((kind, i) => (
              <button key={kind} type="button" className="hand-piece">
                <Koma kind={kind} side="black" marks={false} />
                {i > 3 && <span className="hand-count">{i}</span>}
              </button>
            ))}
          </div>
        </div>
      </section>
    </div>
  );
}

const root = document.getElementById('root');
if (!root) throw new Error('#root가 없다. pieces.html을 확인할 것');

createRoot(root).render(
  <StrictMode>
    <Pieces />
  </StrictMode>,
);
