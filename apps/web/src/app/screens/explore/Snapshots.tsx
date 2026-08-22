import { useState } from 'react';

import { useExploreSnapshots } from '@/hooks/useExploreSnapshots';
import { dateJa } from '@/libs/review/labels';
import type { ExploreSnapshot } from '@/protocol/explore';

/**
 * 검토에서 저장한 국면. 목록·불러오기·이름 고치기·삭제가 판 옆에서 다 된다.
 *
 * 불러오기는 주소를 고치는 일 하나다 — 이 화면의 정본이 주소라(ExploreScreen) 불러온
 * 자리도 새로고침·뒤로 가기·링크 공유에 그대로 살아 있다.
 *
 * 이름이 비면 서버가 짓는다. 그 문구를 여기 옮겨 적지 않는다
 * (`exploreSnapshotDefaultName`).
 */
interface SnapshotsProps {
  /** 지금 보고 있는 手合割 id. 빈 값이 平手다. */
  handicap: string;
  /** 지금까지 둔 수순. 저장은 이 두 칸을 그대로 보낸다. */
  moves: readonly string[];
  /**
   * 저장할 수 있는 자리인가. 판이 아직 안 섰으면 false다 — 거절된 줄은 서버가 어차피
   * 막지만 그 실패를 사람에게 보일 이유가 없다.
   */
  savable: boolean;
  /** 저장된 국면을 판에 세운다. 부르는 쪽이 주소를 고친다. */
  onLoad: (handicap: string, moves: string[]) => void;
}

/** 이름 칸의 상한. 서버와 같은 값이다(exploreSnapshotNameMax) — 여기서 먼저 막는다. */
const NAME_MAX = 40;

export function Snapshots({ handicap, moves, savable, onLoad }: SnapshotsProps) {
  const { loaded, signedOut, pending, error, save, rename, remove, reload } = useExploreSnapshots();
  const [name, setName] = useState('');
  /** 이름을 고치는 중인 줄. 한 번에 하나다. */
  const [editing, setEditing] = useState<{ id: number; name: string } | null>(null);
  /** 지우기를 한 번 누른 줄. 두 번 눌러야 지워진다. */
  const [confirming, setConfirming] = useState<number | null>(null);

  // 로그인이 없으면 아예 안 그린다. 그 문구는 이미 옆 패널에 서 있다(explore.go).
  if (signedOut) return null;

  const line = moves.join(',');

  const onSave = (): void => {
    void save(name, handicap, moves).then((ok) => {
      if (ok) setName('');
    });
  };

  const onRename = (): void => {
    if (!editing) return;
    const next = editing.name.trim();
    if (next === '') return;
    void rename(editing.id, next).then((ok) => {
      if (ok) setEditing(null);
    });
  };

  return (
    <section className="review-panel explore-snapshots" aria-label="保存した局面">
      <h2 className="panel-title">保存した局面</h2>

      {/* 저장. 폼이라 이름을 치고 Enter 하나로 끝난다. */}
      <form
        className="explore-snapshot-save"
        onSubmit={(e) => {
          e.preventDefault();
          onSave();
        }}
      >
        <input
          className="explore-snapshot-input"
          type="text"
          value={name}
          maxLength={NAME_MAX}
          placeholder="名前（未入力なら手数が入ります）"
          aria-label="この局面の名前"
          onChange={(e) => setName(e.target.value)}
        />
        <button type="submit" className="btn btn--primary" disabled={!savable || pending}>
          この局面を保存
        </button>
      </form>

      {/* 저장·이름·삭제가 실패를 같은 자리에 남긴다. 자리를 셋으로 가르면 어느 것이
          실패했는지가 오히려 안 보인다. */}
      {error && (
        <p className="rejection" role="alert">
          {error}
        </p>
      )}

      {loaded.state === 'loading' && <p className="review-empty">読み込み中…</p>}

      {loaded.state === 'error' && (
        <div className="explore-error" role="alert">
          <p className="rejection">{loaded.message}</p>
          <button type="button" className="btn" onClick={reload}>
            もう一度読み込む
          </button>
        </div>
      )}

      {loaded.state === 'ready' &&
        (loaded.data.length === 0 ? (
          <p className="review-empty">気になる局面まで並べて、ここに残しておけます。</p>
        ) : (
          <ul className="explore-snapshot-list">
            {loaded.data.map((snapshot) => (
              <li key={snapshot.id}>
                {editing?.id === snapshot.id ? (
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
                      aria-label="新しい名前"
                      onChange={(e) => setEditing({ id: snapshot.id, name: e.target.value })}
                    />
                    <button type="submit" className="btn btn--primary" disabled={pending}>
                      変更
                    </button>
                    <button type="button" className="btn" disabled={pending} onClick={() => setEditing(null)}>
                      やめる
                    </button>
                  </form>
                ) : (
                  <div className="explore-snapshot-row">
                    <button
                      type="button"
                      className="explore-snapshot"
                      // 지금 보고 있는 자리인가. 표시일 뿐 누르는 것은 막지 않는다.
                      data-on={
                        ((snapshot.handicap ?? '') === handicap && snapshot.moves.join(',') === line) || undefined
                      }
                      disabled={pending}
                      onClick={() => onLoad(snapshot.handicap ?? '', snapshot.moves)}
                    >
                      <span className="explore-snapshot-name">{snapshot.name}</span>
                      <SnapshotMeta snapshot={snapshot} />
                    </button>
                    <button
                      type="button"
                      className="explore-snapshot-act"
                      disabled={pending}
                      onClick={() => {
                        setConfirming(null);
                        setEditing({ id: snapshot.id, name: snapshot.name });
                      }}
                    >
                      名前
                    </button>
                    {/* 두 번 눌러야 지워진다. 불러오기 버튼 바로 옆이라 한 번에
                        지워지면 잘못 누른 것이 곧 잃는 것이 된다. */}
                    <button
                      type="button"
                      className="explore-snapshot-act"
                      data-danger={confirming === snapshot.id || undefined}
                      disabled={pending}
                      onClick={() => {
                        if (confirming !== snapshot.id) {
                          setConfirming(snapshot.id);
                          return;
                        }
                        setConfirming(null);
                        void remove(snapshot.id);
                      }}
                    >
                      {confirming === snapshot.id ? '本当に削除' : '削除'}
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

/**
 * 한 줄의 곁 정보 — 手合割·手数·저장 시각. 이름에 UNIQUE 가 없으므로(migrations/015)
 * 같은 이름 둘을 가르는 것이 이 셋이다.
 */
function SnapshotMeta({ snapshot }: { snapshot: ExploreSnapshot }) {
  return (
    <span className="explore-snapshot-meta">
      {/* 平手면 안 온다. 접지 않는 것이 기본값이라 적을 것이 없다(protocol/handicaps.ts). */}
      {snapshot.handicapJa !== undefined && <span className="explore-snapshot-handicap">{snapshot.handicapJa}</span>}
      <span>{snapshot.moves.length}手</span>
      <time dateTime={snapshot.savedAt}>{dateJa(snapshot.savedAt)}</time>
    </span>
  );
}
