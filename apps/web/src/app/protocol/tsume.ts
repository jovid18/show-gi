import type { ApiError } from '@/protocol/review';

/**
 * 詰め将棋 트리의 수 하나. 공격 쪽 수의 `replies` 는 수비 쪽 응수 전부이고(긴 것부터), 수비 쪽
 * 수의 `replies` 는 공격 쪽의 다음 수 하나다. 詰み를 거는 수와 無駄合い는 `replies` 가 없다.
 */
export interface TsumeMove {
  usi: string;
  /** 棋譜 표기(▲5二金). 서버가 적는다. */
  ja: string;
  /** 이 수를 둔 뒤의 국면. */
  sfen: string;
  /** 이 수 뒤 詰みまでの手数. 無駄合い는 세지 않는다. */
  rest: number;
  futile?: boolean;
  replies?: TsumeMove[];
}

export interface TsumeResponse {
  status: 'mate' | 'nomate' | 'unknown';
  plies?: number;
  shortest: boolean;
  tree?: TsumeMove;
  /** 화면에 그대로 나가는 일본어. */
  message: string;
}

export class TsumeError extends Error {
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
}

export async function solveTsume(sfen: string, signal: AbortSignal | null = null): Promise<TsumeResponse> {
  const res = await fetch('/api/tsume/solve', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sfen }),
    signal,
  });
  if (res.ok) return (await res.json()) as TsumeResponse;
  const err = (await res.json().catch(() => null)) as ApiError | null;
  throw new TsumeError(err?.error || 'internal', err?.message || '詰みを調べられませんでした。');
}
