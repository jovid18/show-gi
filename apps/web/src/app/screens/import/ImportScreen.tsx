import { useCallback, useRef, useState } from 'react';

import { useKifuImport } from '@/hooks/useKifuImport';
import type { ChosenResult } from '@/protocol/kifu';
import type { MyColor } from '@/protocol/review';
import { navigate } from '@/routes/router';

/**
 * 밖에서 둔 자기 기보를 취해 오는 화면(docs/journal §126).
 *
 * 두 단계로 나눠 놓았다. 먼저 읽어서 手数와 앞뒤 수를 보여 주고, 사람이 「이 판이 맞다」와
 * 「내가 어느 쪽이었나」를 확인한 뒤에야 취해 온다 — 잘못 읽은 기보 하나가 엔진 몇 분이라,
 * 확인이 없으면 그 시간이 통째로 버려진다.
 *
 * 붙여 넣기와 파일 선택이 같은 자리로 흘러간다. 파일은 글자를 읽어 같은 상자에 넣을 뿐이라
 * (`readAsText`) 그 뒤가 한 갈래다.
 */

/** 받는 파일. 어느 쪽이든 안이 글자라 브라우저가 읽어 상자에 붓는다. */
const ACCEPT = '.kif,.kifu,.ki2,.csa,.txt,text/plain';

export function ImportScreen() {
  const [text, setText] = useState('');
  const [color, setColor] = useState<MyColor | null>(null);
  const [chosen, setChosen] = useState<ChosenResult | null>(null);
  const { phase, preview, error, read, submit, reset } = useKifuImport();
  const fileRef = useRef<HTMLInputElement>(null);

  const busy = phase !== 'idle';

  const changeText = useCallback(
    (next: string) => {
      setText(next);
      // 원문이 바뀌면 앞의 미리보기는 다른 판의 것이다. 남겨 두면 사람이 남의 手数를
      // 보면서 취해 오기를 누른다.
      if (preview || error) reset();
    },
    [preview, error, reset],
  );

  const pickFile = useCallback(
    async (file: File | undefined) => {
      if (!file) return;
      changeText(await file.text());
    },
    [changeText],
  );

  /** 기보가 결과를 말하면 안 묻는다. 기록이 사람의 기억보다 맞다. */
  const asksResult = preview !== null && preview.result === undefined;
  const ready = preview !== null && color !== null && (!asksResult || chosen !== null);

  const onSubmit = useCallback(async () => {
    if (!ready || color === null) return;
    const id = await submit(text, color, asksResult ? (chosen ?? undefined) : undefined);
    if (id !== null) navigate({ name: 'review', id });
  }, [ready, color, text, chosen, asksResult, submit]);

  return (
    <section className="import">
      <h1 className="import__title">棋譜を取り込む</h1>
      <p className="import__lead">
        ほかで指した自分の対局を貼り付けると、この場で解析してクイズと棋力の目安を出します。 KIF・KI2・CSA・USI
        に対応しています。
      </p>

      <label className="import__label" htmlFor="import-text">
        棋譜
      </label>
      <textarea
        id="import-text"
        className="import__text"
        value={text}
        rows={12}
        spellCheck={false}
        placeholder={'1 ７六歩(77)\n2 ３四歩(33)\n…'}
        disabled={busy}
        onChange={(e) => changeText(e.target.value)}
      />

      <div className="import__row">
        <input
          ref={fileRef}
          type="file"
          accept={ACCEPT}
          className="import__file"
          disabled={busy}
          onChange={(e) => void pickFile(e.target.files?.[0])}
        />
        <button
          type="button"
          className="import__button"
          disabled={busy || text.trim() === ''}
          onClick={() => void read(text)}
        >
          {phase === 'reading' ? '読み取っています…' : '読み取る'}
        </button>
      </div>

      {error !== null && (
        <p className="import__error" role="alert">
          {error}
        </p>
      )}

      {preview !== null && (
        <div className="import__preview">
          <h2 className="import__subtitle">読み取った内容</h2>

          {/* 옮겨 적힌 판은 그렇다고 말한다. 그 수도 전부 룰 엔진을 지나 왔지만,
              사람이 자기 기보인지 눈으로 확인할 수 있게 하는 것이 지어내기에 대한
              두 번째 방어다. */}
          {preview.transcribed && <p className="import__note">AI が書式を読み取りました。下の手順をご確認ください。</p>}

          <dl className="import__facts">
            <div>
              <dt>手数</dt>
              <dd>{preview.plies}手</dd>
            </div>
            {preview.handicapJa !== undefined && (
              <div>
                <dt>手合割</dt>
                <dd>{preview.handicapJa}</dd>
              </div>
            )}
            {preview.sente !== undefined && preview.sente !== '' && (
              <div>
                <dt>先手</dt>
                <dd>{preview.sente}</dd>
              </div>
            )}
            {preview.gote !== undefined && preview.gote !== '' && (
              <div>
                <dt>後手</dt>
                <dd>{preview.gote}</dd>
              </div>
            )}
            {preview.result !== undefined && (
              <div>
                <dt>結果</dt>
                <dd>{RESULT_JA[preview.result]}</dd>
              </div>
            )}
          </dl>

          {/* 앞뒤를 같이 보여 준다. 앞만 보여 주면 「뒤가 잘렸는가」를 알 수 없고,
              그것이 취해 오기에서 가장 흔한 오류다. */}
          <p className="import__moves">
            {preview.head.join(' ')}
            {preview.tail !== undefined && preview.tail.length > 0 && ` … ${preview.tail.join(' ')}`}
          </p>

          <fieldset className="import__choice">
            <legend>あなたはどちらでしたか</legend>
            {(['b', 'w'] as const).map((c) => (
              <label key={c}>
                <input type="radio" name="color" checked={color === c} disabled={busy} onChange={() => setColor(c)} />
                {COLOR_JA[c]}
              </label>
            ))}
          </fieldset>

          {asksResult && (
            <fieldset className="import__choice">
              <legend>この対局の結果</legend>
              {(['win', 'loss', 'draw'] as const).map((r) => (
                <label key={r}>
                  <input
                    type="radio"
                    name="result"
                    checked={chosen === r}
                    disabled={busy}
                    onChange={() => setChosen(r)}
                  />
                  {CHOSEN_JA[r]}
                </label>
              ))}
            </fieldset>
          )}

          <button type="button" className="import__button" data-primary disabled={!ready || busy} onClick={onSubmit}>
            {phase === 'importing' ? '取り込んでいます…' : 'この内容で取り込む'}
          </button>

          {/* 몇 분 걸린다는 것을 미리 말한다. 안 말하면 되짚기 화면의 「解析しています」를
              고장으로 읽는다. */}
          <p className="import__note">解析には数分かかります。振り返りの画面で待てます。</p>
        </div>
      )}
    </section>
  );
}

const COLOR_JA: Record<MyColor, string> = { b: '先手（下手）', w: '後手（上手）' };

const RESULT_JA: Record<'sente' | 'gote' | 'draw', string> = {
  sente: '先手の勝ち',
  gote: '後手の勝ち',
  draw: '引き分け',
};

const CHOSEN_JA: Record<ChosenResult, string> = { win: '勝ち', loss: '負け', draw: '引き分け' };
