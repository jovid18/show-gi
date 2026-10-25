/**
 * 안내 화면이 늘어놓는 口出し의 종류.
 *
 * 이름의 주인은 서버다(`internal/explain/label.go`). 다른 화면은 전부 서버가 만든
 * `categoryJa` 를 그대로 받아 쓰고, 코드(`hangs_piece`)를 일본어로 바꾸는 자리가 없다.
 *
 * 여기만 예외다. 안내 화면은 대국 없이 「무엇이 있나」를 미리 보여 주는 자리라 실제로 걸린
 * 개입이 없고 받을 데가 없다. 대신 `label_guide_test.go` 가 이 파일을 읽어 열한 개가 이름까지
 * 서버와 같은지 본다.
 *
 * 풀이(note)는 화면이 직접 적고 `i18n/ns/guide.ts` 에 code 로 둔다. 서버 문구는 그 판의 그
 * 수에 하는 말이라(`baseMessages`) 시제도 주어도 다르다.
 */
export interface GuideCategory {
  code: string;
  nameJa: string;
}

export const GUIDE_CATEGORIES: readonly GuideCategory[] = [
  { code: 'missed_mate', nameJa: '詰み逃し' },
  { code: 'slower_mate', nameJa: '詰みの遠回り' },
  { code: 'lets_mate', nameJa: '詰まされる' },
  { code: 'hangs_piece', nameJa: 'タダ捨て' },
  { code: 'shallow_trap', nameJa: '浅い得' },
  { code: 'unpromoted', nameJa: '不成' },
  { code: 'greedy_capture', nameJa: '割に合わない取り' },
  { code: 'idle_check', nameJa: '追う手' },
  { code: 'king_exposed', nameJa: '玉が薄い' },
  { code: 'forced_loss', nameJa: '受けきれない損' },
  { code: 'other', nameJa: '大きな形勢損' },
];
