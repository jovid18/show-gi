import { useCallback, useState } from 'react';

import type { ChosenResult, KifuImported, KifuPreview, KifuRequest } from '@/protocol/kifu';
import type { ApiError, MyColor } from '@/protocol/review';

/**
 * 취해 오기의 두 요청.
 *
 * 상태를 화면이 아니라 여기서 든다. 「읽는 중」과 「취해 오는 중」이 같은 버튼을 잠그고
 * 같은 자리에 오류를 그리므로, 갈라 두면 두 벌을 맞춰야 한다.
 *
 * 오류 문구는 서버가 만든 것을 그대로 쓴다. 서버가 「몇 手目를 못 읽었나」를 알고,
 * 화면이 그 문장을 다시 지으면 어휘가 두 벌이 된다.
 */
export type ImportPhase = 'idle' | 'reading' | 'importing';

interface State {
  phase: ImportPhase;
  preview: KifuPreview | null;
  error: string | null;
}

const initial: State = { phase: 'idle', preview: null, error: null };

/** 서버가 답을 안 준 자리. 통신이 끊긴 것과 500 이 같은 문장이다 — 사람이 할 일이 같다. */
const UNREACHABLE_JA = '棋譜を読み取れませんでした。しばらくしてからもう一度お試しください。';

async function post<T>(path: string, body: KifuRequest): Promise<T> {
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (res.ok) return (await res.json()) as T;

  // 서버가 문구를 만든다. 못 읽으면 우리 문장을 쓴다 — 빈 오류보다 낫다.
  let message = UNREACHABLE_JA;
  try {
    message = ((await res.json()) as ApiError).message || message;
  } catch {
    // 본문이 JSON이 아니다(프록시가 낸 502 같은 것). 위 기본 문구로 간다.
  }
  throw new Error(message);
}

export function useKifuImport() {
  const [state, setState] = useState<State>(initial);

  /** 읽기만 한다. 판도 안 만들고 엔진도 안 쓴다. */
  const read = useCallback(async (text: string) => {
    setState({ phase: 'reading', preview: null, error: null });
    try {
      const preview = await post<KifuPreview>('/api/kifu/parse', { text });
      setState({ phase: 'idle', preview, error: null });
    } catch (e) {
      setState({ phase: 'idle', preview: null, error: (e as Error).message });
    }
  }, []);

  /** 취해 온다. 성공하면 판 번호를 준다 — 화면이 그 자리에서 되짚기로 옮겨 간다. */
  const submit = useCallback(async (text: string, myColor: MyColor, result?: ChosenResult): Promise<number | null> => {
    setState((s) => ({ ...s, phase: 'importing', error: null }));
    try {
      // `result` 는 있을 때만 싣는다. `exactOptionalPropertyTypes` 라 undefined 를
      // 그대로 넘길 수 없고, 넘겨 봐야 JSON 에서 사라지는 값이다.
      const body: KifuRequest = result === undefined ? { text, myColor } : { text, myColor, result };
      const got = await post<KifuImported>('/api/kifu/import', body);
      setState((s) => ({ ...s, phase: 'idle' }));
      return got.gameId;
    } catch (e) {
      setState((s) => ({ ...s, phase: 'idle', error: (e as Error).message }));
      return null;
    }
  }, []);

  /** 원문이 바뀌면 미리보기는 그 판의 것이 아니다. 남겨 두면 남의 手数를 보고 취해 온다. */
  const reset = useCallback(() => setState(initial), []);

  return { ...state, read, submit, reset };
}
