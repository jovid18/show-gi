import { GUIDE_CATEGORIES } from '@/libs/game/categories';
import { hrefOf, navigate } from '@/routes/router';

/**
 * 처음 온 사람에게 **이 앱이 무엇을 하는지**를 한 화면으로 말한다.
 *
 * 대국 화면이 스스로 설명하지 못하는 것이 셋이라 이 자리가 생겼다 — 개입은 **일어나야**
 * 보이고(그때는 이미 판이 되물러져 있다), 버튼의 예산은 눌러 봐야 알고, 카테고리 열 개는
 * 한 판에 두세 개밖에 안 나온다.
 *
 * **화면 캡처를 안 쓴다.** 그림 대신 실제 컴포넌트의 클래스를 그대로 얹는다 — 아래의 눈금·
 * 버튼·불꽃은 대국 화면과 **같은 CSS**라 판이 바뀌면 여기도 같이 바뀐다. 캡처는 그 자리에서
 * 낡고, 낡은 것을 아무도 안 잡는다(CLAUDE.md 「문서」).
 *
 * 숫자(3·5·6·10)를 여기 적는 것은 **사람이 보는 값이라서**다. 서버가 예산을 스냅샷으로
 * 보내지만 그건 「지금 몇 번 남았나」이고, 이 화면은 대국 밖이라 받을 판이 없다.
 * 주인은 `internal/game/session.go` 의 상수들이다.
 */
