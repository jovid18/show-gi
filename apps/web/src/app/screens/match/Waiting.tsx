import { useState } from 'react';

import { useT } from '@/i18n';
import type { MatchConnection } from '@/hooks/useMatch';
import type { Room } from '@/protocol/match';
import { SeatText } from './Join';
import { Unavailable } from './Unavailable';

/**
 * 상대가 들어오기 전의 화면.
 *
 * 여기서 할 일이 링크를 건네는 것 하나라 링크가 화면의 주인공이다. 대국은 둘이 다 붙어야
 * 시작되므로 판은 아직 그리지 않는다.
 */
export function Waiting({
  connection,
  room,
  roomId,
  rejection,
}: {
  connection: MatchConnection;
  room: Room | null;
  roomId: string;
  /** 서버가 말한 거절. 방이 걷혔을 때 그 이유가 여기로 온다. */
  rejection: string | null;
}) {
  const t = useT();
  const w = t.match.waiting;
  // 방 하나 받지 못하고 끊겼으면 앉지 못한 것이다. 앞의 확인(`fetchRoom`)을 통과했는데도
  // 그렇다면 그 사이에 남이 자리를 채운 것이고, 그 답은 「열 수 없다」 하나다.
  if (connection === 'closed' && !room) return <Unavailable />;

  if (!room) {
    return <p className="review-status">{w.connecting}</p>;
  }

  const url = `${window.location.origin}/rooms/${roomId}`;
  // 끊긴 것을 경고 없이 넘기지 않는다. 대국이 시작되기 전에도 끊길 수 있고(배포·네트워크·
  // 방 만료), 그대로 두면 이 화면이 이미 죽은 링크를 계속 광고한다(journal §83).
  const dropped = connection === 'closed';

  return (
    <div className="setup">
      <h2 className="setup__head">{room.waiting ? w.headWaiting : w.headStarting}</h2>

      {dropped && (
        <div className="match-lost" role="alert">
          {/* 서버가 이유를 말했으면 그것을 그대로 쓴다. 방이 걷힌 것과 그냥 끊긴 것은
              사람에게 다른 일이고, 앞은 다시 눌러도 열리지 않는다. */}
          <p>{rejection ?? w.dropped}</p>
          <button type="button" className="btn btn--primary" onClick={() => window.location.reload()}>
            {w.reconnect}
          </button>
        </div>
      )}

      {/* 끊긴 동안에는 링크를 그리지 않는다. 배너가 「이 방은 끝났다」고 말하는 옆에 복사
          버튼이 떠 있으면 그 링크를 보내는 사람이 생긴다. */}
      {dropped ? null : room.waiting ? (
        <>
          <p className="setup__caveat">{w.sendLink}</p>
          <InviteLink url={url} />
          <p className="setup__caveat">
            {/* 정원과 만료를 먼저 말한다. 「누구나 볼 수 있는 곳」에 붙여도 되는지가 그 두
                사실로 갈린다. */}
            {w.onlyOneBefore}
            <strong>{w.onlyOne}</strong>
            {w.onlyOneAfter}
          </p>
        </>
      ) : (
        // 방 주인에게 자기 이름을 말하지 않는다. 손님이 자리만 잡고 붙어 있지 않은 동안
        // 방 주인이 돌아오면 여기로 오는데, 그때 `hostName` 은 보고 있는 사람 자신이다.
        <p className="setup__caveat">{room.isHost ? w.hostAway : w.guestAway(room.hostName)}</p>
      )}

      <p className="setup__caveat">
        <SeatText room={room} />
      </p>

      {/* 대인전에는 개입이 없다고 미리 말한다. 이 앱을 개입으로 알고 온 사람에게는 뜨지
          않는 것이 「고장」으로 읽힌다. */}
      <p className="setup__caveat">{w.noHelp}</p>
    </div>
  );
}

/** 링크 한 줄과 복사 버튼. 복사가 막힌 환경이 있어 주소를 글자로도 보여준다. */
function InviteLink({ url }: { url: string }) {
  const w = useT().match.waiting;
  const [copied, setCopied] = useState(false);

  return (
    <div className="invite">
      <input className="invite__url" type="text" value={url} readOnly aria-label={w.inviteLabel} />
      <button
        type="button"
        className="btn btn--primary"
        onClick={() => {
          void navigator.clipboard
            .writeText(url)
            .then(() => {
              setCopied(true);
              window.setTimeout(() => setCopied(false), 2000);
            })
            // 복사가 막혀 있어도 위 입력칸에 주소가 있다.
            .catch(() => setCopied(false));
        }}
      >
        {copied ? w.copied : w.copy}
      </button>
    </div>
  );
}
