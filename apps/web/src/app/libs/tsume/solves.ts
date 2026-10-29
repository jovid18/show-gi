import type { Messages } from '@/i18n';
import { SignedOutError, UnavailableError } from '@/libs/explore/snapshots';
import type { ApiError } from '@/protocol/review';
import type { TsumeSolve, TsumeSolveListResponse } from '@/protocol/tsume';

/**
 * 푼 詰め将棋의 기록 셋(목록·이름 고치기·삭제). 서버는 `tsume_solves.go` 다.
 *
 * 저장이 없다. 풀기(`solveTsume`)가 풀 때마다 서버에서 남긴다.
 *
 * 닫힌 사유는 검토의 저장 국면과 같은 두 갈래라 그 오류를 그대로 쓴다.
 */

async function reject(res: Response, fallback: string): Promise<never> {
  if (res.status === 401) throw new SignedOutError();
  const err = (await res.json().catch(() => null)) as ApiError | null;
  if (err?.error === 'store_unavailable') throw new UnavailableError();
  throw new Error(err?.message || fallback);
}

export async function fetchTsumeSolves(signal: AbortSignal, t: Messages): Promise<TsumeSolve[]> {
  const res = await fetch('/api/tsume/solves', { signal });
  if (!res.ok) return reject(res, t.position.solves.loadFailed);
  return ((await res.json()) as TsumeSolveListResponse).solves;
}

/** 이름만 고친다. 빈 이름도 받는다. 그때는 결과가 이름 자리에 선다. */
export async function renameTsumeSolve(id: number, name: string, t: Messages): Promise<void> {
  const res = await fetch(`/api/tsume/solves/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  });
  if (!res.ok) await reject(res, t.position.solves.renameFailed);
}

export async function deleteTsumeSolve(id: number, t: Messages): Promise<void> {
  const res = await fetch(`/api/tsume/solves/${id}`, { method: 'DELETE' });
  if (!res.ok) await reject(res, t.position.solves.removeFailed);
}
