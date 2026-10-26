import { useCallback, useEffect, useRef, useState } from 'react';

import type { Loaded } from '@/hooks/useReview';
import { useT } from '@/i18n';
import { SignedOutError, UnavailableError } from '@/libs/explore/snapshots';
import { deleteTsumeSolve, fetchTsumeSolves, renameTsumeSolve } from '@/libs/tsume/solves';
import type { TsumeSolve } from '@/protocol/tsume';

/**
 * 푼 詰め将棋의 기록과 그것을 고치는 둘. `useExploreSnapshots` 와 같은 모양이고 저장만 없다.
 *
 * `solved` 가 바뀌면 목록을 다시 묻는다. 기록은 풀기가 서버에서 남기므로 화면이 줄을 지어
 * 붙일 수 없다. 다시 물을 때 목록을 지우지 않는다. 지우면 풀 때마다 목록이 한 번 깜빡인다.
 */
export interface TsumeSolveSource {
  loaded: Loaded<TsumeSolve[]>;
  signedOut: boolean;
  unavailable: boolean;
  pending: boolean;
  error: string;
  rename: (id: number, name: string) => Promise<boolean>;
  remove: (id: number) => Promise<boolean>;
  reload: () => void;
}

export function useTsumeSolves(solved: number): TsumeSolveSource {
  const [loaded, setLoaded] = useState<Loaded<TsumeSolve[]>>({ state: 'loading' });
  const [signedOut, setSignedOut] = useState(false);
  const [unavailable, setUnavailable] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  const reload = useCallback(() => setAttempt((n) => n + 1), []);
  const t = useT();
  const tRef = useRef(t);
  tRef.current = t;

  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    setLoaded((prev) => (prev.state === 'ready' ? prev : { state: 'loading' }));

    fetchTsumeSolves(controller.signal, tRef.current)
      .then((solves) => setLoaded({ state: 'ready', data: solves }))
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        if (err instanceof SignedOutError || err instanceof UnavailableError) {
          if (err instanceof SignedOutError) setSignedOut(true);
          else setUnavailable(true);
          setLoaded({ state: 'ready', data: [] });
          return;
        }
        setLoaded({ state: 'error', message: err instanceof Error ? err.message : '' });
      });

    return () => controller.abort();
  }, [attempt, solved]);

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

  const rename = useCallback(
    (id: number, name: string) =>
      mutate(async () => {
        await renameTsumeSolve(id, name, tRef.current);
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
        await deleteTsumeSolve(id, tRef.current);
        if (!alive.current) return;
        setLoaded((prev) =>
          prev.state === 'ready' ? { state: 'ready', data: prev.data.filter((s) => s.id !== id) } : prev,
        );
      }),
    [mutate],
  );

  return { loaded, signedOut, unavailable, pending, error, rename, remove, reload };
}
