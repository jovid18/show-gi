import { useCallback, useEffect, useMemo, useRef, useState, type DragEvent } from 'react';

import { PositionEditor } from './PositionEditor';
import { TsumeTree } from './TsumeTree';
import { SIGN_IN_PATH, type MeResponse } from '@/protocol/auth';
import {
  IMAGE_ACCEPT,
  PositionError,
  checkPosition,
  readImageFile,
  readPosition,
  readPositionText,
  saveLabel,
  type PositionFault,
} from '@/protocol/position';
import { TsumeError, solveTsume, type TsumeResponse } from '@/protocol/tsume';
import { parseSfen, toSfen, type Board as BoardModel } from '@/models/sfen';
import { navigate } from '@/routes/router';

/**
 * 「局面を読み取る」. 판이 찍힌 그림을 올려 국면을 가져오는 화면이다(journal §129).
 *
 * 그림 업로드, 판독 결과 확인·수정, 手番 선택 순서로 진행한다.
 * 판독 오류는 사람이 확인해 고친다(journal §129). 완료하면 `routeExplore`의 `s`에 국면을 담아
 * 검토 화면으로 이동한다. 주소에 국면이 남으므로 새로고침과 링크 공유 후에도 복원할 수 있다.
 *
 * 手番은 사진이 말해 주지 않아 사람에게 묻는다. 「先手か」가 아니라 「あなたの手番か」인 것은,
 * 아래쪽을 先手로 두는 정규화를 서버가 이미 했기 때문이다(`internal/boardread`).
 *
 * 읽는 걸음에만 로그인이 필요하다. 고치는 것과 분석하는 것은 룰 계산과 엔진 슬롯이라 익명에게
 * 열려 있다.
 *
 * `tsume` 은 같은 화면을 詰め将棋에 쓴다(「詰将棋を解く」, journal §147). 手番을 묻지 않는다. 아래쪽
 * 사람이 언제나 다음에 두는 공격 쪽이다. 확인이 끝나면 화면을 떠나지 않고 그 자리에 수순 트리를
 * 그린다. 국면을 글자(SFEN·`position sfen … moves …`·KIF 의 局面図)로도 받는다. 글자는 手番을 담고 있어 그대로 쓴다.
 */
export function PositionScreen({ me, mode = 'explore' }: { me: MeResponse; mode?: Mode }) {
  // 로그인하지 않은 것을 오류로 다루지 않는다. 메뉴에는 이 줄이 로그인한 사람에게만 보이지만
  // 주소를 직접 열면 익명으로 들어오고, 그때 상자를 그려 주면 그림을 고르고 누른 뒤에야
  // 로그인이 필요하다는 것을 알게 된다(ImportScreen 과 같은 자리).
  if (me.user === null) return <SignInFirst enabled={me.enabled} mode={mode} />;
  return <PositionForm mode={mode} />;
}

type Mode = 'explore' | 'tsume';

const TITLE: Record<Mode, string> = { explore: '局面を読み取る', tsume: '詰将棋を解く' };

