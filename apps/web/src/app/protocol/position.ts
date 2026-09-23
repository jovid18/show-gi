// 사진에서 국면을 가져오는 API 의 계약. 서버의 `internal/server/position.go` 와 짝이다.
//
// 두 엔드포인트가 같은 형태로 응답한다. 판독은 국면을 하나 만들고, 검사는 「이 국면이
// 성립하는가」에 답한다. 그래서 확인 화면이 「방금 읽은 판」과 「내가 고친 판」을 같은 코드로
// 그린다(journal §129).

import type { ApiError } from '@/protocol/review';

/** 판독에 보낼 그림의 크기 상한. 서버의 `boardread.MaxImage` 와 같아야 한다. */
export const MAX_IMAGE_BYTES = 6 * 1024 * 1024;

/**
 * 받는 파일 형식. 서버가 파일 앞부분을 보고 다시 확인하므로(`boardread.imageMIME`), 여기서는
 * 파일 선택 창의 목록을 좁히는 데만 쓴다.
 */
export const IMAGE_ACCEPT = 'image/png,image/jpeg,image/webp';

/** 국면이 어긴 규칙 하나. */
export interface PositionFault {
  /**
   * 사유의 영어 이름(`nifu`·`check ignored`…). 화면이 사유별로 처리를 나눌 일이 생기면 이
   * 값을 본다.
   */
  reason: string;
  /**
   * 화면 배열의 인덱스(0~80). 駒 수·玉 수처럼 특정 칸을 가리킬 수 없는 사유면 오지 않는다.
   *
   * 좌표 규약은 서버와 같다. `parseSfen` 이 만드는 `squares` 의 인덱스가 곧 서버의 칸
   * 번호다(`internal/shogi` 패키지 doc).
   */
  square?: number;
  /** 화면에 그대로 표시하는 일본어. 화면은 문장을 만들지 않는다. */
  message: string;
}

/**
 * 국면 하나와, 그 국면에 대한 룰 엔진의 검사 결과 전부.
 *
 * `faults` 가 비어 있어야 분석으로 넘어간다. `warnings` 는 막지 않는다. 駒가 몇 장 모자라거나
 * 이미 詰んでいる 국면이 여기에 해당하고, 둘 다 그대로 분석할 수 있다.
 */
export interface PositionResponse {
  sfen: string;
  faults: PositionFault[];
  warnings: string[];
  /**
   * 저장해 둔 그림의 이름(`board-01`). 그림 수집 폴더가 켜져 있을 때만 온다.
   *
   * 화면은 이 값을 들고 있다가 「解析する」를 누를 때 서버에 돌려보낸다. 그때 사람이 고친
   * 판이 이 그림의 라벨이 된다(`saveLabel`). 값이 오지 않으면 이 단계를 건너뛴다.
   */
  imageId?: string;
}

/**
 * 실패 사유 코드. 화면은 이 값으로 「다시 시도해 보라」와 「다른 그림을 써라」를 구분해
 * 안내한다.
 */
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
 * 그림 한 장을 보내 국면으로 판독한다.
 *
 * 그림은 base64 로 보낸다. 서버는 그림을 어디에도 저장하지 않고, 응답을 만든 뒤 버린다.
 *
 * `signal` 의 기본값은 `null` 이다. `exactOptionalPropertyTypes` 에서는 `undefined` 를
 * `RequestInit.signal` 에 넣을 수 없다. 사람이 버튼을 눌러 한 번 보내는 요청은 중간에 취소할
 * 일도 없다.
 */
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
 * 이 국면이 성립하는가. 엔진도 로그인도 필요 없으므로 칸을 하나 고칠 때마다 호출해도 된다.
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

/**
 * 사람이 확인한 국면을 그 그림의 라벨로 저장한다(journal §129).
 *
 * 판독 정확도를 잴 그림을 모을 때만 쓴다. 서버의 수집 폴더가 꺼져 있으면 `imageId` 가 오지
 * 않으므로, 호출하는 쪽이 아예 부르지 않는다.
 */
export async function saveLabel(imageId: string, sfen: string): Promise<void> {
  try {
    await fetch('/api/position/label', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ imageId, sfen }),
    });
  } catch {
    // 라벨 저장이 실패해도 막지 않는다. 사람이 요청한 것은 이 국면의 분석이다. 여기서 막으면
    // 분석이 실패한 것처럼 보인다.
  }
}

async function unwrap(res: Response): Promise<PositionResponse> {
  if (res.ok) return (await res.json()) as PositionResponse;
  // 실패 사유는 서버가 일본어로 보낸다(position.go 의 boardReadMessages). 응답을 읽지 못했을
  // 때만 여기서 정한 문구를 쓴다.
  const err = (await res.json().catch(() => null)) as ApiError | null;
  throw new PositionError(
    (err?.error as PositionErrorCode) || 'read_failed',
    err?.message || '画像から局面を読み取れませんでした。',
  );
}

/**
 * 파일 하나를 base64 data URL 로 읽는다. 이 값 하나를 `<img src>` 와 요청 본문에 같이 쓴다.
 *
 * 크기는 읽기 전에 확인한다. 읽은 뒤에 막으면 브라우저가 이미 파일 전체를 메모리에 올린 뒤다.
 * 큰 파일이면 그사이에 탭이 멈춘다(`ImportScreen` 과 같은 판단).
 */
export async function readImageFile(file: File): Promise<string> {
  if (file.size > MAX_IMAGE_BYTES) {
    throw new PositionError('too_large', '画像が大きすぎます。6MB までのスクリーンショットをお使いください。');
  }
  const bytes = new Uint8Array(await file.arrayBuffer());

  // 조금씩 나눠 변환한다. `String.fromCharCode(...bytes)` 는 바이트 수만큼 인자를 펼치므로,
  // 몇 MB짜리 그림이면 호출 스택이 넘친다.
  let binary = '';
  for (let i = 0; i < bytes.length; i += CHUNK) {
    binary += String.fromCharCode(...bytes.subarray(i, i + CHUNK));
  }

  // MIME 형식은 그림을 화면에 표시하는 데만 쓴다. 서버는 이 값을 믿지 않고 파일 앞부분을 직접
  // 확인한다(`boardread.imageMIME`).
  const mime = file.type === '' ? 'image/png' : file.type;
  return `data:${mime};base64,${btoa(binary)}`;
}

/**
 * 한 번에 변환하는 바이트 수. 스택이 넘치지 않을 만큼이면 되고, 이 숫자 자체에 의미는 없다.
 */
const CHUNK = 0x8000;
