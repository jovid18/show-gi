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

/**
 * 푼 詰め将棋의 기록 한 줄(`GET /api/tsume/solves`). 풀 때마다 서버가 남긴다.
 *
 * 트리를 들지 않는다. 다시 열면 `sfen` 으로 풀기를 다시 부르고, 서버가 캐시에서 답한다.
 */
export interface TsumeSolve {
  id: number;
  /** 사람이 붙인 이름. 비어 있으면 화면이 결과를 이름 자리에 쓴다. */
  name: string;
  /** 풀기에 그대로 보낼 수 있는 국면. */
  sfen: string;
  status: TsumeResponse['status'];
  /** `mate` 일 때만 온다. */
  plies?: number;
  /** `plies` 보다 짧은 詰み가 없음을 확인했는가. 거짓이면 「N手詰め」로 단정하지 않는다. */
  shortest: boolean;
  solvedAt: string;
}

export interface TsumeSolveListResponse {
  solves: TsumeSolve[];
}

export class TsumeError extends Error {
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
}

/** `fallback` 은 서버가 사유를 말하지 않았을 때 쓰는 문구다. */
export async function solveTsume(
  sfen: string,
  fallback: string,
  signal: AbortSignal | null = null,
): Promise<TsumeResponse> {
  const res = await fetch('/api/tsume/solve', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sfen }),
    signal,
  });
  if (res.ok) return (await res.json()) as TsumeResponse;
  const err = (await res.json().catch(() => null)) as ApiError | null;
  throw new TsumeError(err?.error || 'internal', err?.message || fallback);
}
