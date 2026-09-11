import { lazy, Suspense, useEffect, useSyncExternalStore } from 'react';

import { useViewer } from '@/hooks/useViewer';
import { getPlaying, subscribePlaying } from '@/libs/game/playing';
import { GameScreen } from '@/screens/game/GameScreen';
import { HomeScreen } from '@/screens/home/HomeScreen';
import { hrefOf, navigate, useRoute, type Route } from '@/routes/router';

/**
 * 되짚기는 나중에 받는다.
 *
 * 이 화면이 평가치 궤적 때문에 차트 라이브러리를 끌고 온다(recharts). 첫 화면에 같이 실리면
 * 대국을 열려고 온 사람이 되짚기용 코드를 먼저 내려받는다.
 */
const ReviewScreen = lazy(async () => ({ default: (await import('@/screens/review/ReviewScreen')).ReviewScreen }));

/** 퀴즈도 나중에 받는다. 판 하나를 보러 온 사람이 퀴즈 화면까지 내려받을 이유가 없다. */
const QuizScreen = lazy(async () => ({ default: (await import('@/screens/quiz/QuizScreen')).QuizScreen }));

/** 마이페이지도 나중에 받는다. 첫 화면은 메뉴이고 여기는 판 하나도 그리지 않는다. */
const ProfileScreen = lazy(async () => ({
  default: (await import('@/screens/me/ProfileScreen')).ProfileScreen,
}));

/** 안내도 나중에 받는다. 글이 전부라 첫 화면에 실릴 이유가 가장 적다. */
const GuideScreen = lazy(async () => ({
  default: (await import('@/screens/guide/GuideScreen')).GuideScreen,
}));

/** 검토도 나중에 받는다. 판·駒台가 첫 화면에 이미 있어 이 조각은 화면 자신뿐이다. */
const ExploreScreen = lazy(async () => ({
  default: (await import('@/screens/explore/ExploreScreen')).ExploreScreen,
}));

/** 가져오기도 나중에 받는다. 화면에 상자 하나와 미리보기만 있다. */
const ImportScreen = lazy(async () => ({
  default: (await import('@/screens/import/ImportScreen')).ImportScreen,
}));

const PositionScreen = lazy(async () => ({
  default: (await import('@/screens/position/PositionScreen')).PositionScreen,
}));

/** 대인전도 나중에 받는다. 개입도 힌트도 없어 코드를 나눠 쓰지 않는다(journal §83). */
const MatchScreen = lazy(async () => ({
  default: (await import('@/screens/match/MatchScreen')).MatchScreen,
}));

/**
 * 화면마다 다른 제목. 검색 결과에 나가고, 탭을 여러 개 열어 둔 사람이 구분하는 줄이다.
 *
 * `index.html` 의 것과 어휘를 맞춘다. 저쪽이 첫 로드와 크롤러가 보는 값이라, 같은 화면이
 * 두 이름을 가지면 안 된다.
 */
const TITLE_JA: Record<Route['name'], string> = {
  // 홈만 「화면 이름 | show-gi」 꼴에서 빠진다. `index.html` 의 `<title>` 이 가리키는 바로
  // 그 자리다(canonical · OG · sitemap 도 같다).
  home: 'show-gi — 口を出すときを自分で決める将棋の相手',
  game: '対局 | show-gi',
  reviews: '振り返り | show-gi',
  review: '振り返り | show-gi',
  quiz: 'クイズ | show-gi',
  me: 'マイページ | show-gi',
  guide: 'あそびかた | show-gi',
  room: '対人戦 | show-gi',
  explore: '検討 | show-gi',
  import: '棋譜の取り込み | show-gi',
  position: '局面の読み取り | show-gi',
};

