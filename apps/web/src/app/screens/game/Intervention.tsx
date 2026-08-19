import { useEffect, useMemo, useRef } from 'react';

import { evalTone, playerCp, rankOf, rowScoreJa, type ExploredMove } from '@/libs/whatif/branch';
import type { Intervention as InterventionData } from '@/protocol/game';
import type { WhatIfNode } from '@/protocol/whatif';

interface InterventionProps {
  intervention: InterventionData;
  /**
   * 분기의 지금 자리. `null` 이면 아직 못 받았다 — 그동안에도 카드는 그대로 서 있고
   * 목록 자리만 비어 있다(카드가 통째로 바뀌지 않는다는 규칙이 이 컴포넌트의 전부다).
   */
  node: WhatIfNode | null;
  pending: boolean;
  error: string | null;
  /** 물러진 수 자체의 값. 분기의 첫 자리에서 나온다. */
  retractedEval: string;
  /**
   * 이 판의 「형세 0」(플레이어 관점 cp). 平手면 0이다.
   *
   * 후보 줄의 색만 이 값을 쓴다(`evalTone`) — 숫자는 원본 cp 그대로다.
   */
  baselineCp: number;
  /**
   * 판을 만질 수 있는가. 여기서 다시 계산하지 않는다 — 대국 화면이 이미 정한 값이고,
   * 두 벌이면 어긋난다(成りますか가 떠 있는 동안이 실제로 그 자리였다).
   */
  playable: boolean;
  /** 이 자리에서 사람이 직접 둬 본 수들. 후보 셋 밖의 것만 들어 있다. */
  explored: readonly ExploredMove[];
  onPlay: (usi: string) => void;
  onBack: () => void;
  onRoot: () => void;
  /** 첫 요청이 튕겼을 때 다시 묻는다. 그 자리가 없으면 카드가 영영 비어 있다. */
  onRetry: () => void;
  onDismiss: () => void;
}

/** 목록의 한 줄. 엔진이 고른 후보와 사람이 둬 본 수가 같은 모양으로 선다. */
interface Row {
  usi: string;
  ja: string;
  /** 그 수를 둔 쪽 관점 cp. 후보끼리 견주는 자라 이쪽이어야 위가 「그 쪽에게 좋은 수」다. */
  cp: number | undefined;
  /** 같은 관점의 詰み 手数. 양수면 그 수를 둔 쪽이 詰ます. */
  mateIn: number | undefined;
  /**
   * 최선수 대비 낙폭. 서버가 준 것만 쓴다 — 여기서 뺄셈을 하면 낙폭의 정의가 두 벌이
   * 되고, 사람이 둬 본 수는 그 자리의 후보 탐색에서 나온 값이 아니라 애초에 기준이 없다.
   */
  lossCp: number | undefined;
  /** 엔진이 1위로 꼽은 수인가. 판 위의 초록 화살표가 가리키는 것과 같다. */
  best: boolean;
}

/**
 * 최선수 대비 낙폭 한 칸. 「この手を選ぶといくら損か」이고, 사람이 직접 요구한 값이다
 * (docs/playtests/2026-08-13-human-1.md §6 #11).
 *
 * 1위 줄의 「最善」이 이 칸의 이름표다. 맨 위에 그 한 단어가 서 있어서 아래의 「−320」이
 * 무엇에 대한 값인지가 그 자리에서 읽힌다 — 열 제목을 얹지 않고 그 일을 한다. 1위를 색으로만
 * 표시하던 자리이기도 해서, 색을 못 보는 사람에게도 1위가 남는다.
 *
 * 낙폭이 없는 줄은 비운다. 사람이 직접 둬 본 수(기준이 될 후보 탐색이 없다)와 詰み이
 * 섞인 줄(cp가 환산값이라 뺄셈이 낙폭이 아니다)이 그렇다 — 0으로 채우면 「최선수와 같다」는
 * 거짓이 된다.
 */
function lossJa(row: Row): string {
  if (row.best) return '最善';
  return row.lossCp ? `−${row.lossCp}` : '';
}

/**
 * 지금 분기가 어떤 상태인지 한 줄로. 판정을 여기서 하지 않는다 — 상태도 차례도 서버가
 * 정한 것을 말로 옮길 뿐이다.
 *
 * 되짚기의 `branchStatusJa` 와 시제가 다르다. 저쪽은 끝난 판을 보는 자리라 「負けでした」
 * 이고, 여기는 아직 안 벌어진 일이라 「負けになります」다.
 */
