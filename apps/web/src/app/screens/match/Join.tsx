import { useT } from '@/i18n';
import type { Room } from '@/protocol/match';
import { navigate } from '@/routes/router';

/**
 * 손님이 링크를 열었을 때 앉기 전에 보는 화면.
 *
 * 누르기 전에는 자리를 잡지 않는다. 앉는 순간 그 방은 정원이 차서 누구도 들어갈 수 없고 시계도
 * 돌기 시작한다. 링크를 잘못 누른 사람이 모르는 사이에 남의 방을 닫아 버리면 안 된다.
 */
export function Join({ room, onJoin }: { room: Room; onJoin: () => void }) {
  const t = useT();
  return (
    <div className="setup">
      <h2 className="setup__head">{t.match.join.head(room.hostName)}</h2>

      <p className="setup__caveat">
        <SeatText room={room} />
      </p>

      {/* 개입이 없다는 것을 먼저 말한다. 이 앱을 개입으로 알고 온 사람에게는 뜨지 않는
          것이 「고장」으로 읽힌다. */}
      <p className="setup__caveat">{t.match.join.noHelp}</p>

      <button type="button" className="btn btn--primary setup__start" onClick={onJoin}>
        {t.match.join.submit}
      </button>

      <button type="button" className="btn setup__guide" onClick={() => navigate({ name: 'game' })}>
        {t.match.join.cancel}
      </button>
    </div>
  );
}

/** 내 자리와 시간 규칙 한 줄. 강조 둘이 끼어 있어 문장을 앞뒤로 나눠 둔다. `Waiting` 도 쓴다. */
export function SeatText({ room }: { room: Room }) {
  const seat = useT().match.seat;
  return (
    <>
      {seat.before}
      <strong>{room.yourColor === 'b' ? seat.black : seat.white}</strong>
      {seat.middle}
      <strong>{seat.perMove}</strong>
      {seat.after}
    </>
  );
}
