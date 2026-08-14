import { useEffect, useState } from 'react';

import type { GameSetup } from '@/hooks/useGame';
import type { Color } from '@/protocol/game';
import { fetchOpenings, type Opening } from '@/protocol/openings';
import { ROUTE_GUIDE } from '@/routes/const';

/**
 * 대국을 시작하기 전에 고르는 화면.
 *
 * **여기서 고르기 전에는 서버에 붙지 않는다**(`useGame`). 미리 붙으면 그 순간 판이 하나
 * 열려 기록에 남고, 아무것도 고르지 않은 채로 先手 평수 대국이 시작된다.
 *
 * 고를 것을 둘로 묶어 뒀다 — **어느 쪽을 잡나**와 **상대가 무엇을 하나**다. 난이도는 여기
 * 없다. 그건 두는 동안 상대가 스스로 맞춘다(docs/06-status.md §47).
 */

const COLORS: { value: Color; label: string; note: string }[] = [
  { value: 'b', label: '先手', note: '自分から先に指します' },
  { value: 'w', label: '後手', note: '相手の出方を見てから指します' },
];

interface SetupProps {
  /** 지난 판의 설정. **한 판 두고 온 사람은 여기서 시작한다** — 같은 조건으로 또 두는
   *  것이 흔하고, 매번 처음부터 고르게 하면 「もう一局」이 두 번 더 눌러야 하는 일이 된다. */
  initial: GameSetup | null;
  onStart: (setup: GameSetup) => void;
}

export function Setup({ initial, onStart }: SetupProps) {
  const [color, setColor] = useState<Color>(initial?.color ?? 'b');
  const [opening, setOpening] = useState<string | null>(initial?.opening ?? null);
  const [openings, setOpenings] = useState<Opening[]>([]);

  // 목록을 못 받아도 화면은 선다 — 「おまかせ」 하나로 대국은 시작할 수 있다(fetchOpenings).
  useEffect(() => {
    const ac = new AbortController();
    void fetchOpenings(ac.signal).then(setOpenings);
    return () => ac.abort();
  }, []);

  return (
    <div className="setup">
      <h2 className="setup__head">対局のじゅんび</h2>

      <fieldset className="setup__group">
        <legend className="setup__legend">あなたの手番</legend>
        <div className="setup__choices">
          {COLORS.map((c) => (
            <button
              key={c.value}
              type="button"
              className="setup__choice"
              data-on={color === c.value || undefined}
              aria-pressed={color === c.value}
              onClick={() => setColor(c.value)}
            >
              <span className="setup__choice-name">{c.label}</span>
              <span className="setup__choice-note">{c.note}</span>
            </button>
          ))}
        </div>
      </fieldset>

      <fieldset className="setup__group">
        <legend className="setup__legend">相手の戦型</legend>
        <div className="setup__choices setup__choices--wrap">
          {/* **「おまかせ」가 기본이다.** 이름을 모르는 사람이 첫 화면에서 고민하게 만들지
              않는다 — 골라 보고 싶은 사람만 아래에서 고른다. */}
          <button
            type="button"
            className="setup__choice"
            data-on={opening === null || undefined}
            aria-pressed={opening === null}
            onClick={() => setOpening(null)}
          >
            <span className="setup__choice-name">おまかせ</span>
            <span className="setup__choice-note">相手が自由に指します</span>
          </button>

          {openings.map((o) => (
            <button
              key={o.id}
              type="button"
              className="setup__choice"
              data-on={opening === o.id || undefined}
              aria-pressed={opening === o.id}
              onClick={() => setOpening(o.id)}
            >
              <span className="setup__choice-name">{o.name}</span>
              <span className="setup__choice-note">{o.note}</span>
            </button>
          ))}
        </div>

        {/* 출처는 고른 것 하나만 보여준다. 넷을 늘어놓으면 고르는 자리가 각주로 덮인다. */}
        {opening !== null && <SourceLink openings={openings} id={opening} />}
      </fieldset>

      <p className="setup__caveat">
        {/* **진형은 초반뿐이라고 화면이 먼저 말한다.** 안 적어 두면 상대가 진형을 벗어나는
            순간이 고장으로 읽힌다 — 손을 놓는 조건은 book_opponent.go 에 있다. */}
        戦型を選ぶと、相手は序盤だけその形に組みます。駒がぶつかってからは自分で考えます。
      </p>

      <button type="button" className="btn btn--primary setup__start" onClick={() => onStart({ color, opening })}>
        対局をはじめる
      </button>

      {/* **헤더의 버튼만으로는 못 찾는다.** 처음 온 사람이 실제로 보는 화면은 여기 하나다.
          시작 버튼 **아래**에 두는 것이 요점 — 위에 두면 두러 온 사람을 먼저 붙잡는다.

          헤더와 **같이 새 탭이다**(App.tsx). 같은 곳이 자리에 따라 다르게 열리면 두 번째로
          누를 때 무엇이 일어날지 모르게 되고, 덤으로 여기서 고른 手番·戦型이 안 날아간다. */}
      <a className="setup__guide" href={ROUTE_GUIDE} target="_blank" rel="noreferrer noopener">
        はじめての方へ — このアプリの遊びかた
        <span aria-hidden="true"> ↗</span>
      </a>
    </div>
  );
}

function SourceLink({ openings, id }: { openings: Opening[]; id: string }) {
  const found = openings.find((o) => o.id === id);
  if (!found) return null;
  return (
    <p className="setup__source">
      <a href={found.source} target="_blank" rel="noreferrer noopener">
        {found.name}について
      </a>
    </p>
  );
}
