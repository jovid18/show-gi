import { Fragment } from 'react';

import { useT } from '@/i18n';
import { GUIDE_CATEGORIES } from '@/libs/game/categories';
import { hrefOf, navigate } from '@/routes/router';

/**
 * 처음 온 사람에게 이 앱이 무엇을 하는지를 한 화면으로 말한다.
 *
 * 대국 화면이 스스로 설명하지 못하는 것이 셋이다. 개입은 일어나야 보이고(그때는 이미 판이
 * 되물러져 있다), 버튼의 예산은 눌러 봐야 알고, 카테고리 열한 개는 한 판에 두세 개밖에 나오지
 * 않는다.
 *
 * 화면 캡처를 쓰지 않는다. 아래의 눈금·버튼·불꽃은 대국 화면과 같은 CSS 라 판이 바뀌면 여기도
 * 같이 바뀐다. 캡처는 그 자리에서 낡는다.
 *
 * 숫자(3·5·6·10)가 여기 적혀 있다. 서버가 보내는 예산은 「지금 몇 번 남았나」이고 이 화면은
 * 대국 밖이라 받을 판이 없다. 주인은 `internal/game/session.go` 의 상수들이다.
 */
export function GuideScreen() {
  const t = useT();
  const g = t.guide;
  return (
    <article className="guide">
      <header className="guide__hero">
        {/* 로고는 `public/` 에 있다(brand/icons.sh). 여기만 큰 것을 쓰므로 192를 부른다.
            헤더의 96과 따로 두면 둘 다 자기 크기에 맞는 파일을 받는다. */}
        <img className="guide__logo" src="/icon-192.png" alt="" width={96} height={96} decoding="async" />
        <div>
          <h1 className="guide__title">{g.title}</h1>
          <p className="guide__lead">
            <Rich text={g.lead} />
          </p>
        </div>
      </header>

      <nav className="guide__toc" aria-label={g.tocLabel}>
        {SECTIONS.map((id) => (
          <a key={id} className="guide__toc-link" href={`#${id}`}>
            {g.toc[id]}
          </a>
        ))}
      </nav>

      <section className="guide__section" id="start" aria-labelledby="start-h">
        <h2 className="guide__head" id="start-h">
          {g.start.head}
        </h2>
        <ol className="guide__steps">
          <li className="guide__step">
            <h3 className="guide__step-head">{g.start.setupHead}</h3>
            <p className="guide__text">
              <Rich text={g.start.setupText} />
            </p>
          </li>
          <li className="guide__step">
            <h3 className="guide__step-head">{g.start.playHead}</h3>
            <p className="guide__text">
              <Rich text={g.start.playText} />
            </p>
          </li>
          <li className="guide__step">
            <h3 className="guide__step-head">{g.start.signInHead}</h3>
            <p className="guide__text">
              <Rich text={g.start.signInText} />
            </p>
          </li>
        </ol>
      </section>

      <section className="guide__section" id="intervene" aria-labelledby="intervene-h">
        <h2 className="guide__head" id="intervene-h">
          {g.intervene.head}
        </h2>
        <p className="guide__text">
          <Rich text={g.intervene.text} />
        </p>

        {/* 세 걸음을 가로로 늘어놓는다. 판을 그리면 「그때 판이 이렇게 생겼다」로 읽힌다. */}
        <ol className="guide__flow">
          {g.intervene.flow.map((text, i) => (
            <li key={i} className="guide__flow-step">
              <span className="guide__flow-num" aria-hidden="true">
                {i + 1}
              </span>
              <span className="guide__flow-text">{text}</span>
            </li>
          ))}
        </ol>

        <h3 className="guide__sub">{g.intervene.cardHead}</h3>
        <dl className="guide__defs">
          <div>
            <dt>{g.intervene.moveTerm}</dt>
            <dd>{g.intervene.moveDef}</dd>
          </div>
          <div>
            <dt>{g.intervene.whyTerm}</dt>
            <dd>{g.intervene.whyDef}</dd>
          </div>
          <div>
            <dt>{g.intervene.replyTerm}</dt>
            <dd>{g.intervene.replyDef}</dd>
          </div>
          <div>
            <dt>{g.intervene.tryTerm}</dt>
            <dd>
              <Rich text={g.intervene.tryDef} />
            </dd>
          </div>
        </dl>

        {/* 원칙 하나를 눈에 띄게 내건다. 이걸 모르면 「왜 정답을 안 알려주지」가 고장으로
            읽힌다(docs/01-core.md §1). */}
        <p className="guide__note">
          <Rich text={g.intervene.note} />
        </p>
      </section>

      <section className="guide__section" id="kinds" aria-labelledby="kinds-h">
        <h2 className="guide__head" id="kinds-h">
          {g.kinds.head}
        </h2>
        <p className="guide__text">
          <Rich text={g.kinds.text(GUIDE_CATEGORIES.length)} />
        </p>
        <ul className="guide__cats">
          {GUIDE_CATEGORIES.map((c) => (
            <li key={c.code} className="guide__cat">
              <span className="guide__cat-name">{c.nameJa}</span>
              <span className="guide__cat-note">{(g.kinds.notes as Record<string, string | undefined>)[c.code]}</span>
            </li>
          ))}
        </ul>
      </section>

      <section className="guide__section" id="ask" aria-labelledby="ask-h">
        <h2 className="guide__head" id="ask-h">
          {g.ask.head}
        </h2>
        <p className="guide__text">
          <Rich text={g.ask.text} />
        </p>

        {/* 대국 화면과 같은 클래스다. 見本이 진짜 버튼이라 모양이 어긋날 수가 없다. 안내
            화면에서 눌러도 갈 곳이 없어 `disabled` 로 둔다. */}
        <div className="guide__buttons">
          <div className="guide__button-row">
            <button type="button" className="btn" disabled>
              {g.ask.hint}
              <span className="play-actions__left" aria-hidden="true">
                {g.ask.left(6)}
              </span>
            </button>
            <p className="guide__text">
              <Rich text={g.ask.hintText} />
              <br />
              <span className="guide__muted">{g.ask.hintMuted}</span>
            </p>
          </div>

          <div className="guide__button-row">
            <button type="button" className="btn" disabled>
              {g.ask.undo}
              <span className="play-actions__left" aria-hidden="true">
                {g.ask.left(3)}
              </span>
            </button>
            <p className="guide__text">
              <Rich text={g.ask.undoText} />
              <br />
              <span className="guide__muted">{g.ask.undoMuted}</span>
            </p>
          </div>

          <div className="guide__button-row">
            <button type="button" className="btn btn--danger" disabled>
              {g.ask.resign}
            </button>
            <p className="guide__text">{g.ask.resignText}</p>
          </div>
        </div>
      </section>

      <section className="guide__section" id="board" aria-labelledby="board-h">
        <h2 className="guide__head" id="board-h">
          {g.board.head}
        </h2>
        <dl className="guide__defs">
          <div>
            <dt>{g.board.dotsTerm}</dt>
            <dd>{g.board.dotsDef}</dd>
          </div>
          <div>
            <dt>{g.board.flameTerm}</dt>
            <dd>
              <span className="guide__flame" aria-hidden="true" />
              {g.board.flameDef}
            </dd>
          </div>
          <div>
            <dt>{g.board.redTerm}</dt>
            <dd>{g.board.redDef}</dd>
          </div>
          <div>
            <dt>{g.board.blueTerm}</dt>
            <dd>
              <Rich text={g.board.blueDef} />
            </dd>
          </div>
          <div>
            <dt>{g.board.strengthTerm}</dt>
            <dd>
              {/* 실제 눈금과 같은 마크업. 3(ふつう)이 초기값이다. */}
              <span className="strength__pips guide__pips" aria-hidden="true">
                {[1, 2, 3, 4, 5].map((n) => (
                  <i key={n} data-on={n <= 3 || undefined} />
                ))}
              </span>
              {g.board.strengthDef}
            </dd>
          </div>
          <div>
            <dt>{g.board.shapeTerm}</dt>
            <dd>
              <Rich text={g.board.shapeDef} />
            </dd>
          </div>
        </dl>
      </section>

      <section className="guide__section" id="after" aria-labelledby="after-h">
        <h2 className="guide__head" id="after-h">
          {g.after.head}
        </h2>
        <dl className="guide__defs">
          <div>
            <dt>{g.after.summaryTerm}</dt>
            <dd>{g.after.summaryDef}</dd>
          </div>
          <div>
            <dt>{g.after.reviewTerm}</dt>
            <dd>
              <Rich text={g.after.reviewDef} />
            </dd>
          </div>
          <div>
            <dt>{g.after.quizTerm}</dt>
            <dd>{g.after.quizDef}</dd>
          </div>
          <div>
            <dt>{g.after.meTerm}</dt>
            <dd>{g.after.meDef}</dd>
          </div>
        </dl>
      </section>

      <section className="guide__section" id="notes" aria-labelledby="notes-h">
        <h2 className="guide__head" id="notes-h">
          {g.notes.head}
        </h2>
        <ul className="guide__notes">
          {g.notes.items.map((text, i) => (
            <li key={i}>
              <Rich text={text} />
            </li>
          ))}
        </ul>
      </section>

      <footer className="guide__cta">
        {/* `navigate` 를 탄다. 이 화면이 새 탭이 아니게 되면서(journal §86) 앱 안의 이동이
            됐고, 그냥 링크로 두면 브라우저가 문서를 새로 받아 상시 마운트된 대국 화면이 갖고
            있던 총평이 사라진다. */}
        <a
          className="btn btn--primary"
          href={hrefOf({ name: 'game' })}
          onClick={(e) => {
            if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
            e.preventDefault();
            navigate({ name: 'game' });
          }}
        >
          {g.cta}
        </a>
      </footer>
    </article>
  );
}

/** 목차. 절과 같은 순서여야 해서 한 자리에 둔다. 제목은 `guide.toc` 에 있다. */
const SECTIONS = ['start', 'intervene', 'kinds', 'ask', 'board', 'after', 'notes'] as const;

/** `**굵게**` 와 줄바꿈(`\n`)만 아는 작은 렌더러. 문구를 앞뒤 조각으로 쪼개지 않으려고 둔다. */
function Rich({ text }: { text: string }) {
  return text.split('\n').map((line, i) => (
    <Fragment key={i}>
      {i > 0 && <br />}
      {line.split('**').map((part, j) => (j % 2 === 1 ? <strong key={j}>{part}</strong> : part))}
    </Fragment>
  ));
}
