import type { Room } from '@/protocol/match';
import { navigate } from '@/routes/router';

/**
 * 손님이 링크를 열었을 때 앉기 전에 보는 화면.
 *
 * 누르기 전에는 자리를 안 잡는다. 앉는 순간 그 방은 정원이 차서 아무도 못 들어가고,
 * 그와 동시에 시계가 돌기 시작한다 — 링크를 잘못 눌렀거나 지금 둘 수 없는 사람이
 * 모르는 사이에 남의 방을 닫아 버리면 안 된다.
 */
export function Join({ room, onJoin }: { room: Room; onJoin: () => void }) {
  return (
    <div className="setup">
      <h2 className="setup__head">{room.hostName}さんと対局しますか</h2>

      <p className="setup__caveat">
        あなたは<strong>{room.yourColor === 'b' ? '先手' : '後手'}</strong>です。持ち時間は<strong>一手60秒</strong>
        で、切れるとその場で負けになります。
      </p>

      {/* 개입이 없다는 것을 먼저 말한다. 이 앱을 개입으로 알고 온 사람에게는 그것이
          안 뜨는 것이 「고장」으로 읽힌다 — 안 뜨는 게 아니라 없는 것이다. */}
      <p className="setup__caveat">
        対人戦では、口出し（悪手の巻き戻し）もヒントも待ったも出ません。終わったあとに棋譜を振り返れます。
      </p>

      <button type="button" className="btn btn--primary setup__start" onClick={onJoin}>
        対局に参加する
      </button>

      <button type="button" className="btn setup__guide" onClick={() => navigate({ name: 'game' })}>
        やめておく
      </button>
    </div>
  );
}
