import { useCallback, useEffect, useMemo, useRef, useState, type DragEvent } from 'react';

import { PositionEditor } from './PositionEditor';
import { SIGN_IN_PATH, type MeResponse } from '@/protocol/auth';
import {
  IMAGE_ACCEPT,
  PositionError,
  checkPosition,
  readImageFile,
  readPosition,
  type PositionFault,
} from '@/protocol/position';
import { parseSfen, toSfen, type Board as BoardModel } from '@/models/sfen';
import { navigate } from '@/routes/router';

/**
 * 「局面を読み取る」 — 판이 찍힌 그림을 올려 국면을 취해 오는 화면(journal §129).
 *
 * 세 걸음이다. 그림을 올리고, 읽어 낸 판을 사람이 확인해 고치고, 手番을 고른다.
 * 그 셋이 끝나면 국면이 검토의 주소가 되어(`routeExplore` 의 `s`) 이 화면을 떠난다 —
 * 남는 것이 주소 한 줄이라 새로고침에도 링크 공유에도 판이 살아 있다.
 *
 * **확인 걸음이 이 기능의 전부다.** 룰 엔진은 「그런 판은 없다」까지만 잡는다(二歩·玉 수·
 * 行き所のない駒·王手放置). 銀을 成銀으로 잘못 읽은 판은 여전히 합법적인 국면이라 아무
 * 코드도 안 걸리고, 그 자리의 검증자는 사람뿐이다 — 그래서 올린 그림을 판 옆에 그대로
 * 띄운다. 두 개를 나란히 놓지 않으면 사람이 무엇과 대조해야 하는지를 알 수 없다.
 *
 * **手番을 묻는다.** 사진은 그것을 말해 주지 않는다. 그리고 「先手か」가 아니라
 * 「あなたの手番か」로 묻는다 — 그림에서 알 수 있는 것은 아래쪽이 자기 편이라는 것뿐이고,
 * 그 아래쪽을 先手로 두는 정규화는 서버가 이미 했다(`internal/boardread`).
 *
 * **로그인이 필요하다.** 그림을 읽는 것이 돈을 쓰는 일이라 사람마다 세야 하고, 익명끼리는
 * 구별할 수단이 없다. 고치는 것과 분석하는 것에는 그 벽이 없다 — 그쪽은 룰 계산과
 * 엔진 슬롯이라 이미 익명에게 열려 있다.
 */
export function PositionScreen({ me }: { me: MeResponse }) {
  // 로그인 안 한 것은 오류가 아니다. 메뉴에서는 이 줄이 로그인한 사람에게만 보이지만
  // 주소를 직접 열면 익명으로 여기 선다 — 그때 상자를 그려 주면 사람이 그림을 고르고
  // 누른 뒤에야 로그인이 필요하다는 것을 알게 된다(ImportScreen 과 같은 자리).
  if (me.user === null) return <SignInFirst enabled={me.enabled} />;
  return <PositionForm />;
}

function SignInFirst({ enabled }: { enabled: boolean }) {
  return (
    <section className="import">
      <h1 className="import__title">局面を読み取る</h1>
      <p className="import__lead">
        将棋盤が写った画像を上げると、その局面を読み取って形勢と最善手を調べます。
        <br />
        画像の読み取りには回数の上限があるため、ログインが必要です。
      </p>
      {enabled && (
        <p className="profile__signin">
          <a className="btn btn--primary" href={SIGN_IN_PATH}>
            ログイン
          </a>
        </p>
      )}
    </section>
  );
}

