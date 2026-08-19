import type { ResumableGame } from '@/protocol/resume';

/**
 * 두다 만 판이 있을 때 시작 화면 대신 뜨는 물음.
 *
 * **고르는 화면(`Setup`)보다 앞이다.** 두던 판이 있는데 선후공부터 다시 고르게 하면
 * 그 판은 사람이 존재를 모르는 채로 사라진다.
 *
 * **로그인한 사람에게만 뜬다.** 서버가 익명에게는 후보를 안 준다 — 익명 판은 서로 구별할
 * 수단이 없어서 「누구의 중단된 판인가」에 답할 수가 없다(`internal/server/resume.go`).
 */

const COLOR_JA: Record<ResumableGame['myColor'], string> = {
  b: '先手',
  w: '後手',
};

/** 駒落ち는 先手/後手가 아니라 下手/上手다. **사람은 언제나 下手다**(`newSetup`). */
const SHITATE_JA = '下手';

interface ResumeProps {
  game: ResumableGame;
  onResume: () => void;
  onDecline: () => void;
}

export function Resume({ game, onResume, onDecline }: ResumeProps) {
  return (
    <div className="setup resume">
      <h2 className="setup__head">前の対局が残っています</h2>

      <dl className="resume__facts">
        <div>
          <dt>手番</dt>
          {/* 駒落ち에서는 이 판의 어휘가 갈린다 — 先手/後手가 아니라 下手/上手다. */}
          <dd>{game.handicapJa === undefined ? COLOR_JA[game.myColor] : SHITATE_JA}</dd>
        </div>
        {/* **駒落ち 판에는 戦型이 없다.** 진형과 手合割은 같이 못 고르므로(Setup) 그 자리에
            「おまかせ」를 적으면 고를 수 있었던 것처럼 읽힌다. */}
        {game.handicapJa === undefined ? (
          <div>
            <dt>相手の戦型</dt>
            {/* 이름이 없으면 「おまかせ」다. **id를 그리지 않는다** — 화면이 코드로 문장을 짓지 않는다. */}
            <dd>{game.openingJa ?? 'おまかせ'}</dd>
          </div>
        ) : (
          <div>
            <dt>手合割</dt>
            <dd>{game.handicapJa}</dd>
          </div>
        )}
        <div>
          <dt>進んだ手数</dt>
          <dd>{game.moveCount}手</dd>
        </div>
      </dl>

      <p className="setup__caveat">
        {/* **그만두면 어떻게 되는지를 누르기 전에 말한다.** 그 판은 되짚기에도 안 나오므로
            (journal §51) 여기서 안 적으면 사라진 이유를 알 길이 없다. */}
        「はじめから」を選ぶと、この対局は中断のまま終わります。振り返りには出ません。
      </p>

      <div className="resume__actions">
        <button type="button" className="btn btn--primary" onClick={onResume}>
          続きから指す
        </button>
        <button type="button" className="btn" onClick={onDecline}>
          はじめから
        </button>
      </div>
    </div>
  );
}
