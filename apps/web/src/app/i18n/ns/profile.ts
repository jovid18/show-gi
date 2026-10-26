// 마이페이지 화면과, 대국 화면·마이페이지가 함께 쓰는 태그 축 이름.

import type { StyleTag } from '@/protocol/game';

export const ja = {
  anonymous: 'ログインすると、これまでの成績と棋力の目安が見られます。',
  anonymousNote: 'ログインしていない対局は記録が誰のものか分からないため、ここには出ません。',
  signIn: 'ログイン',
  loadFailed: '成績を読み込めませんでした。',
  rank: {
    head: '棋力の目安',
    note: '指し手の精度から算出した目安です。道場や他のアプリの段級とは異なります。',
    empty: 'まだ測っていません。何局か指すと出ます。',
  },
  record: {
    head: '成績',
    games: '対局',
    win: '勝ち',
    loss: '負け',
    draw: '引き分け',
    empty: 'まだ終わった対局がありません。',
    play: '対局する',
  },
  weak: {
    head: '崩れやすいところ',
    count: (n: number) => `${n}回`,
    noRepeat: '同じところで繰り返し戻したことは、まだありません。',
    none: 'まだ戻した手がありません。',
  },
  styles: {
    head: '組んだ形',
    games: (n: number) => `${n}局`,
    empty: 'まだ名前のついた形がありません。囲いや戦法が組み上がると、その場で盤に名前が出ます。',
  },
  signOut: 'ログアウト',
  /** 태그 축 이름. 한쪽에서 「戦法」, 다른 쪽에서 「戦型」이 되지 않게 한 자리에 둔다(libs/game/tags.ts). */
  tagKind: {
    castle: '囲い',
    formation: '戦法',
    opening: '戦型',
    tesuji: '手筋',
  } satisfies Record<StyleTag['kind'], string>,
};

export const ko: typeof ja = {
  anonymous: '로그인하면 지금까지의 성적과 기력 가늠을 볼 수 있습니다.',
  anonymousNote: '로그인하지 않은 대국은 기록이 누구의 것인지 알 수 없어서 여기에 나오지 않습니다.',
  signIn: '로그인',
  loadFailed: '성적을 불러오지 못했습니다.',
  rank: {
    head: '기력 가늠',
    note: '두는 수의 정확도로 산출한 가늠입니다. 도장이나 다른 앱의 단급과는 다릅니다.',
    empty: '아직 측정하지 않았습니다. 몇 판 두면 나옵니다.',
  },
  record: {
    head: '성적',
    games: '대국',
    win: '승',
    loss: '패',
    draw: '무승부',
    empty: '아직 끝난 대국이 없습니다.',
    play: '대국하기',
  },
  weak: {
    head: '무너지기 쉬운 곳',
    count: (n: number) => `${n}회`,
    noRepeat: '같은 곳에서 거듭 되돌린 적은 아직 없습니다.',
    none: '아직 되돌린 수가 없습니다.',
  },
  styles: {
    head: '짠 모양',
    games: (n: number) => `${n}판`,
    empty: '아직 이름 붙은 모양이 없습니다. 진형이나 전법이 완성되면 그 자리에서 판에 이름이 나옵니다.',
  },
  signOut: '로그아웃',
  tagKind: {
    castle: '진형',
    formation: '전법',
    opening: '전형',
    tesuji: '수법',
  },
};
