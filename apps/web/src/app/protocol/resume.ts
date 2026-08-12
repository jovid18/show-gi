// `GET /api/resumable` · `POST /api/resumable/{id}/decline` 의 계약.
// 서버의 `internal/server/resume.go` 와 짝이다.
//
// **기보가 여기 없다.** 물음 카드가 그릴 것은 「몇 手目까지 두던 판인가」뿐이고, 수순을
// 실으면 이 표면이 되짚기의 우회로가 된다 — 그쪽은 결과가 나온 판만 연다(그 파일의 주석).

/** 이어할 수 있는 판. */
export interface ResumableGame {
  id: number;
  /** 그 판에서 사람이 잡은 쪽. 이어하면 그대로 이어진다. */
  myColor: 'b' | 'w';
  startedAt: string;
  moveCount: number;
  /** 그때 고른 상대의 진형 id. 「おまかせ」였으면 안 온다. */
  opening?: string;
  /** 그 진형의 일본어 이름. **화면이 id로 문장을 짓지 않는다.** */
  openingJa?: string;
}

/**
 * 이어할 수 있는 판을 묻는다. 없으면 null.
 *
 * **실패해도 null이다.** 여기가 막혀도 새 대국은 그대로 시작할 수 있어야 하고, 「이어할
 * 판이 없다」와 그림이 같다 — `fetchOpenings` 와 같은 판단이다.
 *
 * 로그인 안 했으면 서버가 늘 null을 준다. 익명 판은 서로 구별할 수단이 없어서 「누구의
 * 중단된 판인가」에 답할 수가 없다.
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
 *
 * **답을 안 기다린다.** 실패해도 사람은 이미 새 대국으로 넘어가야 하고, 실패의 결과는
 * 「다음에 한 번 더 물어본다」뿐이다 — 그것 때문에 시작 화면을 막지 않는다.
 */
export function declineResume(id: number): void {
  void fetch(`/api/resumable/${id}/decline`, { method: 'POST' }).catch(() => {
    // 위 주석 참조 — 화면에 말하지 않는다.
  });
}
