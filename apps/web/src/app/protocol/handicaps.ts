export interface Handicap {
  id: string;
  name: string;
  note: string;
}

/**
 * 실패하면 던지지 않고 빈 목록을 준다. 平手만 고를 수 있어도 대국은 시작돼야 한다.
 *
 * 「平手」는 목록에 없다. 화면이 그 버튼을 직접 그린다.
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
