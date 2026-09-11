import { useCallback, useEffect, useMemo, useRef, useState, type DragEvent } from 'react';

import { PositionEditor } from './PositionEditor';
import { SIGN_IN_PATH, type MeResponse } from '@/protocol/auth';
import {
  IMAGE_ACCEPT,
  PositionError,
  checkPosition,
  readImageFile,
  readPosition,
  saveLabel,
  type PositionFault,
} from '@/protocol/position';
import { parseSfen, toSfen, type Board as BoardModel } from '@/models/sfen';
import { navigate } from '@/routes/router';

/**
 * 「局面を読み取る」. 판이 찍힌 그림을 올려 국면을 가져오는 화면이다(journal §129).
 *
 * 세 걸음이다. 그림을 올리고, 읽어 낸 판을 사람이 확인해 고치고, 手番을 고른다. 확인 걸음이
 * 이 기능의 하나뿐인 검증자다(journal §129). 셋이 끝나면 국면이 검토의 주소가 되어
 * (`routeExplore` 의 `s`) 이 화면을 떠난다. 남는 것이 주소 한 줄이라 새로고침에도 링크
 * 공유에도 판이 살아 있다.
 *
 * 手番은 사진이 말해 주지 않아 사람에게 묻는다. 「先手か」가 아니라 「あなたの手番か」인 것은,
 * 아래쪽을 先手로 두는 정규화를 서버가 이미 했기 때문이다(`internal/boardread`).
 *
 * 읽는 걸음에만 로그인이 필요하다. 고치는 것과 분석하는 것은 룰 계산과 엔진 슬롯이라 익명에게
 * 열려 있다.
 */
export function PositionScreen({ me }: { me: MeResponse }) {
  // 로그인하지 않은 것을 오류로 다루지 않는다. 메뉴에는 이 줄이 로그인한 사람에게만 보이지만
  // 주소를 직접 열면 익명으로 들어오고, 그때 상자를 그려 주면 그림을 고르고 누른 뒤에야
  // 로그인이 필요하다는 것을 알게 된다(ImportScreen 과 같은 자리).
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
  /** 올린 그림. data URL 그대로 갖고 있다 — `<img src>` 와 요청 본문이 같은 값이다. */
  const [image, setImage] = useState<string | null>(null);
  const [reading, setReading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  /** 읽어 낸 판. 사람이 고치는 정본이 여기 하나다. */
  const [board, setBoard] = useState<BoardModel | null>(null);
  /**
   * 남겨 둔 그림의 이름. 판독을 재는 그림을 모을 때만 온다(`PositionResponse.imageId`).
   *
   * 있으면 「解析する」가 라벨도 같이 남겨, 그림과 정답의 짝이 하나 쌓인다.
   */
  const [imageId, setImageId] = useState<string | null>(null);
  const [faults, setFaults] = useState<PositionFault[]>([]);
  const [warnings, setWarnings] = useState<string[]>([]);
  /**
   * `faults` 가 어느 판의 것인가.
   *
   * 검사는 왕복이라 한 걸음 늦는다. 그 사이에 칸을 고치고 누르면 직전 판의 판정으로 버튼이
   * 열려 있어서, 二歩인 판이 검토로 넘어가 서버에 거절당한다. 뒤로 가면 화면이 다시 뜨면서
   * 올린 그림과 고친 것이 사라진다.
   */
  const [checkedSfen, setCheckedSfen] = useState('');

  const sfen = useMemo(() => (board ? toSfen(board) : ''), [board]);

  /**
   * 판이 바뀌면 성립하는지를 다시 묻는다. 엔진도 로그인도 쓰지 않는 자리라 한 칸을 고칠
   * 때마다 물어도 된다.
   *
   * 늦게 온 답은 쓰지 않는다. 두 칸을 이어서 고치면 요청이 겹치고, 먼저 보낸 것이 나중에
   * 오면 화면이 한 걸음 전의 판정을 그린다.
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
        setCheckedSfen(sfen);
      })
      .catch(() => {
        // 검사가 돌지 못해도 판은 그린다. 여기서 사유를 비우면 「문제가 없다」로 읽혀
        // 성립하지 않는 판이 분석으로 넘어가므로, 직전 판정을 그대로 둔다.
      });
    return () => controller.abort();
  }, [sfen]);

  const read = useCallback(async (dataURL: string) => {
    setImage(dataURL);
    setBoard(null);
    setFaults([]);
    setWarnings([]);
    setCheckedSfen('');
    setImageId(null);
    setError(null);
    setReading(true);
    try {
      const res = await readPosition(dataURL);
      setBoard(parseSfen(res.sfen));
      setFaults(res.faults);
      setWarnings(res.warnings);
      setCheckedSfen(res.sfen);
      setImageId(res.imageId ?? null);
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
   * 붙여 넣기로도 받는다. `navigator.clipboard.read()` 를 쓰지 않는 이유는 journal §129.
   *
   * 창 전체에서 듣는다. 이 화면에 글자를 넣는 자리가 없어 남의 붙여넣기를 가로챌 일이 없고,
   * 사람이 어디를 눌러 두었는지를 신경 쓰지 않아도 된다.
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
   * `dragover` 에서 기본 동작을 막지 않으면 브라우저가 그 그림을 탭에서 열어 버리고,
   * 사람이 만든 판이 그 자리에서 사라진다.
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

  /**
   * 사유가 하나라도 있으면 분석으로 넘어가지 않는다. 서버도 같은 문으로 거절한다.
   *
   * 판정이 지금 판의 것일 때만 연다(`checkedSfen`). 고친 직후에는 아직 직전 판의 결과다.
   */
  const analyzable = board !== null && faults.length === 0 && checkedSfen === sfen;

  const analyze = (): void => {
    if (!analyzable) return;
    // 라벨을 기다리지 않는다. 사람이 누른 일은 분석으로 넘어가는 것이고, 라벨은 곁다리다
    // (`saveLabel` 은 실패를 삼킨다).
    if (imageId !== null) void saveLabel(imageId, sfen);
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
        {/* 판이 화면의 절반인 방송 캡처가 크게 틀렸다(journal §129). 자르면 나아지는지는
            아직 재지 않았고, 사람에게 시키는 값이 작아서 먼저 권한다. */}
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
            {/* 올린 그림을 판 옆에 그대로 둔다. 대조할 원본이 화면에 없으면 확인이라는
                걸음 자체가 성립하지 않는다(journal §129). */}
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
            {!analyzable && (
              <span className="import__filename">
                {checkedSfen === sfen ? '局面を直すと解析できます。' : '局面を確かめています…'}
              </span>
            )}
          </div>
        </>
      )}
    </section>
  );
}
