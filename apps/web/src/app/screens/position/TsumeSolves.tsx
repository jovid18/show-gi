import { useState } from 'react';

import { useTsumeSolves } from '@/hooks/useTsumeSolves';
import { useLocale, useT, type Messages } from '@/i18n';
import { dateText } from '@/libs/review/labels';
import type { TsumeSolve } from '@/protocol/tsume';

/**
 * 푼 詰め将棋의 기록(journal §155). 목록·다시 열기·이름 고치기·삭제가 여기서 다 된다.
 *
 * 저장 버튼이 없다. 풀 때마다 서버가 남기고, 화면은 `solved` 가 바뀌면 목록을 다시 묻는다.
 * 모양은 검토의 저장 국면(`Snapshots`)과 같은 클래스를 쓴다.
 */
interface TsumeSolvesProps {
  /** 지금까지 푼 횟수. 바뀌면 목록을 다시 묻는다. */
  solved: number;
  /** 지금 판의 SFEN. 같은 줄에 표시만 한다. */
  current: string;
  /** 그 국면을 판에 올리고 다시 푼다. 답은 서버 캐시에서 온다. */
  onOpen: (sfen: string) => void;
}

/** 이름 칸의 상한. 서버와 같은 값이다(exploreSnapshotNameMax). */
const NAME_MAX = 40;

export function TsumeSolves({ solved, current, onOpen }: TsumeSolvesProps) {
  const t = useT();
  const s = t.position.solves;
  const { loaded, signedOut, unavailable, pending, error, rename, remove, reload } = useTsumeSolves(solved);
  const [editing, setEditing] = useState<{ id: number; name: string } | null>(null);
  /** 지우기를 한 번 누른 줄. 두 번 눌러야 지워진다. */
  const [confirming, setConfirming] = useState<number | null>(null);

  // 이 화면은 로그인 뒤에만 열린다(PositionScreen). 그 사이에 나갔거나 기록이 없는 배포면
  // 자리를 만들지 않는다.
  if (unavailable || signedOut) return null;

  const onRename = (): void => {
    if (!editing) return;
    void rename(editing.id, editing.name.trim()).then((ok) => {
      if (ok) setEditing(null);
    });
  };

  return (
    <section className="review-panel explore-snapshots" aria-label={s.label}>
      <h2 className="panel-title">{s.label}</h2>

      {error && (
        <p className="rejection" role="alert">
          {error}
        </p>
      )}

      {loaded.state === 'loading' && <p className="review-empty">{t.common.loading}</p>}

      {loaded.state === 'error' && (
        <div className="explore-error" role="alert">
          <p className="rejection">{loaded.message}</p>
          <button type="button" className="btn" onClick={reload}>
            {t.explore.reload}
          </button>
        </div>
      )}

      {loaded.state === 'ready' &&
        (loaded.data.length === 0 ? (
          <p className="review-empty">{s.empty}</p>
        ) : (
          <ul className="explore-snapshot-list">
            {loaded.data.map((solve) => (
              <li key={solve.id}>
                {editing?.id === solve.id ? (
                  <form
                    className="explore-snapshot-save"
                    onSubmit={(e) => {
                      e.preventDefault();
                      onRename();
                    }}
                  >
                    <input
                      className="explore-snapshot-input"
                      type="text"
                      value={editing.name}
                      maxLength={NAME_MAX}
                      placeholder={s.namePlaceholder}
                      aria-label={s.newNameLabel}
                      onChange={(e) => setEditing({ id: solve.id, name: e.target.value })}
                    />
                    <button type="submit" className="btn btn--primary" disabled={pending}>
                      {s.rename}
                    </button>
                    <button type="button" className="btn" disabled={pending} onClick={() => setEditing(null)}>
                      {s.cancel}
                    </button>
                  </form>
                ) : (
                  <div className="explore-snapshot-row">
                    <button
                      type="button"
                      className="explore-snapshot"
                      data-on={positionKey(solve.sfen) === positionKey(current) || undefined}
                      disabled={pending}
                      onClick={() => onOpen(solve.sfen)}
                    >
                      <span className="explore-snapshot-name">{solve.name || resultText(solve, s.result)}</span>
                      <SolveMeta solve={solve} />
                    </button>
                    <button
                      type="button"
                      className="explore-snapshot-act"
                      disabled={pending}
                      onClick={() => {
                        setConfirming(null);
                        setEditing({ id: solve.id, name: solve.name });
                      }}
                    >
                      {s.name}
                    </button>
                    {/* 다시 열기 버튼 바로 옆이라 두 번 눌러야 지워진다(Snapshots 와 같다). */}
                    <button
                      type="button"
                      className="explore-snapshot-act"
                      data-danger={confirming === solve.id || undefined}
                      disabled={pending}
                      onClick={() => {
                        if (confirming !== solve.id) {
                          setConfirming(solve.id);
                          return;
                        }
                        setConfirming(null);
                        void remove(solve.id);
                      }}
                    >
                      {confirming === solve.id ? s.confirmRemove : s.remove}
                    </button>
                  </div>
                )}
              </li>
            ))}
          </ul>
        ))}
    </section>
  );
}

/** 手数를 뗀 SFEN. 기록의 `sfen` 은 手数가 늘 1이라 글자로 넣은 판과 그 자리만 다를 수 있다. */
function positionKey(sfen: string): string {
  return sfen.slice(0, sfen.lastIndexOf(' '));
}

type ResultWords = Messages['position']['solves']['result'];

/** 최단을 확인하지 못한 詰み는 「N手詰め」로 단정하지 않는다. 풀기의 문구(tsumeMessage)와 같은 갈래다. */
function resultText(solve: TsumeSolve, words: ResultWords): string {
  if (solve.status === 'mate') return (solve.shortest ? words.mate : words.mateUnproven)(solve.plies ?? 0);
  return words[solve.status];
}

/** 한 줄의 곁 정보. 이름을 붙였으면 결과를 여기에 둔다. 이름이 없으면 결과가 이미 이름 자리에 있다. */
function SolveMeta({ solve }: { solve: TsumeSolve }) {
  const t = useT();
  const locale = useLocale();
  return (
    <span className="explore-snapshot-meta">
      {solve.name !== '' && <span>{resultText(solve, t.position.solves.result)}</span>}
      <time dateTime={solve.solvedAt}>{dateText(solve.solvedAt, locale)}</time>
    </span>
  );
}
