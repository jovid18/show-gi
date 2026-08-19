import { useEffect, useState } from 'react';

import type { GameSetup } from '@/hooks/useGame';
import { useViewer } from '@/hooks/useViewer';
import type { Color } from '@/protocol/game';
import { fetchHandicaps, type Handicap } from '@/protocol/handicaps';
import { createRoom, type SeatChoice } from '@/protocol/match';
import { fetchOpenings, type Opening } from '@/protocol/openings';
import { ROUTE_GUIDE } from '@/routes/const';
import { navigate } from '@/routes/router';

/**
 * 대국을 시작하기 전에 고르는 화면.
 *
 * **여기서 고르기 전에는 서버에 붙지 않는다**(`useGame`). 미리 붙으면 그 순간 판이 하나
 * 열려 기록에 남고, 아무것도 고르지 않은 채로 先手 평수 대국이 시작된다.
 *
 * 고를 것을 셋으로 묶어 뒀다 — **얼마나 접나**(手合割) · **어느 쪽을 잡나** · **상대가
 * 무엇을 하나**다. 난이도 눈금은 여기 없다. 그건 두는 동안 상대가 스스로 맞춘다(journal §47).
 *
 * **手合割이 맨 위이고, 고르면 아래 둘이 사라진다.** 駒落ち는 사람이 下手(先手)로 정해져
 * 있고 진형은 平手 수순이라 같이 못 쓴다 — 서버도 같은 순서로 덮으므로(`newSetup`) 화면이
 * 그 규칙을 되비추는 것이지 새로 정하는 것이 아니다.
 */

const COLORS: { value: Color; label: string; note: string }[] = [
  { value: 'b', label: '先手', note: '自分から先に指します' },
  { value: 'w', label: '後手', note: '相手の出方を見てから指します' },
];