function noteJa(node: WhatIfNode | null, pending: boolean, failed: boolean): string {
  // 못 받았으면 「조사 중」이라고 말하지 않는다. 기다리면 온다는 거짓말이 되고,
  // 그 옆에 이미 실패 문구가 서 있다.
  if (failed) return 'もう一度読み込むと、この手のあとを試せます。';
  if (!node) return pending ? '読んでいます…' : 'この手のあとを調べています。';
  switch (node.status) {
    case 'checkmate':
      return node.yourTurn ? '詰みです。ここで負けになります。' : '詰みです。ここで勝ちになります。';
    case 'stalemate':
      // 쇼기에서 手詰まり는 무승부가 아니라 패배다.
      return node.yourTurn ? '手詰まりです。ここで負けになります。' : '手詰まりです。ここで勝ちになります。';
    default:
      // 어느 쪽이든 사람이 둔다. 상대 차례면 「상대라면 어떻게 둘까」를 직접 둬 보는 것이
      // 이 자리의 내용이고, 그때 초록 화살표가 엔진의 답을 짚고 있다.
      return node.yourTurn ? 'あなたの番。盤の上でも指せます。' : '相手の番。相手の手も指せます。';
  }
}

/**
 * 제지형 개입의 문구 쪽.
 *
 * 최선수를 말하지 않는다(docs/01-core.md §1). 여기 나오는 수는 전부 물러진 수 뒤의
 * 것이고, 그 국면은 되물러서 이미 사라졌다 — 「지금 이렇게 두라」가 되는 수는 한 줄도 없다.
 * 바닥을 지키는 쪽은 `useWhatIf` 의 `floor` 와 서버의 `branchRoot` 둘이다.
 *
 * 이 카드는 다른 것으로 바뀌지 않는다. 판을 만졌다고 분기 패널이 이 자리를 빼앗으면
 * 읽던 설명이 사라진다 — 설명·목록·무르기를 한 카드에 두고, 분기가 되는 것은 판뿐이다.
 *
 * 판 위의 연출(빛·유령 駒·광선·기운 시점)은 `Board`와 `index.css`에 있다.
 */
