import { useCallback, useEffect, useState } from 'react';

import { declineResume, fetchResumable, type ResumableGame } from '@/protocol/resume';

export interface Resumable {
  /** 이어할 수 있는 판. 없거나 이미 답했으면 null. */
  game: ResumableGame | null;
  /** 「いいえ」. 그 판을 닫고 카드를 접는다. */
  decline: () => void;
  /** 「はい」를 눌러 이어했다. 카드만 접는다. 판을 여는 것은 `useGame` 이다. */
  taken: () => void;
}

/**
 * 이어할 수 있는 대국을 조회하고 선택 결과를 처리한다.
 *
 * 새로고침하거나 브라우저를 다시 열 때 한 번 조회한다. 앱 내 탭 이동은 대국 화면을
 * 감추기만 하고 연결을 유지하므로 다시 조회하지 않는다(`App.tsx`).
 *
 * 조회 실패는 이어할 판이 없는 경우와 동일하게 표시한다. 새 대국은 시작할 수 있다(`useViewer`).
 */
export function useResumable(): Resumable {
  const [game, setGame] = useState<ResumableGame | null>(null);

  useEffect(() => {
    const ac = new AbortController();
    void fetchResumable(ac.signal).then(setGame);
    return () => ac.abort();
  }, []);

  const decline = useCallback(() => {
    setGame((prev) => {
      // 답을 기다리지 않고 카드를 접는다(protocol/resume.ts 의 `declineResume`).
      if (prev) declineResume(prev.id);
      return null;
    });
  }, []);

  const taken = useCallback(() => setGame(null), []);

  return { game, decline, taken };
}
