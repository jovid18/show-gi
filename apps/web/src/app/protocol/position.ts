// 사진에서 국면을 취해 오는 표면의 계약. 서버의 `internal/server/position.go` 와 짝이다.
//
// 두 뿌리가 같은 모양을 준다 — 읽기가 국면 하나를 만들고, 검사가 「이 국면이 성립하는가」에
// 답한다. 확인 화면이 「방금 읽은 판」과 「내가 고친 판」을 같은 코드로 그리는 자리라
// 갈라 두지 않는다(journal §129).

import type { ApiError } from '@/protocol/review';

/** 읽기가 받는 그림의 크기 상한. 서버의 `boardread.MaxImage` 와 같아야 한다. */
export const MAX_IMAGE_BYTES = 6 * 1024 * 1024;

/**
 * 받는 파일 형식. 서버가 앞머리로 다시 확인하므로(`boardread.imageMIME`) 여기는
 * 파일 고르기 창을 좁히는 용도다.
 */
export const IMAGE_ACCEPT = 'image/png,image/jpeg,image/webp';

/** 국면이 어긴 규칙 하나. */
export interface PositionFault {
  /** 사유의 영어 이름(`nifu`·`check ignored`…). 화면이 분기할 자리가 생기면 이것을 본다. */
  reason: string;
  /**
   * 화면 배열 인덱스(0~80). 칸으로 짚을 수 없는 사유(말 수·玉 수)면 안 온다.
   *
   * 서버와 같은 좌표 규약이다 — `parseSfen` 이 만드는 `squares` 의 색인이 그대로
   * 서버의 칸 번호다(`internal/shogi` 패키지 doc).
   */
  square?: number;
  /** 화면에 그대로 나가는 일본어. 화면이 문장을 만들지 않는다. */
  message: string;
}

/**
 * 국면 하나와, 그것에 대해 룰 엔진이 말할 수 있는 전부.
 *
 * `faults` 가 비어 있어야 분석으로 넘어갈 수 있다. `warnings` 는 거절이 아니다 —
 * 말이 몇 장 모자라거나 이미 詰んでいる 국면이고, 둘 다 그대로 분석할 수 있다.
 */
export interface PositionResponse {
  sfen: string;
  faults: PositionFault[];
  warnings: string[];
}

/** 실패의 사유 코드. 화면이 「다시 눌러 보라」와 「그림을 바꿔라」를 갈라 말한다. */
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

/**
 * 그림 한 장을 국면으로 읽힌다.
 *
 * 그림을 base64 로 실어 보낸다. 서버는 그것을 어디에도 안 남기고, 응답을 만든 뒤 버린다.
 */
// `signal` 이 `null` 을 받는다. `undefined` 는 `exactOptionalPropertyTypes` 에서
// `RequestInit.signal` 에 못 들어가고, 사람이 누른 한 번은 끊을 자리가 없다.
export async function readPosition(image: string, signal: AbortSignal | null = null): Promise<PositionResponse> {
  const res = await fetch('/api/position/read', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ image }),
    signal,
  });
  return unwrap(res);
}

/**
 * 이 국면이 성립하는가. 엔진도 로그인도 안 쓰는 자리라 한 칸을 고칠 때마다 물어도 된다.
 */
export async function checkPosition(sfen: string, signal: AbortSignal | null = null): Promise<PositionResponse> {
  const res = await fetch('/api/position/check', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sfen }),
    signal,
  });
  return unwrap(res);
}

async function unwrap(res: Response): Promise<PositionResponse> {
  if (res.ok) return (await res.json()) as PositionResponse;
  // 서버가 이유를 일본어로 준다(position.go 의 boardReadMessages). 못 읽을 때만 우리 문구다.
  const err = (await res.json().catch(() => null)) as ApiError | null;
  throw new PositionError(
    (err?.error as PositionErrorCode) || 'read_failed',
    err?.message || '画像から局面を読み取れませんでした。',
  );
}

/**
 * 파일 하나를 base64 data URL 로 읽는다. 그 한 값이 `<img src>` 이면서 요청 본문이다.
 *
 * 크기를 읽기 전에 본다. 읽고 나서 막으면 브라우저가 그 파일을 통째로 메모리에 올린
 * 뒤이고, 큰 파일에서는 그 사이에 탭이 멈춘다(`ImportScreen` 과 같은 판단).
 */
export async function readImageFile(file: File): Promise<string> {
  if (file.size > MAX_IMAGE_BYTES) {
    throw new PositionError('too_large', '画像が大きすぎます。6MB までのスクリーンショットをお使いください。');
  }
  const bytes = new Uint8Array(await file.arrayBuffer());

  // 조각내어 옮긴다. `String.fromCharCode(...bytes)` 는 인자를 바이트 수만큼 펼치므로
  // 몇 MB짜리 그림에서 호출 스택이 넘친다 — 큰 스크린샷에서만 터지는 고장이라 손으로
  // 시험하면 안 만난다.
  let binary = '';
  for (let i = 0; i < bytes.length; i += CHUNK) {
    binary += String.fromCharCode(...bytes.subarray(i, i + CHUNK));
  }

  // 형식 이름은 그림을 화면에 그리는 데만 쓴다. 서버는 이 이름을 안 믿고 앞머리를
  // 직접 본다(`boardread.imageMIME`).
  const mime = file.type === '' ? 'image/png' : file.type;
  return `data:${mime};base64,${btoa(binary)}`;
}

/** 한 번에 옮기는 바이트 수. 스택이 넘치지 않는 크기면 되고, 값 자체에 뜻은 없다. */
const CHUNK = 0x8000;