export function Intervention({
  intervention,
  node,
  pending,
  error,
  retractedEval,
  baselineCp,
  playable,
  explored,
  onPlay,
  onBack,
  onRoot,
  onRetry,
  onDismiss,
}: InterventionProps) {
  const dismissRef = useRef<HTMLButtonElement>(null);

  // 입력이 판 쪽으로 열려 있어도 초점의 첫 자리는 여기다. 키보드·스크린리더 사용자가
  // 판을 더듬어 「指し直す」를 찾게 두지 않는다.
  useEffect(() => {
    dismissRef.current?.focus();
  }, []);

  /**
   * 「상대는 이렇게 詰ませてくる」. 증명된 詰み 수순일 때만 온다(서버의 analyst.go).
   *
   * 자를 자리가 없는 유일한 수순이라 남았다 — PV는 어디서 끊을지가 국면마다 달라서,
   * 그 자리는 아래 목록이 대신한다.
   */
  const mateLine = intervention.refutation ?? [];

  /** 물러진 수 뒤의 분기. 바닥(물러진 수 자체)은 이미 위에서 이름으로 불렀다. */
  const line = node?.line.slice(1) ?? [];
  const branching = line.length > 0;

  const rows = useMemo<Row[]>(() => {
    const seen = new Set<string>();
    const out: Row[] = [];
    for (const c of node?.candidates ?? []) {
      seen.add(c.usi);
      out.push({
        usi: c.usi,
        ja: c.ja || c.usi,
        cp: c.evalCp,
        mateIn: c.mateIn,
        lossCp: c.lossCp,
        best: out.length === 0,
      });
    }
    // 사람이 둬 본 수는 표식 없이 같은 줄로 선다. 「탐색한 수」라고 적으면 목록이
    // 두 종류가 되는데, 읽는 사람에게 그 둘은 같은 물음의 답이다 — 이 수를 두면 얼마인가.
    for (const e of explored) {
      if (!seen.has(e.usi)) out.push({ ...e, ja: e.ja || e.usi, lossCp: undefined, best: false });
    }
    return out.toSorted((a, b) => rankOf(b) - rankOf(a));
  }, [node, explored]);

  // 낙폭은 서버가 준 0~1이다. 여기서 다시 계산하지 않고 보이는 단위로만 바꾼다.
  // 막대가 넘치지 않게만 자른다 — 숫자를 손보기 시작하면 화면과 판정이 갈라진다.
  const drop = Math.min(100, Math.max(0, Math.round(intervention.deltaWin * 100)));

  /** 이 자리의 수를 두는 것이 상대인가. 값의 주인이 누구인지가 여기서 갈린다. */
  const byOpponent = !!node && !node.yourTurn;
  /** 한 자리도 못 받은 채 튕겼다. 이 상태는 저절로 안 풀린다 — 사람이 다시 눌러야 한다. */
  const stuck = !node && !pending && error !== null;

  return (
    <div className="intervention" role="alert">
      <p className="intervention-label">待った</p>

      <p className="intervention-move">
        <span className="intervention-move-ja">{intervention.retractedJa}</span>
        <span className="intervention-move-tail">を戻しました</span>
        {/* 그 수를 두면 얼마가 되나. 낙폭(%)은 판정의 값이고 이건 국면의 값이다 —
            아래 목록의 cp와 같은 자를 쓰므로 「거기서 더 나빠지는가」가 견줘진다. */}
        {retractedEval && <span className="intervention-move-eval">{retractedEval}</span>}
      </p>

      <p className="intervention-message">{intervention.message}</p>

      {mateLine.length > 0 && (
        <div className="refutation">
          <p className="refutation-label">このあと詰まされます</p>
          <ol className="refutation-line">
            {mateLine.map((move, i) => (
              // 같은 수가 수순에 두 번 나올 수 있다(왕복). 자리까지 키에 넣는다.
              <li key={`${i}-${move.usi}`} data-by={move.by}>
                <span className="refutation-move">{move.ja}</span>
              </li>
            ))}
          </ol>
        </div>
      )}

      <div className="intervention-branch">
        <p className="intervention-branch-head">
          <span className="intervention-branch-title">そのまま指していたら</span>
          {node && <span className="intervention-branch-turn">{node.yourTurn ? 'あなたの番' : '相手の番'}</span>}
        </p>

        {/* 지금까지 둬 본 줄. 뿌리에서는 아예 안 그린다 — 빈 자리를 만들어 두면
            목록이 그만큼 아래로 밀려 매번 높이가 흔들린다. */}
        {branching && (
          <ol className="intervention-branch-line">
            {line.map((move) => (
              <li key={move.ply} data-by={move.by}>
                {move.ja || move.usi}
              </li>
            ))}
          </ol>
        )}

        {/* 자리는 지키고 내용만 기다린다. 목록을 통째로 다른 것으로 바꾸면 값이 올 때마다
            카드가 무너졌다 다시 선다(되짚기에서 겪은 그 문제다 — WhatIfPanel). */}
        <ul className="intervention-options">
          {rows.map((r) => (
            <li key={r.usi}>
              <button
                type="button"
                className="intervention-options-row"
                data-best={r.best || undefined}
                // 색이 값이라, 값이 없는 줄은 색도 없다. 0으로 채우면 호각으로 읽힌다.
                // 플레이어 관점으로 뒤집어 넘긴다 — 파랑·빨강은 「나에게 좋은가」다(evalTone).
                style={
                  {
                    '--tone': evalTone(playerCp(r, byOpponent), baselineCp),
                  } as React.CSSProperties
                }
                disabled={!playable}
                onClick={() => onPlay(r.usi)}
              >
                <span className="intervention-options-move">{r.ja}</span>
                <span className="intervention-options-cp">{rowScoreJa(r, byOpponent)}</span>
                <span className="intervention-options-loss">{lossJa(r)}</span>
              </button>
            </li>
          ))}
        </ul>

        <p className="intervention-branch-note" data-tone={node?.status === 'playing' ? 'turn' : 'result'}>
          {noteJa(node, pending, stuck)}
        </p>

        {error && (
          <p className="rejection" role="alert">
            {error}
          </p>
        )}

        {/* 여기가 막히면 카드에 남는 것이 문구뿐이다. 노드가 없으면 목록도 무르기도
            안 서고, 요청은 회차가 끝날 때까지 저절로 다시 나가지 않는다. */}
        {stuck && (
          <div className="intervention-branch-actions">
            <button type="button" className="btn btn--step" disabled={pending} onClick={onRetry}>
              もう一度読む
            </button>
          </div>
        )}

        {/* 되돌아가는 길이 둘이다. 한 수씩 물리는 것과 분기를 접는 것은 다른 일이다 —
            몇 수를 들어간 뒤에 처음으로 돌아가려고 「一手戻る」를 다섯 번 누르게 두지 않는다.
            바닥은 물러진 수다: 그 앞으로는 어느 버튼으로도 못 간다(useWhatIf 의 floor). */}
        {branching && (
          <div className="intervention-branch-actions">
            <button type="button" className="btn btn--step" disabled={pending} onClick={onBack}>
              一手戻る
            </button>
            <button type="button" className="btn btn--step" disabled={pending} onClick={onRoot}>
              最初へ
            </button>
          </div>
        )}
      </div>

      <p className="intervention-delta">
        <span className="intervention-delta-text">勝率 −{drop}%</span>
        <span className="intervention-delta-track" aria-hidden="true">
          <span className="intervention-delta-fill" style={{ width: `${drop}%` }} />
        </span>
      </p>

      <div className="intervention-actions">
        <button ref={dismissRef} type="button" className="btn btn--primary" onClick={onDismiss}>
          指し直す
        </button>
      </div>
    </div>
  );
}
