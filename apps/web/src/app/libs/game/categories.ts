/**
 * 안내 화면이 늘어놓는 **口出し의 종류**.
 *
 * **이름의 주인은 서버다**(`internal/explain/label.go`). 대국 카드·되짚기·총평·마이페이지는
 * 전부 서버가 만든 `categoryJa` 를 그대로 받아 쓰고, 화면이 코드(`hangs_piece`)를 일본어로
 * 바꾸는 자리는 한 곳도 없다 — 어휘가 두 벌이 되면 어긋났을 때 어느 쪽이 맞는지 알 수 없다.
 *
 * **여기만 예외이고, 그래서 잠가 뒀다.** 안내 화면은 「무엇이 있나」를 대국 없이 미리 보여
 * 주는 자리라 실제로 걸린 개입이 없고, 받을 데가 없어서 적는 수밖에 없다. 대신
 * `label_guide_test.go` 가 이 파일을 읽어 **열 개가 이름까지 서버와 같은지** 본다 —
 * 카테고리를 늘리거나 이름을 고치면 Go 테스트가 여기서 깨진다.
 *
 * `note` 는 서버 문구가 아니다. 저쪽은 **그 판의 그 수**에 하는 말이고(`baseMessages`),
 * 이쪽은 두기 전에 읽는 설명이라 시제도 주어도 다르다.
 */
export interface GuideCategory {
  code: string;
  nameJa: string;
  note: string;
}

export const GUIDE_CATEGORIES: readonly GuideCategory[] = [
  { code: 'missed_mate', nameJa: '詰み逃し', note: '詰ませられたのに、別の手を選んだとき。' },
  { code: 'slower_mate', nameJa: '詰みの遠回り', note: '詰みは残っているけれど、手数が伸びる手を選んだとき。' },
  { code: 'lets_mate', nameJa: '詰まされる', note: 'その手を指すと、逆に自分の玉が詰まされるとき。' },
  { code: 'hangs_piece', nameJa: 'タダ捨て', note: '取り返せない場所に駒を置いてしまったとき。' },
  { code: 'shallow_trap', nameJa: '浅い得', note: '一手先までは得なのに、その先で形勢が入れ替わるとき。' },
  { code: 'unpromoted', nameJa: '不成', note: '動かす駒も行き先も合っているのに、成っていないとき。' },
  { code: 'greedy_capture', nameJa: '割に合わない取り', note: '駒は取れるけれど、払う代償のほうが大きいとき。' },
  { code: 'idle_check', nameJa: '追う手', note: '王手はかかるものの続きがなく、手番を渡すだけのとき。' },
  { code: 'king_exposed', nameJa: '玉が薄い', note: '自玉のまわりが手薄になり、相手の攻めが届くとき。' },
  { code: 'other', nameJa: '大きな形勢損', note: '上のどれにも当てはまらないけれど、形勢を大きく損ねるとき。' },
];
