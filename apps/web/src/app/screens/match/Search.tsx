import { useQueue } from '@/hooks/useQueue';
import { useViewer } from '@/hooks/useViewer';
import { navigate } from '@/routes/router';

/**
 * 아무나와 두는 갈래 — 대기열.
 *
 * 링크 방식(`Setup.tsx` 의 `FriendMatch`)과 갈리는 것이 셋이다. 手番을 안 고르고
 * (先手 랜덤), 手合割도 없고(平手 확정), 짝이 잡히면 확인 화면 없이 그 방으로 들어간다 —
 * 스스로 줄에 선 사람에게는 「자리를 태워도 되나」를 물을 이유가 없다(journal §92).
 *
 * 상대에 대해 아무것도 안 보여준다. 段級도 전적도 레이팅도 없다 — 레이팅은 밴드를
 * 세우는 내부 값이고, 보여주면 사람이 그것을 지키려 두기 시작한다(docs/01-core.md §5).
 */
export function Search() {
  const { me } = useViewer();
  const { searching, waiting, waitedMs, error, start, stop } = useQueue((roomId) =>
    navigate({ name: 'room', id: roomId }),
  );

  // 로그인이라는 것이 이 배포에 없으면 자리를 아예 안 그린다(`FriendMatch` 와 같은 규칙).
  if (!me.enabled) return null;

  return (
    <div className="setup__friend">
      <h3 className="setup__legend">誰かと対局</h3>
      {me.user === null ? (
        <p className="setup__caveat">誰かと指すにはログインが必要です。</p>
      ) : (
        <>
          <p className="setup__caveat">
            {/* 정한 것 넷을 누르기 전에 말한다. 手番·手合割을 못 고르는 것은 이 갈래의
                성질이고, 안 적어 두면 위에서 고른 것이 안 먹는 것으로 읽힌다. */}
            近い実力の相手を探します。<strong>平手</strong>で、先手・後手はランダムです。持ち時間は一手60秒で、
            対人戦では口出しもヒントも出ません。
          </p>

          {searching ? (
            <>
              <p className="review-status" aria-live="polite">
                相手を探しています… <strong>{Math.floor(waitedMs / 1000)}秒</strong>
              </p>
              <p className="setup__caveat">
                {/* 「안 잡히는 것」과 「고장」을 여기서 가른다. 동시에 기다리는 사람이
                    없으면 안 잡히는 것을 그대로 받아들이기로 정했고(journal §92), 그러면
                    사람에게 그 사실을 알려 줄 자리가 하나 필요하다. */}
                {waiting > 1
                  ? `いま${waiting}人が待っています。待つほど相手の範囲が広がります。`
                  : 'いま待っているのはあなただけです。相手が来るまでこのままお待ちください。'}
              </p>
              <button type="button" className="btn setup__guide" onClick={stop}>
                さがすのをやめる
              </button>
            </>
          ) : (
            <button type="button" className="btn setup__start" onClick={start}>
              相手をさがす
            </button>
          )}

          {error !== null && (
            <p className="rejection" role="alert">
              {error}
            </p>
          )}
        </>
      )}
    </div>
  );
}