function PositionForm() {
  /** 올린 그림. data URL 그대로 든다 — `<img src>` 와 요청 본문이 같은 값이다. */
  const [image, setImage] = useState<string | null>(null);
  const [reading, setReading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  /** 읽어 낸 판. 사람이 고치는 정본이 여기 하나다. */
  const [board, setBoard] = useState<BoardModel | null>(null);
  const [faults, setFaults] = useState<PositionFault[]>([]);
  const [warnings, setWarnings] = useState<string[]>([]);

  const sfen = useMemo(() => (board ? toSfen(board) : ''), [board]);

  /**
   * 판이 바뀌면 성립하는지를 다시 묻는다. 엔진도 로그인도 안 쓰는 자리라 한 칸을
   * 고칠 때마다 물어도 되고, 그래서 二歩가 되는 순간 그 자리에서 보인다.
   *
   * 늦게 온 답을 안 쓴다. 이어서 두 칸을 고치면 요청 둘이 겹치는데, 먼저 보낸 것이
   * 나중에 오면 화면이 한 걸음 전의 판정을 그린다 — 그러면 사람이 고친 것이 안 고쳐진
   * 것처럼 보인다.
   */
  const checkID = useRef(0);
  useEffect(() => {
    if (sfen === '') return;
    const mine = ++checkID.current;
    const controller = new AbortController();
    void checkPosition(sfen, controller.signal)
      .then((res) => {
        if (mine !== checkID.current) return;
        setFaults(res.faults);
        setWarnings(res.warnings);
      })
      .catch(() => {
        // 검사가 못 돌아도 판은 그린다. 여기서 사유를 비우면 「문제가 없다」로 읽혀서
        // 성립하지 않는 판이 분석으로 넘어간다 — 서버가 다시 거절하지만, 그때의 실패는
        // 고칠 자리가 없는 실패다. 그래서 직전 판정을 그대로 둔다.
      });
    return () => controller.abort();
  }, [sfen]);

  const read = useCallback(async (dataURL: string) => {
    setImage(dataURL);
    setBoard(null);
    setFaults([]);
    setWarnings([]);
    setError(null);
    setReading(true);
    try {
      const res = await readPosition(dataURL);
      setBoard(parseSfen(res.sfen));
      setFaults(res.faults);
      setWarnings(res.warnings);
    } catch (e) {
      setError(e instanceof PositionError ? e.message : '画像から局面を読み取れませんでした。');
    } finally {
      setReading(false);
    }
  }, []);

  const take = useCallback(
    async (file: File | null | undefined) => {
      if (!file) return;
      setError(null);
      try {
        await read(await readImageFile(file));
      } catch (e) {
        setError(e instanceof PositionError ? e.message : '画像を読み込めませんでした。');
      }
    },
    [read],
  );

  /**
   * 붙여 넣기로도 받는다. 이 기능에 오는 그림은 대개 방금 찍은 스크린샷이라, 파일로
   * 저장했다가 고르는 걸음을 없애는 것이 이 화면에서 가장 값싼 개선이다.
   *
   * `navigator.clipboard.read()` 가 아니라 `paste` 이벤트다. 저쪽은 Chromium 전용이고
   * 권한을 묻는데, 이쪽은 Safari·Firefox 에서도 그대로 된다.
   *
   * 창 전체에서 듣는다. 이 화면에 글자를 넣는 자리가 없어서 남의 붙여넣기를 가로챌
   * 일이 없고, 사람이 어디를 눌러 두었는지를 신경 쓰지 않아도 된다.
   */
  useEffect(() => {
    const onPaste = (e: ClipboardEvent): void => {
      const file = [...(e.clipboardData?.items ?? [])]
        .filter((i) => i.kind === 'file' && i.type.startsWith('image/'))
        .map((i) => i.getAsFile())
        .find((f): f is File => f !== null);
      if (file) void take(file);
    };
    window.addEventListener('paste', onPaste);
    return () => window.removeEventListener('paste', onPaste);
  }, [take]);

  /**
   * 끌어다 놓는 것도 받는다. 파일 고르기·붙여 넣기와 같은 자리로 흘러간다(`take`).
   *
   * `dragover` 에서 기본 동작을 막아야 한다. 안 막으면 브라우저가 그 그림을 탭에서
   * 통째로 열어 버려서, 사람이 만든 판이 그 자리에서 사라진다.
   */
  const [dragging, setDragging] = useState(false);
  const onDragOver = (e: DragEvent): void => {
    e.preventDefault();
    setDragging(true);
  };
  const onDrop = (e: DragEvent): void => {
    e.preventDefault();
    setDragging(false);
    void take(e.dataTransfer.files[0]);
  };

  const faultSquares = useMemo(
    () => new Set(faults.map((f) => f.square).filter((s): s is number => s !== undefined)),
    [faults],
  );

  /** 사유가 하나라도 있으면 분석으로 안 넘어간다. 서버도 같은 문으로 거절한다. */
  const analyzable = board !== null && faults.length === 0;

  const analyze = (): void => {
    if (!analyzable) return;
    navigate({ name: 'explore', handicap: '', moves: [], sfen });
  };

  const setTurn = (turn: 'black' | 'white'): void => {
    if (board) setBoard({ ...board, turn });
  };

  return (
    <section
      className="import position"
      data-dragging={dragging || undefined}
      onDragOver={onDragOver}
      onDragLeave={() => setDragging(false)}
      onDrop={onDrop}
    >
      <h1 className="import__title">局面を読み取る</h1>
      <p className="import__lead">
        将棋盤が写った画像を上げてください。読み取った局面をあなたが確かめてから、形勢と最善手を調べます。
        <br />
        {/* 실측에서 판이 화면의 절반인 방송 캡처가 크게 틀렸다(journal §129). 자르면
            나아지는지는 아직 안 쟀지만, 사람에게 시키는 값이 작아서 먼저 권한다. */}
        盤のまわりを切り取って、盤と駒台だけが写るようにすると読み取りやすくなります。
        <br />
        画像は解析のあと保存されません。
      </p>

      <div className="import__row">
        <label className="import__button">
          画像を選ぶ
          <input
            className="import__file"
            type="file"
            accept={IMAGE_ACCEPT}
            onChange={(e) => void take(e.target.files?.[0])}
          />
        </label>
        <span className="import__filename">
          スクリーンショットをそのまま貼り付け（⌘V）ても、ここにドラッグしてもかまいません。
        </span>
      </div>

      {error !== null && <p className="position__error">{error}</p>}
      {reading && <p className="review-status">画像から局面を読み取っています…</p>}

      {image !== null && board !== null && (
        <>
          <div className="position__compare">
            {/* 올린 그림을 판 옆에 그대로 둔다. 룰 엔진이 못 잡는 오독(銀↔成銀·駒の向き)의
                검증자가 사람이라, 대조할 원본이 화면에 없으면 확인이라는 걸음 자체가
                성립하지 않는다. */}
            <figure className="position__shot">
              <img src={image} alt="上げた画像" />
              <figcaption>上げた画像</figcaption>
            </figure>
            <div className="position__read">
              <h2 className="position__subtitle">読み取った局面</h2>
              <p className="import__label">ちがうマスを押すと駒を直せます。下があなたの側です。</p>
              <PositionEditor board={board} faults={faultSquares} onChange={setBoard} />
            </div>
          </div>

          {/* 手番. 사진이 말해 주지 않는 유일한 값이라 반드시 사람이 고른다. */}
          <fieldset className="position__turn">
            <legend className="import__label">この画像は、どちらの手番ですか</legend>
            <label>
              <input type="radio" name="turn" checked={board.turn === 'black'} onChange={() => setTurn('black')} />
              あなたの手番
            </label>
            <label>
              <input type="radio" name="turn" checked={board.turn === 'white'} onChange={() => setTurn('white')} />
              相手の手番
            </label>
          </fieldset>

          {faults.length > 0 && (
            <ul className="position__faults">
              {faults.map((f, i) => (
                <li key={`${f.reason}-${f.square ?? i}`}>{f.message}</li>
              ))}
            </ul>
          )}
          {warnings.map((w) => (
            <p className="position__warning" key={w}>
              {w}
            </p>
          ))}

          <div className="import__row">
            <button type="button" className="btn btn--primary" disabled={!analyzable} onClick={analyze}>
              この局面を解析する
            </button>
            {!analyzable && <span className="import__filename">局面を直すと解析できます。</span>}
          </div>
        </>
      )}
    </section>
  );
}
