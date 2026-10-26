import { useSyncExternalStore } from 'react';

import { getLocale, subscribeLocale, type Locale } from './locale';
import * as common from './ns/common';
import * as game from './ns/game';
import * as board from './ns/board';
import * as review from './ns/review';
import * as quiz from './ns/quiz';
import * as guide from './ns/guide';
import * as profile from './ns/profile';
import * as explore from './ns/explore';
import * as importer from './ns/importer';
import * as position from './ns/position';
import * as match from './ns/match';

/**
 * 화면 문자열 전부. 이름공간 하나가 `ns/` 의 파일 하나다.
 *
 * 일본어가 원본이다. 각 파일의 `ko` 는 `typeof ja` 로 선언하므로 키가 빠지거나 인자 타입이
 * 다르면 typecheck 가 막는다. 인자를 덜 받는 함수는 타입으로 막히지 않아 `messages.test.ts` 가 본다.
 */
const ja = {
  common: common.ja,
  game: game.ja,
  board: board.ja,
  review: review.ja,
  quiz: quiz.ja,
  guide: guide.ja,
  profile: profile.ja,
  explore: explore.ja,
  importer: importer.ja,
  position: position.ja,
  match: match.ja,
};

export type Messages = typeof ja;

const ko: Messages = {
  common: common.ko,
  game: game.ko,
  board: board.ko,
  review: review.ko,
  quiz: quiz.ko,
  guide: guide.ko,
  profile: profile.ko,
  explore: explore.ko,
  importer: importer.ko,
  position: position.ko,
  match: match.ko,
};

const MESSAGES: Record<Locale, Messages> = { ja, ko };

export function messagesOf(locale: Locale): Messages {
  return MESSAGES[locale];
}

/** 지금 언어의 문자열. 언어가 바뀌면 이 훅을 부른 컴포넌트가 다시 그려진다. */
export function useT(): Messages {
  return MESSAGES[useSyncExternalStore(subscribeLocale, getLocale)];
}

export function useLocale(): Locale {
  return useSyncExternalStore(subscribeLocale, getLocale);
}
