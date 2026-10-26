import { useT } from '@/i18n';
import { useQueue } from '@/hooks/useQueue';
import { useViewer } from '@/hooks/useViewer';
import { navigate } from '@/routes/router';

/**
 * 아무나와 두는 갈래. 대기열이다.
 *
 * 링크 방식(`Setup.tsx` 의 `FriendMatch`)과 갈리는 것이 셋이다. 手番을 고르지 않고(先手 랜덤),
 * 手合割도 없고(平手 확정), 짝이 잡히면 확인 화면 없이 그 방으로 들어간다(journal §92).
 *
 * 상대에 대해 아무것도 보여주지 않는다. 段級도 전적도 레이팅도 없다. 보여주면 사람이 그것을
 * 지키려 두기 시작한다(docs/01-core.md §5).
 */
export function Search() {
  const t = useT();
  const m = t.match.search;
  const { me } = useViewer();
  const { searching, waiting, waitedMs, error, start, stop } = useQueue((roomId) =>
    navigate({ name: 'room', id: roomId }),
  );

  // 로그인이라는 것이 이 배포에 없으면 자리를 아예 그리지 않는다(`FriendMatch` 와 같은 규칙).
  if (!me.enabled) return null;

  return (
    <div className="setup__friend">
      <h3 className="setup__legend">{m.legend}</h3>
      {me.user === null ? (
        <p className="setup__caveat">{m.signIn}</p>
      ) : (
        <>
          <p className="setup__caveat">
            {/* 정한 것 넷을 누르기 전에 말한다. 적어 두지 않으면 위에서 고른 것이 무시된
                것으로 읽힌다. */}
            {m.rulesBefore}
            <strong>{m.even}</strong>
            {m.rulesAfter}
          </p>

          {searching ? (
            <>
              <p className="review-status" aria-live="polite">
                {m.searching} <strong>{m.seconds(Math.floor(waitedMs / 1000))}</strong>
              </p>
              <p className="setup__caveat">
                {/* 「짝이 잡히지 않는 것」과 「고장」을 여기서 가른다. 동시에 기다리는
                    사람이 없으면 잡히지 않는 것을 그대로 받아들이기로 정했고(journal §92),
                    그 사실을 알려 줄 자리가 하나 필요하다. */}
                {waiting > 1 ? m.othersWaiting(waiting) : m.aloneWaiting}
              </p>
              <button type="button" className="btn setup__guide" onClick={stop}>
                {m.stop}
              </button>
            </>
          ) : (
            <button type="button" className="btn setup__start" onClick={start}>
              {m.start}
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
