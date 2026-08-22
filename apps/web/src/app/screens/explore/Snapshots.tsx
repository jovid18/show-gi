import { useState } from 'react';

import { useExploreSnapshots } from '@/hooks/useExploreSnapshots';
import { dateJa } from '@/libs/review/labels';
import type { ExploreSnapshot } from '@/protocol/explore';

/**
 * 검토에서 저장한 국면. 목록·불러오기·이름 고치기·삭제가 판 옆에서 다 된다.
 *
 * 불러오기가 주소를 고치는 일이다. 이 화면의 정본이 주소이므로(ExploreScreen) 저장된
 * 국면을 판에 세우는 것도 「주소를 그 값으로 바꾼다」 하나로 끝나고, 그래서 불러온 자리가
 * 새로고침·뒤로 가기·링크 공유에 그대로 살아 있다.
 *
 * 이름은 사람이 붙인 것이고 서버가 든다. 비워 두면 서버가 手数로 하나 짓는다 —
 * 그 문구를 여기 옮겨 적지 않는다(explore_snapshots.go 의 exploreSnapshotDefaultName).
 */
interface SnapshotsProps {
  /** 지금 보고 있는 手合割 id. 빈 값이 平手다. */
  handicap: string;
  /** 지금까지 둔 수순. 저장은 이 두 칸을 그대로 보낸다. */
  moves: readonly string[];
  /**
   * 저장할 수 있는 자리인가. 판이 아직 안 섰으면 false다 — 거절된 줄을 저장하면 서버가
   * 어차피 막지만(한 수씩 되짚어 본다) 그 실패를 사람에게 보일 이유가 없다.
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
  /** 지우기를 한 번 누른 줄. 두 번 눌러야 지워진다 — 되돌릴 수 없는 자리라 그렇다. */
  const [confirming, setConfirming] = useState<number | null>(null);

  // 로그인이 없으면 아예 안 그린다. 검토 자체가 같은 벽 뒤에 있어서 그 문구가 이미
  // 옆 패널에 서 있다(explore.go 의 login_required).
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

      {/* 저장. 폼이라 Enter 하나로 끝난다 — 판에서 손을 뗀 사람이 이름을 치고 바로 누른다. */}
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

      {/* 실패는 저장·이름·삭제가 같은 자리에 남긴다. 셋이 다 이 패널 안의 일이라
          자리가 셋이면 어느 것이 실패했는지가 오히려 안 보인다. */}
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
                      // 지금 보고 있는 자리와 같은가. 「이미 여기다」를 말하는 표시일 뿐
                      // 누르는 것을 막지 않는다 — 다시 눌러 제자리로 돌아오는 것이 맞다.
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
                    {/* 두 번 눌러야 지워진다. 되돌릴 수 없는 자리이고, 불러오기 버튼 바로
                        옆이라 한 번에 지워지면 잘못 누른 것이 곧 잃는 것이 된다. */}
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
 * 한 줄의 곁 정보 — 手合割·手数·저장 시각.
 *
 * 셋이 다 있어야 이름이 같은 두 줄을 가를 수 있다. 이름에 UNIQUE 를 안 걸었으므로
 * (migrations/015) 이 자리가 그 몫을 든다.
 */
function SnapshotMeta({ snapshot }: { snapshot: ExploreSnapshot }) {
  return (
    <span className="explore-snapshot-meta">
      {/* 平手면 안 온다. 화면이 「平手」를 만들어 붙이지 않는다 — 접지 않는 것이 기본값이라
          그 줄에는 적을 것이 없다(protocol/handicaps.ts 와 같은 규약). */}
      {snapshot.handicapJa !== undefined && <span className="explore-snapshot-handicap">{snapshot.handicapJa}</span>}
      <span>{snapshot.moves.length}手</span>
      <time dateTime={snapshot.savedAt}>{dateJa(snapshot.savedAt)}</time>
    </span>
  );
}
