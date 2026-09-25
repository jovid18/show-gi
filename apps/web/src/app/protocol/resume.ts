// 수순을 싣지 않는다. 실으면 이 API 가 결과가 나오지 않은 판의 기보를 여는 우회로가 된다
// (되짚기는 결과가 나온 판만 연다).

export interface ResumableGame {
  id: number;
  myColor: 'b' | 'w';
  startedAt: string;
  moveCount: number;
  opening?: string;
  openingJa?: string;
  handicap?: string;
  handicapJa?: string;
}

/**
 * 이어할 수 있는 판을 묻는다. 없으면 null.
 *
 * 실패해도 던지지 않고 null 을 준다. 여기가 막혀도 새 대국은 시작할 수 있어야 한다.
 */
export async function fetchResumable(signal: AbortSignal): Promise<ResumableGame | null> {
  try {
    const res = await fetch('/api/resumable', { signal });
    if (!res.ok) return null;
    const body = (await res.json()) as { game?: ResumableGame | null };
    return body.game ?? null;
  } catch {
    return null;
  }
}

/**
 * 「いいえ」를 남긴다. 그 판은 중단된 채로 끝나고 다시 물어보지 않는다.
 */
export function declineResume(id: number): void {
  void fetch(`/api/resumable/${id}/decline`, { method: 'POST' }).catch(() => {
    // 실패하면 다음에 한 번 더 묻는다. 화면에 알리지 않는다.
  });
}
