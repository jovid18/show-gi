import { useCallback, useEffect, useRef, useState } from 'react';

import type { Loaded } from '@/hooks/useReview';
import {
  deleteSnapshot,
  fetchSnapshots,
  renameSnapshot,
  saveSnapshot,
  SignedOutError,
  UnavailableError,
} from '@/libs/explore/snapshots';
import type { ExploreSnapshot } from '@/protocol/explore';

/**
 * 검토에서 저장한 국면 목록과 그것을 고치는 셋.
 *
 * 고친 뒤에 다시 묻지 않고 서버가 답한 행으로 그 자리만 고친다 — 다시 물으면 목록이
 * 한 번 사라졌다 서고, 그 사이에 방금 저장한 줄을 못 찾는다.
 *
 * `useFetch` 를 안 쓴다(useReview). 저쪽은 주소 하나를 읽는 것뿐이고, 여기는 목록의
 * 주인이라 고치는 자리가 셋이다.
 */
export interface SnapshotSource {
  loaded: Loaded<ExploreSnapshot[]>;
  /**
   * 로그인 벽에서 닫혔다. 목록도 저장 칸도 없고 화면이 한 줄만 남긴다 — 검토 자체는
   * 로그인 없이 도므로(journal §100) 이 패널을 통째로 지우면 저장이 「없는 기능」이 된다.
   */
  signedOut: boolean;
  /**
   * 기록이 없는 배포라 이 표면이 아예 없다. 그때는 화면이 패널을 안 그린다 — 로그인 벽과
   * 갈리는 자리다: 저쪽은 사람이 열 수 있고 이쪽은 열 방법이 없다.
   */
  unavailable: boolean;
  /** 고치는 요청이 도는 중. 목록을 지우지 않고 버튼만 잠근다. */
  pending: boolean;
  /** 마지막 실패. 서버가 준 일본어다(libs/explore/snapshots.ts). */
  error: string;
  /** 지금 보고 있는 자리를 남긴다. 성공하면 true — 부르는 쪽이 입력을 비운다. */
  save: (name: string, handicap: string, moves: readonly string[]) => Promise<boolean>;
  rename: (id: number, name: string) => Promise<boolean>;
  remove: (id: number) => Promise<boolean>;
  reload: () => void;
}

export function useExploreSnapshots(): SnapshotSource {
  const [loaded, setLoaded] = useState<Loaded<ExploreSnapshot[]>>({ state: 'loading' });
  const [signedOut, setSignedOut] = useState(false);
  const [unavailable, setUnavailable] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  const reload = useCallback(() => setAttempt((n) => n + 1), []);

  /** 떠난 뒤에 온 답으로 상태를 고치지 않는다. 고치기 셋은 취소를 안 건다. */
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    setLoaded({ state: 'loading' });

    fetchSnapshots(controller.signal)
      .then((snapshots) => setLoaded({ state: 'ready', data: snapshots }))
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        if (err instanceof SignedOutError || err instanceof UnavailableError) {
          // 빈 목록으로 두면 「하나도 없다」로 읽히고, 그 옆에 눌러도 안 되는 저장
          // 버튼이 선다. 붉은 알림도 아니다 — 둘 다 다시 눌러서 열리는 실패가 아니다.
          if (err instanceof SignedOutError) setSignedOut(true);
          else setUnavailable(true);
          setLoaded({ state: 'ready', data: [] });
          return;
        }
        setLoaded({ state: 'error', message: err instanceof Error ? err.message : '' });
      });

    return () => controller.abort();
  }, [attempt]);

  /**
   * 고치는 요청 하나를 감싼다. 셋이 같은 자리에서 잠기고 같은 자리에 실패를 남긴다 —
   * 세 벌로 적으면 한쪽만 `pending` 을 안 내린다.
   */
  const mutate = useCallback(async (run: () => Promise<void>): Promise<boolean> => {
    setPending(true);
    setError('');
    try {
      await run();
      return true;
    } catch (err: unknown) {
      if (!alive.current) return false;
      if (err instanceof SignedOutError) {
        setSignedOut(true);
        return false;
      }
      if (err instanceof UnavailableError) {
        setUnavailable(true);
        return false;
      }
      setError(err instanceof Error ? err.message : '');
      return false;
    } finally {
      if (alive.current) setPending(false);
    }
  }, []);

  const save = useCallback(
    (name: string, handicap: string, moves: readonly string[]) =>
      mutate(async () => {
        const created = await saveSnapshot(name, handicap, moves);
        if (!alive.current) return;
        // 앞에 붙인다. 서버도 최근 저장한 것을 앞에 두므로(query/explore.sql) 다시
        // 물어본 순서와 같다.
        setLoaded((prev) => (prev.state === 'ready' ? { state: 'ready', data: [created, ...prev.data] } : prev));
      }),
    [mutate],
  );

  const rename = useCallback(
    (id: number, name: string) =>
      mutate(async () => {
        await renameSnapshot(id, name);
        if (!alive.current) return;
        setLoaded((prev) =>
          prev.state === 'ready'
            ? { state: 'ready', data: prev.data.map((s) => (s.id === id ? { ...s, name } : s)) }
            : prev,
        );
      }),
    [mutate],
  );

  const remove = useCallback(
    (id: number) =>
      mutate(async () => {
        await deleteSnapshot(id);
        if (!alive.current) return;
        setLoaded((prev) =>
          prev.state === 'ready' ? { state: 'ready', data: prev.data.filter((s) => s.id !== id) } : prev,
        );
      }),
    [mutate],
  );

  return { loaded, signedOut, unavailable, pending, error, save, rename, remove, reload };
}