export function App() {
  const route = useRoute();
  const onHome = route.name === 'home';
  const onGame = route.name === 'game';
  const onMe = route.name === 'me';
  const onGuide = route.name === 'guide';
  const { me, signOut } = useViewer();

  /** 두는 중인가. 아래 되돌리기가 이 값 하나에 걸린다. 소유권은 `libs/game/playing.ts`. */
  const playing = useSyncExternalStore(subscribePlaying, getPlaying);

  /**
   * 두는 중에는 판만 있다(journal §86). 다른 주소로 들어오면 대국으로 되돌린다.
   *
   * `replace` 다. 이력에 쌓으면 판을 벗어날 수 없다는 사실이 「뒤로 가기가 고장 났다」로
   * 읽힌다. 브라우저를 아예 떠나는 길은 `useUnloadGuard` 가 묻는다.
   */
  useEffect(() => {
    if (playing && !onGame) navigate({ name: 'game' }, { replace: true });
  }, [playing, onGame]);

  useEffect(() => {
    document.title = TITLE_JA[route.name];
  }, [route.name]);

  return (
    <>
      {/* 겹이 장막(40)보다 낮다. 개입이 걸리면 이 줄도 함께 어두워져야 한다. */}
      <header className="app-head">
        <div className="app-head__inner">
          {/* 홈이 아닌 화면에만, 두는 중이 아닐 때만 뜬다(journal §86). 로고와 같은 곳으로
              가는데 합치지 않는 것은, 화살표가 「뒤로」를 글자 없이 말하는 표식이어서다. */}
          {!onHome && !playing && (
            <a
              className="app-back"
              href={hrefOf({ name: 'home' })}
              aria-label="メニューにもどる"
              onClick={(e) => {
                if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
                e.preventDefault();
                navigate({ name: 'home' });
              }}
            >
              <span aria-hidden="true">←</span>
            </a>
          )}

          <BrandMark locked={playing} />
        </div>
      </header>

      <div className="app">
        {/*
          감추기만 한다. 내리지 않는다. 대국은 WebSocket 연결 하나에 매여 있어서, 여기서
          컴포넌트를 내리면 연결이 끊기고 그 판이 `abandoned` 로 닫힌다.

          되돌리는 것은(위 `useEffect`) 그리고 난 뒤라 한 틱 동안 감춰진다. 조건을
          `{onGame && …}` 로 바꾸면 그 한 틱에 판이 닫힌다.
        */}
        <div hidden={!onGame}>
          <GameScreen />
        </div>

        {/* 홈은 나중에 받지 않는다. 여기에 `Suspense` 를 씌우면 첫 방문자가 메뉴를 보기까지
            조각 하나를 더 기다린다. */}
        {onHome && <HomeScreen me={me} playing={playing} />}

        {/* 리뷰는 열 때마다 새로 부른다. 방금 끝난 판이 목록 맨 위에 있어야 한다. */}
        {!onGame && !onHome && (
          <Suspense fallback={<p className="review-status">読み込み中…</p>}>
            {onGuide ? (
              <GuideScreen />
            ) : onMe ? (
              /* 로그아웃이 이 화면 안에 있다(journal §86). 이미 물어 둔 `signOut` 을 내려
                 준다. 화면에서 `useViewer` 를 또 부르면 `/api/me` 가 하나 더 나간다. */
              <ProfileScreen onSignOut={signOut} />
            ) : route.name === 'room' ? (
              // 방마다 새로 만든다. `roomId` 만 갈아 끼우면 이 컴포넌트가 살아남아
              // 앞 방의 스냅샷과 시계를 한 틱 동안 그린다(퀴즈 화면과 같은 자리).
              <MatchScreen key={route.id} roomId={route.id} />
            ) : route.name === 'import' ? (
              /* 로그인 여부는 App 이 이미 갖고 있다(위 ProfileScreen 과 같은 규약). */
              <ImportScreen me={me} />
            ) : route.name === 'position' ? (
              // 사진에서 국면을 가져오는 화면. 로그인 검사가 있고, 끝나면 다른 화면으로
              // 옮겨 간다(journal §129).
              <PositionScreen me={me} />
            ) : route.name === 'explore' ? (
              // 手合割마다 새로 만든다. 뿌리가 바뀌면 다른 판이라, 컴포넌트가 살아남으면
              // 한 틱 동안 앞 手合의 국면이 새 手合의 이름 아래에 뜬다(퀴즈와 같은 자리).
              <ExploreScreen
                key={route.sfen ?? route.handicap}
                handicap={route.handicap}
                moves={route.moves}
                sfen={route.sfen ?? ''}
              />
            ) : route.name === 'quiz' ? (
              // 판마다 새로 만든다. `id` 만 갈아 끼우면 이 컴포넌트가 그대로 살아서
              // 앞 판의 답과 기다린 횟수를 물려받고, 한 틱 동안 남의 문항을 그린다.
              <QuizScreen key={route.id} id={route.id} />
            ) : (
              <ReviewScreen route={route} />
            )}
          </Suspense>
        )}
      </div>
    </>
  );
}

/**
 * 로고와 제품 이름. 두는 중에는 링크를 끈다(journal §86). 눌러도 판으로 되돌아오므로
 * (App 의 `useEffect`) 링크로 두면 「눌렀는데 아무 일도 일어나지 않는다」가 된다.
 *
 * 링크일 때는 `navigate` 를 탄다. `<a href>` 로 두면 브라우저가 문서 전체를 새로 받아 상시
 * 마운트된 대국 화면이 갖고 있던 총평이 사라진다.
 *
 * 안쪽이 같고 겉이 갈리는 것뿐이라 한 자리에 둔다.
 */
function BrandMark({ locked }: { locked: boolean }) {
  const inner = (
    <>
      {/* 마크. 파일은 `public/` 에 있다(brand/icons.sh 가 로고 한 장에서 만든다). 번들에
          넣으면 파비콘·홈 화면 아이콘과 같은 그림이 두 벌로 나간다. 옆에 제품 이름이 글자로
          서 있으므로 `alt` 는 비운다. */}
      <img
        className="app-mark"
        src="/logo-96.png"
        alt=""
        width={30}
        height={30}
        // 헤더는 첫 화면에서 언제나 보이는 자리라 미루지 않는다.
        fetchPriority="high"
        decoding="async"
      />
      <span className="app-brand__text">
        <span className="app-title">show-gi</span>
        <span className="app-tagline">口を出すときを自分で決める将棋の相手</span>
      </span>
    </>
  );

  if (locked) return <div className="app-brand">{inner}</div>;

  return (
    <a
      className="app-brand"
      href={hrefOf({ name: 'home' })}
      onClick={(e) => {
        if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
        e.preventDefault();
        navigate({ name: 'home' });
      }}
    >
      {inner}
    </a>
  );
}