export function GuideScreen() {
  return (
    <article className="guide">
      <header className="guide__hero">
        {/* 로고는 `public/` 에 있다(brand/icons.sh). 여기만 큰 것을 쓰므로 192를 부른다 —
            헤더의 96과 갈라 두면 둘 다 자기 크기에 맞는 파일을 받는다. */}
        <img className="guide__logo" src="/icon-192.png" alt="" width={96} height={96} decoding="async" />
        <div>
          <h1 className="guide__title">はじめての方へ</h1>
          <p className="guide__lead">
            show-gi は、<strong>悪手を指した瞬間に盤を戻して理由を説明する</strong>将棋の相手です。
            <br />
            対局しながら「なぜその手が悪いのか」を覚えていくための道具なので、ふつうの対戦アプリとは少し違います。
          </p>
        </div>
      </header>

      <nav className="guide__toc" aria-label="目次">
        {SECTIONS.map((s) => (
          <a key={s.id} className="guide__toc-link" href={`#${s.id}`}>
            {s.title}
          </a>
        ))}
      </nav>

      <section className="guide__section" id="start" aria-labelledby="start-h">
        <h2 className="guide__head" id="start-h">
          はじめかた
        </h2>
        <ol className="guide__steps">
          <li className="guide__step">
            <h3 className="guide__step-head">手合割と手番、相手の戦型をえらぶ</h3>
            <p className="guide__text">
              まず<strong>手合割</strong>です。<strong>「平手」</strong>は駒を落とさない対局で、
              <strong>「二枚落ち」</strong>のように選ぶと相手（上手）の駒が落ちた状態からはじまり、
              駒を落とした上手から先に指します。平手ならつぎに先手か後手か、そして相手がどんな形に組むかを選びます。分からなければ
              <strong>「平手」</strong>と<strong>「おまかせ」</strong>のままで大丈夫です。
              <br />
              強さはここで選びません。指しているあいだに相手のほうが合わせてきます。
            </p>
          </li>
          <li className="guide__step">
            <h3 className="guide__step-head">ふつうに指す</h3>
            <p className="guide__text">
              駒をクリックすると動ける場所が光ります。持ち駒は盤の下の駒台から打てます。
              <br />
              悪手のときだけ相手が口を出すので、それ以外は最後までふつうの対局です。
            </p>
          </li>
          <li className="guide__step">
            <h3 className="guide__step-head">ログインは任意です</h3>
            <p className="guide__text">
              ログインしなくても対局できます。ログインすると対局が記録に残り、
              <strong>振り返り・マイページ・中断した対局のつづき</strong>が使えるようになります。
            </p>
          </li>
        </ol>
      </section>

      <section className="guide__section" id="intervene" aria-labelledby="intervene-h">
        <h2 className="guide__head" id="intervene-h">
          口出し — このアプリの中心
        </h2>
        <p className="guide__text">
          大きく形勢を損ねる手を指すと、その手は<strong>盤に残りません</strong>
          。指した瞬間に戻され、まわりが暗くなって説明のカードが出ます。
        </p>

        {/* 세 걸음을 가로로 세운다. 실제 화면을 흉내 내는 것이 아니라 **순서**를 말하는
            그림이라 판을 그리지 않는다 — 판을 그리면 「그때 판이 이렇게 생겼다」로 읽힌다. */}
        <ol className="guide__flow">
          <li className="guide__flow-step">
            <span className="guide__flow-num" aria-hidden="true">
              1
            </span>
            <span className="guide__flow-text">悪手を指す</span>
          </li>
          <li className="guide__flow-step">
            <span className="guide__flow-num" aria-hidden="true">
              2
            </span>
            <span className="guide__flow-text">その手が盤から戻る</span>
          </li>
          <li className="guide__flow-step">
            <span className="guide__flow-num" aria-hidden="true">
              3
            </span>
            <span className="guide__flow-text">理由のカードが出る</span>
          </li>
        </ol>

        <h3 className="guide__sub">カードに書いてあること</h3>
        <dl className="guide__defs">
          <div>
            <dt>指そうとした手</dt>
            <dd>戻された手そのものと、その手の評価。</dd>
          </div>
          <div>
            <dt>なぜ悪いのか</dt>
            <dd>相手の駒が何枚利いているか、何手で詰まされるかなど、その局面の事実で説明します。</dd>
          </div>
          <div>
            <dt>相手の反撃</dt>
            <dd>その手を指していたら相手が何をしてくるか。候補が並び、最善手からどれだけ損かも出ます。</dd>
          </div>
          <div>
            <dt>そのまま試せる</dt>
            <dd>
              カードが出ているあいだ、盤は<strong>「その手を指していたら」の世界</strong>
              になります。相手の手も自分で指して確かめられます。閉じると対局の盤に戻ります。
            </dd>
          </div>
        </dl>

        {/* **원칙 하나를 눈에 띄게 세운다.** 이걸 모르면 「왜 정답을 안 알려주지」가 고장으로
            읽힌다 — 실제로 그 자리에서 갈렸다(docs/01-core.md §1). */}
        <p className="guide__note">
          カードは<strong>「次にこう指せ」とは言いません。</strong>
          悪い理由と相手の反撃までを示して、指し直すのはあなたです。答えが欲しいときは、下の「ヒント」を自分で呼びます。
        </p>
      </section>

      <section className="guide__section" id="kinds" aria-labelledby="kinds-h">
        <h2 className="guide__head" id="kinds-h">
          口出しの種類
        </h2>
        <p className="guide__text">
          「なぜ悪いのか」は10種類に分けて記録されます。対局後のマイページでは、この名前ごとに
          <strong>あなたが崩れやすいところ</strong>が集計されます。
        </p>
        <ul className="guide__cats">
          {GUIDE_CATEGORIES.map((c) => (
            <li key={c.code} className="guide__cat">
              <span className="guide__cat-name">{c.nameJa}</span>
              <span className="guide__cat-note">{c.note}</span>
            </li>
          ))}
        </ul>
      </section>

      <section className="guide__section" id="ask" aria-labelledby="ask-h">
        <h2 className="guide__head" id="ask-h">
          自分から呼ぶもの
        </h2>
        <p className="guide__text">
          盤の右側に3つのボタンがあります。<strong>回数は対局ごと</strong>で、使い切ると押せなくなります。
        </p>

        {/* **대국 화면과 같은 클래스다.** 見本이 진짜 버튼이라 모양이 어긋날 수가 없다.
            `disabled` 로 두는 것이 요점 — 안내 화면에서 눌러도 갈 곳이 없다. */}
        <div className="guide__buttons">
          <div className="guide__button-row">
            <button type="button" className="btn" disabled>
              ヒント
              <span className="play-actions__left" aria-hidden="true">
                残り6回
              </span>
            </button>
            <p className="guide__text">
              いまの局面の最善手を教えます。<strong>同じ局面で2段階</strong>
              ——1回目は「どの駒か」まで、2回目は「どう動かすか」まで。3回目はありません。
              <br />
              1局に6回なので、答えまで見られるのは<strong>最大3手ぶん</strong>です。
              <br />
              <span className="guide__muted">答えを見た手は、棋力の計算から外れます。</span>
            </p>
          </div>

          <div className="guide__button-row">
            <button type="button" className="btn" disabled>
              待った
              <span className="play-actions__left" aria-hidden="true">
                残り3回
              </span>
            </button>
            <p className="guide__text">
              自分の指した手を1手戻します。口出しで戻されるのとは別で、こちらは<strong>あなたが決めて戻す</strong>
              ものです。
              <br />
              <span className="guide__muted">戻した手は記録に残り、振り返りに「待った」として出ます。</span>
            </p>
          </div>

          <div className="guide__button-row">
            <button type="button" className="btn btn--danger" disabled>
              投了
            </button>
            <p className="guide__text">その対局を負けとして終えます。押すともう一度確認します。</p>
          </div>
        </div>
      </section>

      <section className="guide__section" id="board" aria-labelledby="board-h">
        <h2 className="guide__head" id="board-h">
          盤と、そのまわりの見かた
        </h2>
        <dl className="guide__defs">
          <div>
            <dt>駒に打たれた点</dt>
            <dd>その駒がどの向きに動けるか。文字が読めなくても動きが分かるようにしてあります。</dd>
          </div>
          <div>
            <dt>盤のふちの炎</dt>
            <dd>
              <span className="guide__flame" aria-hidden="true" />
              詰みが近いほど強くなります。あなたが詰ませられる側のときだけ灯ります。
            </dd>
          </div>
          <div>
            <dt>赤い線</dt>
            <dd>王手をかけている筋です。緑の矢印（相手の次の手）とは色で分けています。</dd>
          </div>
          <div>
            <dt>青いふち</dt>
            <dd>
              行きづまったときのヒントです。<strong>同じ局面で3回戻されると「どの駒か」、5回で「どう動かすか」</strong>
              が自動で出ます。
            </dd>
          </div>
          <div>
            <dt>相手の強さ</dt>
            <dd>
              {/* 실제 눈금과 같은 마크업. 3(ふつう)이 초기값이다. */}
              <span className="strength__pips guide__pips" aria-hidden="true">
                {[1, 2, 3, 4, 5].map((n) => (
                  <i key={n} data-on={n <= 3 || undefined} />
                ))}
              </span>
              あなたの指し手の精度に合わせて、対局中に静かに動きます。自分では設定しません。
            </dd>
          </div>
          <div>
            <dt>形の名前</dt>
            <dd>
              美濃囲いのような形が完成すると、盤の上に名前が一瞬だけ出ます。
              <br />
              まだ組んでいない戦型が1手で組めるときは、右側に「名前のある手があります」と出ることがあります（1局に3回まで）。
            </dd>
          </div>
        </dl>
      </section>

      <section className="guide__section" id="after" aria-labelledby="after-h">
        <h2 className="guide__head" id="after-h">
          対局のあと
        </h2>
        <dl className="guide__defs">
          <div>
            <dt>総評</dt>
            <dd>
              対局が終わった場所に出ます。何で口出しが多かったか、棋力の目安がどう動いたか、どの局面を見直すとよいか。
            </dd>
          </div>
          <div>
            <dt>振り返り</dt>
            <dd>
              評価値のグラフをクリックしてその局面へ移動できます。
              <strong>最善手・実際に指した手・戻された手・自分で待ったした手</strong>
              が同じ並びに出て、そこから別の手も試せます。
            </dd>
          </div>
          <div>
            <dt>クイズ</dt>
            <dd>
              その対局から作られます。正解するまで答えは出ません（3回目に「どの駒を動かすか」だけ出ます）。作れる問題がない対局もあります。
            </dd>
          </div>
          <div>
            <dt>マイページ</dt>
            <dd>
              対局をまたいで見る唯一の画面です。棋力の目安・成績・崩れやすいところ・組んだ形。ログインした対局だけが数えられます。
            </dd>
          </div>
        </dl>
      </section>

      <section className="guide__section" id="notes" aria-labelledby="notes-h">
        <h2 className="guide__head" id="notes-h">
          おぼえておくこと
        </h2>
        <ul className="guide__notes">
          <li>
            段級は<strong>指し手の精度から出した目安</strong>です。道場や将棋ウォーズの段級とは異なります。
          </li>
          <li>ログインしていない対局は、誰のものか分からないため記録に残りません。</li>
          <li>相手が考えているあいだ、少し待つことがあります。同じ局面は2回目から速くなります。</li>
        </ul>
      </section>

      <footer className="guide__cta">
        {/* **`navigate` 를 탄다.** 이 화면이 새 탭이 아니게 되면서(journal §86) 앱 안의
            이동이 됐고, 그냥 링크로 두면 브라우저가 문서를 통째로 새로 받아 **상시
            마운트된 대국 화면이 들고 있던 총평이 사라진다**. */}
        <a
          className="btn btn--primary"
          href={hrefOf({ name: 'game' })}
          onClick={(e) => {
            if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
            e.preventDefault();
            navigate({ name: 'game' });
          }}
        >
          対局をはじめる
        </a>
      </footer>
    </article>
  );
}

/** 목차. 절과 **같은 순서·같은 제목**이어야 해서 한 자리에 둔다. */
const SECTIONS: { id: string; title: string }[] = [
  { id: 'start', title: 'はじめかた' },
  { id: 'intervene', title: '口出し' },
  { id: 'kinds', title: '口出しの種類' },
  { id: 'ask', title: '自分から呼ぶもの' },
  { id: 'board', title: '盤の見かた' },
  { id: 'after', title: '対局のあと' },
  { id: 'notes', title: 'おぼえておくこと' },
];
