import { useCallback, useEffect, useState } from 'react';

import { declineResume, fetchResumable, type ResumableGame } from '@/protocol/resume';

export interface Resumable {
  /** 이어할 수 있는 판. 없거나 이미 답했으면 null. */
  game: ResumableGame | null;
  /** 「いいえ」. 그 판을 닫고 카드를 접는다. */
  decline: () => void;
  /** 「はい」를 눌러 이어했다. 카드만 접는다 — 판을 여는 것은 `useGame` 이다. */
  taken: () => void;
}

/**
 * 두다 만 판이 있는가.
 *
 * 한 번만 묻는다. 이 물음이 뜻을 갖는 것은 문서가 새로 열린 순간뿐이다 — 새로고침과
 * 브라우저 종료가 판을 끊는 유일한 길이고(탭을 옮기는 것은 대국 화면을 감추기만 한다,
 * `App.tsx`), 그 둘은 어느 쪽이든 새 문서로 돌아온다. 그 뒤로 이 값이 바뀌는 자리는
 * 사람이 답하는 것 하나뿐이다.
 *
 * 실패를 화면에 말하지 않는다 — 「이어할 판이 없다」와 그림이 같고, 새 대국은 그대로
 * 시작할 수 있기 때문이다(`useViewer` 와 같은 판단).
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
      // 답을 안 기다리고 카드를 접는다. 실패의 결과는 「다음에 한 번 더 물어본다」
      // 뿐이라(protocol/resume.ts), 그것 때문에 시작 화면을 붙들지 않는다.
      if (prev) declineResume(prev.id);
      return null;
    });
  }, []);

  const taken = useCallback(() => setGame(null), []);

  return { game, decline, taken };
}
