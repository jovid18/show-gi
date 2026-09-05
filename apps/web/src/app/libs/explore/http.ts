import type { ExploreNode } from '@/protocol/explore';
import type { ApiError } from '@/protocol/review';
import type { Send } from '@/hooks/useWhatIf';

/**
 * 검토 한 걸음은 요청/응답이다. 되짚기와 같은 길이고(libs/whatif/http.ts) 갈리는 것은
 * 뿌리를 무엇으로 말하느냐뿐 — 저쪽은 판 번호와 手数, 여기는 手合割 id 또는 판이다.
 *
 * 뿌리를 하나만 싣는다. 판이 있으면 手合割을 안 보낸다 — 서버가 둘을 같이 받으면
 * 거절하고(`bad_root`), 그 거절은 화면이 만들 수 있는 실패다.
 *
 * `req.ply` 를 안 보낸다. 검토의 뿌리는 언제나 0手目라 그 값이 상수이고, 훅이 그것을
 * 열쇠에 넣어 캐시를 거는 데만 쓴다(`useWhatIf` 의 `keyOf`).
 */
export function exploreSend(handicap: string, sfen = ''): Send<ExploreNode> {
  return async (req, signal): Promise<ExploreNode> => {
    const root = sfen ? { sfen } : { handicap };
    const res = await fetch('/api/explore', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...root, moves: req.moves }),
      signal,
    });
    if (!res.ok) {
      // 서버가 이유를 일본어로 준다(explore.go 와 whatifMessages). 못 읽을 때만 우리 문구다.
      const err = (await res.json().catch(() => null)) as ApiError | null;
      throw new Error(err?.message || 'この手順を試せませんでした。');
    }
    return (await res.json()) as ExploreNode;
  };
}