function SignInFirst({ enabled, mode }: { enabled: boolean; mode: Mode }) {
  return (
    <section className="import">
      <h1 className="import__title">{TITLE[mode]}</h1>
      <p className="import__lead">
        {mode === 'tsume'
          ? '詰将棋が写った画像を上げると、その局面を読み取って詰み手順を調べます。'
          : '将棋盤が写った画像を上げると、その局面を読み取って形勢と最善手を調べます。'}
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

function PositionForm({ mode }: { mode: Mode }) {
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
    void checkPosition(sfen, controller.signal, mode === 'tsume')
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
  }, [sfen, mode]);

  const read = useCallback(
    async (dataURL: string) => {
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
        const got = parseSfen(res.sfen);
        // 詰め将棋는 언제나 아래쪽(사람)이 둘 차례다. 판독이 정한 手番을 쓰지 않는다.
        setBoard(mode === 'tsume' ? { ...got, turn: 'black' } : got);
        setImageId(res.imageId ?? null);
        // 판독이 붙인 사유는 보통 국면의 규칙으로 잰 것이다. 詰め将棋는 위 effect 의 검사를 기다린다.
        if (mode !== 'tsume') {
          setFaults(res.faults);
          setWarnings(res.warnings);
          setCheckedSfen(res.sfen);
        }
      } catch (e) {
        setError(e instanceof PositionError ? e.message : '画像から局面を読み取れませんでした。');
      } finally {
        setReading(false);
      }
    },
    [mode],
  );

  /** 붙여 넣은 글자. 읽은 판은 그림에서 읽은 판과 같은 자리(`board`)로 간다. */
  const [text, setText] = useState('');
  const readText = async (): Promise<void> => {
    const t = text.trim();
    if (t === '') return;
    setImage(null);
    setBoard(null);
    setFaults([]);
    setWarnings([]);
    setCheckedSfen('');
    setImageId(null);
    setError(null);
    try {
      const res = await readPositionText(t, null, mode === 'tsume');
      // 사유는 위 effect 의 검사를 기다린다. 판을 그리는 것이 먼저다.
      setBoard(parseSfen(res.sfen));
    } catch (e) {
      setError(e instanceof PositionError ? e.message : '局面を読み取れませんでした。');
    }
  };

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
   * 창 전체에서 듣는다. 그림 파일이 든 붙여넣기만 가져가므로 글자 상자에 붙여 넣는 글자는
   * 가로채지 않고, 사람이 어디를 눌러 두었는지를 신경 쓰지 않아도 된다.
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

  /**
   * 詰め将棋의 답. 어느 판의 답인지를 같이 둔다(`sfen`). 칸을 고치면 그 답은 지금 판의 것이
   * 아니므로 그리지 않는다.
   */
  const [tsume, setTsume] = useState<{ sfen: string; res: TsumeResponse } | null>(null);
  const [solving, setSolving] = useState(false);
  /** 푸는 데 실패한 사유. 버튼 바로 아래에 둔다. 판독 오류 자리는 화면 맨 위라 누른 자리에서 보이지 않는다. */
  const [solveError, setSolveError] = useState<string | null>(null);
  const solve = async (): Promise<void> => {
    if (!analyzable || solving) return;
    if (imageId !== null) void saveLabel(imageId, sfen);
    setSolving(true);
    setSolveError(null);
    try {
      setTsume({ sfen, res: await solveTsume(sfen) });
    } catch (e) {
      setSolveError(e instanceof TsumeError ? e.message : '詰みを調べられませんでした。');
    } finally {
      setSolving(false);
    }
  };

  const analyze = (): void => {
    if (mode === 'tsume') {
      void solve();
      return;
    }
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
      <h1 className="import__title">{TITLE[mode]}</h1>
      <p className="import__lead">
        {mode === 'tsume'
          ? '詰将棋の画像を上げるか、局面を文字で入力してください。局面をあなたが確かめてから、王手の連続で詰む手順を調べます。'
          : '将棋盤が写った画像を上げてください。読み取った局面をあなたが確かめてから、形勢と最善手を調べます。'}
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

      {mode === 'tsume' && (
        <>
          <label className="import__label" htmlFor="position-text">
            文字で入力する（SFEN・USI・KIF）
          </label>
          <textarea
            id="position-text"
            className="import__text"
            value={text}
            rows={3}
            spellCheck={false}
            placeholder="position sfen lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1 moves 7g7f"
            onChange={(e) => setText(e.target.value)}
          />
          <div className="import__row">
            <button
              type="button"
              className="import__button"
              disabled={text.trim() === ''}
              onClick={() => void readText()}
            >
              この局面を使う
            </button>
          </div>
        </>
      )}

      {error !== null && <p className="position__error">{error}</p>}
      {reading && <p className="review-status">画像から局面を読み取っています…</p>}

      {board !== null && (
        <>
          <div className="position__compare">
            {/* 올린 그림을 판 옆에 그대로 둔다. 대조할 원본이 화면에 없으면 확인이라는
                걸음 자체가 성립하지 않는다(journal §129). */}
            {image !== null && (
              <figure className="position__shot">
                <img src={image} alt="上げた画像" />
                <figcaption>上げた画像</figcaption>
              </figure>
            )}
            <div className="position__read">
              <h2 className="position__subtitle">読み取った局面</h2>
              <p className="import__label">
                ちがうマスを押すと駒を直せます。{image !== null ? '下があなたの側です。' : '下が先手です。'}
              </p>
              <PositionEditor board={board} faults={faultSquares} onChange={setBoard} />
            </div>
          </div>

          {/* 手番. 사진이 말해 주지 않는 유일한 값이라 반드시 사람이 고른다. 詰め将棋는 묻지 않는다. */}
          {mode === 'tsume' ? (
            <p className="import__label">
              {image !== null
                ? '下のあなたが攻め方で、次に指します。持ち駒は駒台に写っているものだけを使います。'
                : `${board.turn === 'black' ? '下の先手' : '上の後手'}が攻め方で、次に指します。持ち駒は入力したものだけを使います。`}
            </p>
          ) : (
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
          )}

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
            <button type="button" className="btn btn--primary" disabled={!analyzable || solving} onClick={analyze}>
              {mode === 'tsume' ? '詰みを調べる' : 'この局面を解析する'}
            </button>
            {!analyzable && (
              <span className="import__filename">
                {checkedSfen === sfen ? '局面を直すと解析できます。' : '局面を確かめています…'}
              </span>
            )}
            {solving && (
              <span className="import__filename">詰みを調べています。長い詰みは数分かかることがあります…</span>
            )}
          </div>

          {solveError !== null && <p className="position__error">{solveError}</p>}
          {mode === 'tsume' && tsume !== null && tsume.sfen === sfen && <TsumeTree start={sfen} res={tsume.res} />}
        </>
      )}
    </section>
  );
}