/** 사람과 둘 때의 手番. **振り駒가 하나 더 있다** — 상대가 사람이면 그것이 원래 정하는 법이다. */
const SEATS: { value: SeatChoice; label: string; note: string }[] = [
  { value: 'r', label: '振り駒', note: '部屋をつくったときに決まります' },
  ...COLORS,
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
  const [handicap, setHandicap] = useState<string | null>(initial?.handicap ?? null);
  const [handicaps, setHandicaps] = useState<Handicap[]>([]);

  // 목록을 못 받아도 화면은 선다 — 「おまかせ」와 「平手」 하나로 대국은 시작할 수 있다
  // (fetchOpenings · fetchHandicaps).
  useEffect(() => {
    const ac = new AbortController();
    void fetchOpenings(ac.signal).then(setOpenings);
    void fetchHandicaps(ac.signal).then(setHandicaps);
    return () => ac.abort();
  }, []);

  return (
    <div className="setup">
      <h2 className="setup__head">対局のじゅんび</h2>

      <fieldset className="setup__group">
        <legend className="setup__legend">手合割</legend>
        <div className="setup__choices setup__choices--wrap">
          {/* **「平手」가 기본이고 서버 목록에 없다.** 접지 않는 것은 물어볼 것이 아니라
              기본값이라, 진형의 「おまかせ」와 같은 자리에서 화면이 직접 그린다. */}
          <button
            type="button"
            className="setup__choice"
            data-on={handicap === null || undefined}
            aria-pressed={handicap === null}
            onClick={() => setHandicap(null)}
          >
            <span className="setup__choice-name">平手</span>
            <span className="setup__choice-note">駒を落とさずに指します</span>
          </button>

          {handicaps.map((h) => (
            <button
              key={h.id}
              type="button"
              className="setup__choice"
              data-on={handicap === h.id || undefined}
              aria-pressed={handicap === h.id}
              onClick={() => setHandicap(h.id)}
            >
              <span className="setup__choice-name">{h.name}</span>
              <span className="setup__choice-note">{h.note}</span>
            </button>
          ))}
        </div>

        {handicap !== null && (
          /* **아래 둘이 사라지는 이유를 화면이 먼저 말한다.** 안 적어 두면 방금 고른
             手番이 없어진 것이 고장으로 읽힌다. */
          <p className="setup__caveat">
            駒落ちでは、あなたが下手（先手）から指します。相手（上手）の駒が落ちているので、戦型は選べません。
          </p>
        )}
      </fieldset>

      {handicap === null && (
        <HirateChoices
          color={color}
          setColor={setColor}
          opening={opening}
          setOpening={setOpening}
          openings={openings}
        />
      )}

      <button
        type="button"
        className="btn btn--primary setup__start"
        onClick={() =>
          /* **手合割이 나머지를 덮는다.** 서버도 같은 순서로 덮으므로(`newSetup`) 여기서
             맞춰 두지 않으면 화면이 기억하는 다음 판의 기본값만 어긋난다. */
          onStart(handicap === null ? { color, opening, handicap: null } : { color: 'b', opening: null, handicap })
        }
      >
        対局をはじめる
      </button>

      {/* **상대가 사람인 갈래는 여기서 갈린다.** 위에서 고른 것을 하나도 안 쓴다 —
          手番은 振り駒가 붙어 저쪽이 따로 고르고(FriendMatch), 手合割과 戦型은 컴퓨터에게
          시키는 것이라 사람 상대에게는 뜻이 없다. */}
      <FriendMatch />

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

/**
 * 平手에서만 고르는 둘 — 手番과 상대의 진형.
 *
 * **한 덩이로 갈라 둔다.** 駒落ち에서 둘이 **같이** 사라지고 이유도 하나라서, 조건을 두 군데
 * 두면 나중에 한쪽만 남는다(Setup 의 doc).
 */
function HirateChoices({
  color,
  setColor,
  opening,
  setOpening,
  openings,
}: {
  color: Color;
  setColor: (c: Color) => void;
  opening: string | null;
  setOpening: (id: string | null) => void;
  openings: Opening[];
}) {
  return (
    <>
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
    </>
  );
}

/**
 * 사람과 두는 갈래로 나가는 문.
 *
 * **로그인해야 열린다.** 익명은 서로 구별할 수단이 없어서 「이 방의 상대가 아까 그
 * 사람인가」에 답할 수 없고, 그러면 정원 2명이라는 규칙이 성립하지 않는다.
 *
 * **눌러도 안 되는 버튼을 안 띄운다** — 로그인 안 한 사람에게는 이유를 적은 줄이 선다
 * (`Account` 와 마이페이지 탭이 이미 쓰는 규칙, journal §76).
 *
 * **手番을 위 화면과 따로 고른다.** 저쪽은 엔진 상대의 설정이고 여기는 사람 상대라
 * 振り駒가 붙는다 — 같은 값을 쓰면 그 선택지가 엔진 대국으로도 새어 나간다.
 */
function FriendMatch() {
  const { me } = useViewer();
  const [seat, setSeat] = useState<SeatChoice>('r');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // 로그인이라는 것이 이 배포에 없으면 자리를 아예 안 그린다 — 있는데 못 쓰는 것과
  // 없는 것은 다르고, 후자에 안내문을 띄우면 없는 기능을 말하게 된다.
  if (!me.enabled) return null;

  return (
    <div className="setup__friend">
      <h3 className="setup__legend">友だちと対局</h3>
      {me.user === null ? (
        <p className="setup__caveat">友だちと指すにはログインが必要です。</p>
      ) : (
        <>
          <p className="setup__caveat">
            {/* **정한 것 셋을 누르기 전에 말한다.** 시계는 판을 지게 만들 수 있고,
                개입이 없는 것은 이 앱을 개입으로 알고 온 사람에게 고장으로 읽힌다. */}
            部屋をつくるとリンクが出ます。それを送った相手が開くと対局がはじまります。持ち時間は一手60秒で、
            対人戦では口出しもヒントも出ません。
          </p>
          <fieldset className="setup__group">
            <legend className="setup__legend">あなたの手番</legend>
            <div className="setup__choices setup__choices--seat">
              {SEATS.map((s) => (
                <button
                  key={s.value}
                  type="button"
                  className="setup__choice"
                  data-on={seat === s.value || undefined}
                  aria-pressed={seat === s.value}
                  onClick={() => setSeat(s.value)}
                >
                  <span className="setup__choice-name">{s.label}</span>
                  <span className="setup__choice-note">{s.note}</span>
                </button>
              ))}
            </div>
          </fieldset>
          <button
            type="button"
            className="btn setup__start"
            disabled={busy}
            onClick={() => {
              setBusy(true);
              setError(null);
              const ac = new AbortController();
              // **성공해도 되돌린다.** 이 화면은 방으로 옮겨 가도 언마운트되지 않는다 —
              // `App` 이 `hidden` 으로만 감추므로(대국을 두는 중에 탭을 옮겨도 판이 살아
              // 있어야 한다), 여기서 안 되돌리면 돌아왔을 때 버튼이 영영 눌리지 않는다.
              void createRoom(seat, ac.signal)
                .then((room) => navigate({ name: 'room', id: room.id }))
                .catch((e: Error) => setError(e.message))
                .finally(() => setBusy(false));
            }}
          >
            {busy ? '部屋をつくっています…' : '対局部屋をつくる'}
          </button>
          {error !== null && (
            <p className="rejection" role="alert">
              {error}
            </p>
          )}
        </>
      )}
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
