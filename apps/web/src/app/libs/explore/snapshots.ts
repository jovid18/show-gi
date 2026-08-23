import type { ExploreSnapshot, ExploreSnapshotListResponse } from '@/protocol/explore';
import type { ApiError } from '@/protocol/review';

/**
 * 저장한 국면 넷 — 목록·저장·이름 고치기·삭제. 서버는 `explore_snapshots.go` 다.
 *
 * 검토 한 걸음(`exploreSend`)과 갈라 둔다. 이쪽은 엔진을 안 타므로 저쪽의 429·503이 없다.
 */

/**
 * 로그인이 없으면 이 표면 넷이 다 닫힌다. 검토 자체는 그 벽 밖이다(journal §100) —
 * 그래서 화면이 패널을 지우지 않고 한 줄만 남긴다(`Snapshots`).
 */
export class SignedOutError extends Error {}

/**
 * 기록이 없는 배포다. 저장은 DB에 매여 있어서 검토가 서 있어도 이 넷만 안 선다
 * (`server.go` 의 `snapshotsUnavailable`).
 *
 * 실패와 갈라 둔다. 다시 눌러서 열리는 자리가 아니라 **이 배포에 없는 기능**이고,
 * 붉은 알림과 「もう一度読み込む」를 세워 두면 눌러도 같은 답만 온다.
 */
export class UnavailableError extends Error {}

const FALLBACK_ERROR = '保存した局面を読み込めませんでした。';

/** 서버가 준 일본어를 그대로 올린다. 못 읽을 때만 우리 문구다 — 문구의 주인은 서버다. */
async function reject(res: Response, fallback: string): Promise<never> {
  if (res.status === 401) throw new SignedOutError();
  const err = (await res.json().catch(() => null)) as ApiError | null;
  if (err?.error === 'store_unavailable') throw new UnavailableError();
  throw new Error(err?.message || fallback);
}

export async function fetchSnapshots(signal: AbortSignal): Promise<ExploreSnapshot[]> {
  const res = await fetch('/api/explore/snapshots', { signal });
  if (!res.ok) return reject(res, FALLBACK_ERROR);
  return ((await res.json()) as ExploreSnapshotListResponse).snapshots;
}

/**
 * 지금 보고 있는 자리를 남긴다. 이름이 비면 서버가 하나 짓는다.
 *
 * 판을 안 보낸다. 手合割 id 와 수순이고, 합법성은 서버가 되짚어 확인한다 — 화면이
 * 규칙을 모르는 것은 여기서도 같다.
 */
export async function saveSnapshot(name: string, handicap: string, moves: readonly string[]): Promise<ExploreSnapshot> {
  const res = await fetch('/api/explore/snapshots', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, handicap, moves }),
  });
  if (!res.ok) return reject(res, 'この局面を保存できませんでした。');
  return (await res.json()) as ExploreSnapshot;
}

/** 이름만 고친다. 국면은 그대로다 — 서버도 그 칸만 쓴다. */
export async function renameSnapshot(id: number, name: string): Promise<void> {
  const res = await fetch(`/api/explore/snapshots/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  });
  if (!res.ok) await reject(res, '名前を変更できませんでした。');
}

export async function deleteSnapshot(id: number): Promise<void> {
  const res = await fetch(`/api/explore/snapshots/${id}`, { method: 'DELETE' });
  if (!res.ok) await reject(res, 'この局面を削除できませんでした。');
}
