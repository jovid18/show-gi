import type { ApiError } from '@/protocol/review';

/** 서버의 `boardread.MaxImage` 와 같아야 한다. */
export const MAX_IMAGE_BYTES = 6 * 1024 * 1024;

export const IMAGE_ACCEPT = 'image/png,image/jpeg,image/webp';

export interface PositionFault {
  /** 사유별로 처리를 나눌 때는 `message` 가 아니라 이 값을 본다. */
  reason: string;
  /** `parseSfen` 이 만드는 `squares` 의 인덱스(0~80)다. USI 좌표가 아니다. */
  square?: number;
  message: string;
}

/**
 * `faults` 가 있으면 분석으로 넘어가지 않는다. `warnings` 는 막지 않는다. 駒가 모자라거나 이미
 * 詰んでいる 국면도 분석할 수 있다.
 */
export interface PositionResponse {
  sfen: string;
  faults: PositionFault[];
  warnings: string[];
  /** 서버가 그림을 모으고 있을 때만 온다. 사람이 확인한 판과 함께 `saveLabel` 로 돌려보낸다. */
  imageId?: string;
}

export type PositionErrorCode =
  | 'unauthorized'
  | 'quota'
  | 'unavailable'
  | 'too_large'
  | 'not_image'
  | 'no_board'
  | 'read_failed';

export class PositionError extends Error {
  readonly code: PositionErrorCode;

  constructor(code: PositionErrorCode, message: string) {
    super(message);
    this.code = code;
  }
}

export async function readPosition(image: string, signal: AbortSignal | null = null): Promise<PositionResponse> {
  const res = await fetch('/api/position/read', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ image }),
    signal,
  });
  return unwrap(res);
}

export async function checkPosition(sfen: string, signal: AbortSignal | null = null): Promise<PositionResponse> {
  const res = await fetch('/api/position/check', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sfen }),
    signal,
  });
  return unwrap(res);
}

export async function saveLabel(imageId: string, sfen: string): Promise<void> {
  try {
    await fetch('/api/position/label', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ imageId, sfen }),
    });
  } catch {
    // 실패를 알리지 않는다. 사람이 요청한 것은 분석이라, 여기서 막으면 분석이 실패한 것처럼 보인다.
  }
}

async function unwrap(res: Response): Promise<PositionResponse> {
  if (res.ok) return (await res.json()) as PositionResponse;
  const err = (await res.json().catch(() => null)) as ApiError | null;
  throw new PositionError(
    (err?.error as PositionErrorCode) || 'read_failed',
    err?.message || '画像から局面を読み取れませんでした。',
  );
}

/**
 * 크기는 파일을 읽기 전에 확인한다. 읽은 뒤에 막으면 큰 파일을 메모리에 올리는 동안 탭이 멈춘다.
 */
export async function readImageFile(file: File): Promise<string> {
  if (file.size > MAX_IMAGE_BYTES) {
    throw new PositionError('too_large', '画像が大きすぎます。6MB までのスクリーンショットをお使いください。');
  }
  const bytes = new Uint8Array(await file.arrayBuffer());

  // 나눠서 변환한다. `String.fromCharCode(...bytes)` 를 한 번에 부르면 몇 MB짜리 그림에서 호출
  // 스택이 넘친다.
  let binary = '';
  for (let i = 0; i < bytes.length; i += CHUNK) {
    binary += String.fromCharCode(...bytes.subarray(i, i + CHUNK));
  }

  const mime = file.type === '' ? 'image/png' : file.type;
  return `data:${mime};base64,${btoa(binary)}`;
}

const CHUNK = 0x8000;
