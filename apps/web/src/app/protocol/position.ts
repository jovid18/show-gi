// 사진에서 국면을 가져오는 API 계약이다. 서버 쪽 짝은 `internal/server/position.go` 다.
//
// 읽기와 검사 두 경로가 같은 응답 형태를 쓴다. 읽기는 국면을 만들고 검사는 「이 국면이
// 성립하는가」를 본다. 그래서 확인 화면이 「방금 읽은 판」과 「내가 고친 판」을 같은 코드로
// 표시한다(journal §129).

import type { ApiError } from '@/protocol/review';

/** 읽기에 넣는 그림의 크기 상한이다. 서버 `boardread.MaxImage` 와 같아야 한다. */
export const MAX_IMAGE_BYTES = 6 * 1024 * 1024;

/**
 * 허용하는 파일 형식이다. 파일 선택 창을 거르는 데만 쓰고, 서버가 파일 앞부분으로 다시
 * 확인한다(`boardread.imageMIME`).
 */
export const IMAGE_ACCEPT = 'image/png,image/jpeg,image/webp';

/** 국면이 어긴 규칙 하나다. */
export interface PositionFault {
  /** 사유의 영어 이름이다(`nifu`·`check ignored`…). 화면이 분기해야 하면 이 값을 본다. */
  reason: string;
  /**
   * 화면 배열 인덱스이고 범위는 0~80이다. 駒 수·玉 수처럼 칸을 정할 수 없는 사유면 오지
   * 않는다.
   *
   * 서버와 좌표 규약이 같다. `parseSfen` 의 `squares` 색인이 서버 칸 번호와
   * 같다(`internal/shogi` 패키지 doc).
   */
  square?: number;
  /** 화면에 그대로 표시하는 일본어다. */
  message: string;
}

/**
 * 국면 하나와 그 국면에 대한 룰 엔진의 검사 결과다.
 *
 * `faults` 가 비어야 분석으로 넘어갈 수 있다. `warnings` 는 막지 않는다. 駒가 몇 장
 * 모자라거나 이미 詰んでいる 국면이 여기에 해당하고, 둘 다 분석할 수 있다.
 */
export interface PositionResponse {
  sfen: string;
  faults: PositionFault[];
  warnings: string[];
  /**
   * 저장한 그림의 이름이다(`board-01`). 그림 수집 폴더가 켜져 있을 때만 온다.
   *
   * 화면이 들고 있다가 「解析する」를 누르면 돌려보낸다. 그때 사람이 고친 판을 이 그림의
   * 라벨로 저장한다(`saveLabel`). 값이 오지 않으면 이 단계를 건너뛴다.
   */
  imageId?: string;
}

/**
 * 실패 사유 코드다. 화면이 「다시 눌러 보라」와 「그림을 바꿔라」를 구분해 안내하는 데 쓴다.
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
 * 그림 한 장에서 국면을 읽어 달라고 요청한다. 그림은 base64로 보내고, 서버는 저장하지 않고
 * 응답을 만든 뒤 버린다.
 *
 * `signal` 의 기본값은 `null` 이다. `undefined` 는 `exactOptionalPropertyTypes` 에서
 * `RequestInit.signal` 에 대입할 수 없다. 사람이 한 번 눌러 보내는 요청이라 중단할 지점도
 * 없다.
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
 * 국면이 성립하는지 검사한다. 엔진과 로그인을 쓰지 않으므로 칸 하나를 고칠 때마다 불러도
 * 된다.
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
 * 사람이 확인한 국면을 그 그림의 라벨로 저장한다(journal §129). 판독 측정용 그림을 모을 때만
 * 실행한다.
 *
 * 서버 폴더가 꺼져 있으면 `imageId` 가 오지 않고, 호출하는 쪽이 이 함수를 부르지 않는다.
 */
export async function saveLabel(imageId: string, sfen: string): Promise<void> {
  try {
    await fetch('/api/position/label', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ imageId, sfen }),
    });
  } catch {
    // 실패를 무시한다. 사람이 요청한 것은 「이 국면을 분석해라」다. 여기서 막으면 분석할 수
    // 없는 것처럼 보인다.
  }
}

async function unwrap(res: Response): Promise<PositionResponse> {
  if (res.ok) return (await res.json()) as PositionResponse;
  // 실패 이유 일본어는 서버가 준다(position.go 의 boardReadMessages). 응답을 읽을 수 없을
  // 때만 이 파일의 문구를 쓴다.
  const err = (await res.json().catch(() => null)) as ApiError | null;
  throw new PositionError(
    (err?.error as PositionErrorCode) || 'read_failed',
    err?.message || '画像から局面を読み取れませんでした。',
  );
}

/**
 * 파일 하나를 base64 data URL 로 읽는다. 결과 하나를 `<img src>` 와 요청 본문에 함께 쓴다.
 *
 * 크기는 읽기 전에 검사한다. 읽은 뒤에 막으면 브라우저가 파일 전체를 메모리에 올리고, 큰
 * 파일은 그동안 탭이 멈춘다(`ImportScreen` 과 같은 판단).
 */
export async function readImageFile(file: File): Promise<string> {
  if (file.size > MAX_IMAGE_BYTES) {
    throw new PositionError('too_large', '画像が大きすぎます。6MB までのスクリーンショットをお使いください。');
  }
  const bytes = new Uint8Array(await file.arrayBuffer());

  // 조금씩 나눠 변환한다. `String.fromCharCode(...bytes)` 는 바이트 수만큼 인자를 펼쳐 수 MB
  // 그림에서 호출 스택을 넘는다.
  let binary = '';
  for (let i = 0; i < bytes.length; i += CHUNK) {
    binary += String.fromCharCode(...bytes.subarray(i, i + CHUNK));
  }

  // MIME 이름은 화면 표시에만 쓴다. 서버는 이 이름을 믿지 않고 파일 앞부분을 직접
  // 확인한다(`boardread.imageMIME`).
  const mime = file.type === '' ? 'image/png' : file.type;
  return `data:${mime};base64,${btoa(binary)}`;
}

/** 한 번에 변환하는 바이트 수다. 스택을 넘지 않는 크기면 되고 값 자체에 의미는 없다. */
const CHUNK = 0x8000;
