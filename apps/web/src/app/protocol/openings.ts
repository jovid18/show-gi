// `GET /api/openings` 의 계약. 서버의 `internal/server/openings.go` 와 짝이다.
//
// **수순이 여기 없다.** 서버가 이름과 한 줄 설명까지만 보낸다 — 상대가 다음에 무엇을 둘지가
// 페이로드에 있으면 devtools 하나로 초반이 통째로 보인다(그쪽 파일의 `openingItem` 주석).

/** 고를 수 있는 진형 하나. */
export interface Opening {
  id: string;
  /** 일본어 이름(四間飛車). 그대로 그린다. */
  name: string;
  /** 한 줄 설명. 초심자가 무엇을 고르는지 알 수 있게 서버가 일본어로 준다. */
  note: string;
  /** 출처 URL. 화면에서 이름에 걸어 둔다 — 인용 규약은 journal §30. */
  source: string;
}

/**
 * 진형 목록을 받는다.
 *
 * **실패하면 빈 목록이다.** 이 자리가 막혀도 「おまかせ」로 대국은 시작할 수 있어야 한다 —
 * 목록은 고르는 즐거움이고 대국이 본체다(엔진·DB가 없어도 판정만 꺼지는 것과 같은 판단).
 */
export async function fetchOpenings(signal: AbortSignal): Promise<Opening[]> {
  try {
    const res = await fetch('/api/openings', { signal });
    if (!res.ok) return [];
    const body = (await res.json()) as { openings?: Opening[] };
    return body.openings ?? [];
  } catch {
    return [];
  }
}
