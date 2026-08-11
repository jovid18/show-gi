import type { ApiError } from '@/protocol/review';
import type { WhatIfNode, WhatIfRequest } from '@/protocol/whatif';
import type { Send } from '@/hooks/useWhatIf';

/**
 * 끝난 판의 가정 수순은 **요청/응답**이다.
 *
 * 대국 중에는 같은 것을 그 대국의 WebSocket으로 묻는다(useGame). 길이 갈리는 이유는
 * **뿌리를 어디서 얻느냐**다 — 여기는 DB 기록이고, 두는 중인 판은 세션이 방금 보낸
 * 스냅샷이다. 기록은 비동기로 쌓이므로 개입 직후에는 마지막 수가 아직 없을 수 있다.
 */
export function httpSend(gameId: number): Send {
  return async (req: WhatIfRequest, signal: AbortSignal): Promise<WhatIfNode> => {
    const res = await fetch(`/api/games/${gameId}/whatif`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(req),
      signal,
    });
    if (!res.ok) {
      // 서버가 이유를 일본어로 준다(whatif.go). 못 읽을 때만 우리 문구를 쓴다.
      const err = (await res.json().catch(() => null)) as ApiError | null;
      throw new Error(err?.message || 'この手順を試せませんでした。');
    }
    return (await res.json()) as WhatIfNode;
  };
}
