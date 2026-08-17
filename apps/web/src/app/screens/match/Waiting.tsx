import { useState } from 'react';

import type { MatchConnection } from '@/hooks/useMatch';
import type { Room } from '@/protocol/match';
import { Unavailable } from './Unavailable';

/**
 * 상대가 들어오기 전의 화면.
 *
 * **여기서 할 일이 하나뿐이다 — 링크를 건네는 것.** 그래서 링크가 화면의 주인공이고,
 * 판은 아직 안 그린다(그릴 판이 없다: 판은 둘이 다 붙어야 선다).
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
  // 방 하나 못 받고 끊겼다는 것은 **앉지 못했다**는 뜻이다. 앞의 확인(`fetchRoom`)을
  // 통과했는데도 그렇다면 그 사이에 남이 자리를 채운 것이고, 그 답은 「열 수 없다」 하나다.
  if (connection === 'closed' && !room) return <Unavailable />;

  if (!room) {
    return <p className="review-status">対局部屋につないでいます…</p>;
  }

  const url = `${window.location.origin}/rooms/${roomId}`;
  // **끊긴 것을 조용히 넘기지 않는다.** 판이 서기 전에도 여기서 끊길 수 있는데
  // (배포·네트워크·방 만료) 그대로 두면 이 화면은 **이미 죽은 링크를 계속 광고한다** —
  // 상대가 열어도 404를 받는데 방 주인은 아직 기다리는 줄 안다.
  const dropped = connection === 'closed';

  return (
    <div className="setup">
      <h2 className="setup__head">{room.waiting ? '相手を待っています' : '対局をはじめます'}</h2>

      {dropped && (
        <div className="match-lost" role="alert">
          {/* 서버가 이유를 말했으면 그것을 그대로 쓴다 — 방이 걷힌 것과 그냥 끊긴 것은
              사람에게 다른 일이고, 앞은 다시 눌러도 안 된다. */}
          <p>{rejection ?? '接続が切れました。このリンクは今つながっていません。'}</p>
          <button type="button" className="btn btn--primary" onClick={() => window.location.reload()}>
            つなぎ直す
          </button>
        </div>
      )}

      {room.waiting ? (
        <>
          <p className="setup__caveat">下のリンクを相手に送ってください。相手が開くと、その場で対局がはじまります。</p>
          <InviteLink url={url} />
          <p className="setup__caveat">
            {/* **정원과 만료를 먼저 말한다.** 링크를 어디에 붙일지가 그 두 사실로 갈린다 —
                「누구나 볼 수 있는 곳」에 붙여도 되는지가 여기서 답이 난다. */}
            このリンクで入れるのは<strong>一人だけ</strong>
            です。二人そろうと、それ以外の人は開けなくなります。誰も入らないまま30分たつと、このリンクは使えなくなります。
          </p>
        </>
      ) : (
        <p className="setup__caveat">{room.hostName}さんの対局部屋です。相手がもどるのを待っています。</p>
      )}

      <p className="setup__caveat">
        あなたは<strong>{room.yourColor === 'b' ? '先手' : '後手'}</strong>です。持ち時間は<strong>一手60秒</strong>
        で、切れるとその場で負けになります。
      </p>

      {/* **대인전에는 개입이 없다고 미리 말한다.** 이 앱을 개입으로 알고 온 사람에게는
          그것이 「고장」으로 읽힌다 — 안 뜨는 것이 아니라 없는 것이다. */}
      <p className="setup__caveat">
        対人戦では、口出し（待ったの巻き戻し）もヒントも出ません。終わったあとに棋譜を振り返れます。
      </p>
    </div>
  );
}

/** 링크 한 줄과 복사 버튼. **주소를 글자로도 보여준다** — 복사가 막힌 환경이 있다. */
function InviteLink({ url }: { url: string }) {
  const [copied, setCopied] = useState(false);

  return (
    <div className="invite">
      <input className="invite__url" type="text" value={url} readOnly aria-label="招待リンク" />
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
            // 복사가 막혀 있어도 화면은 그대로 선다 — 위 입력칸에 주소가 있다.
            .catch(() => setCopied(false));
        }}
      >
        {copied ? 'コピーしました' : 'リンクをコピー'}
      </button>
    </div>
  );
}
