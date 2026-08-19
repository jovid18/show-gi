// `GET /api/handicaps` 의 계약. 서버의 `internal/server/handicaps.go` 와 짝이다.
//
// 판도 기준점도 여기 없다. 시작 국면은 대국이 열린 뒤 스냅샷으로 오고(`Snapshot.sfen`),
// 판정의 기준점은 서버의 상수다 — 화면이 아는 것은 고를 이름과 한 줄 설명뿐이다.

/** 고를 수 있는 手合割 하나. */
export interface Handicap {
  id: string;
  /** 일본어 이름(二枚落ち). 그대로 그린다. */
  name: string;
  /** 한 줄 설명. 무엇이 빠지는지를 서버가 일본어로 준다. */
  note: string;
}

/**
 * 手合割 목록을 받는다.
 *
 * 실패하면 빈 목록이다. 그때는 平手 하나만 고를 수 있는 화면이 되고 대국은 그대로
 * 시작된다 — `fetchOpenings` 와 같은 판단이다.
 *
 * 「平手」가 목록에 없다. 접지 않는 것이 기본값이라 화면이 그 버튼을 직접 그린다 —
 * 진형의 「おまかせ」와 같은 자리다.
 */
export async function fetchHandicaps(signal: AbortSignal): Promise<Handicap[]> {
  try {
    const res = await fetch('/api/handicaps', { signal });
    if (!res.ok) return [];
    const body = (await res.json()) as { handicaps?: Handicap[] };
    return body.handicaps ?? [];
  } catch {
    return [];
  }
}
