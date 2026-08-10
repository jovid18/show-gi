-- kb_chunks 코퍼스 첫 적재 — 지금 실제로 뜨는 태그에만 항목을 붙인다.
--
-- **범위를 태그에 맞췄다.** 04-llm.md §4가 「囲い 10–14 + 전법 8–10 + 手筋 15–20 +
-- 블런더 카테고리 7 ≈ 50항목」이라고 적었는데, 手筋은 **감지하는 코드가 없다.**
-- 같은 문서가 「코퍼스를 먼저 만들어도 붙일 곳이 없다」고 적은 그 이유가 手筋에는
-- 아직 그대로 적용된다 — 항목을 넣어도 꺼내올 키가 없어서 검색에 걸리지 않는다.
-- 그래서 이번에는 `internal/tag` 가 내는 12개 + 블런더 카테고리 8개 = 20항목이다.
--
-- `source_url` 이 NOT NULL 인 것을 두 가지로 나눠 채웠다.
--
--   CC-BY-SA-4.0     囲い 항목. 배치 서술을 일본어 위키백과 본문에서 읽고 옮겼다.
--                    좌표의 출처가 실제로 그 문서라서 저작자 표시가 필요하다.
--   engine-derived   전법·블런더 카테고리 항목. 정의가 우리 코드 안에 있다 —
--                    전법은 飛의 筋 하나이고, 카테고리는 `internal/intervene` 의
--                    결정적 룰이다. 남의 글을 옮긴 것이 아니므로 그 파일을 가리킨다.
--
-- `verified_by` 는 전부 'engine' 이다. 어느 항목이든 그 주장이 코드로 확인되는 것만
-- 넣었다 — 좌표는 `internal/tag` 테스트가, 카테고리는 `internal/intervene` 이 판정한다.
-- 비어 있으면 검색에 걸려도 프롬프트에 안 붙는다(001_init.sql 의 부분 인덱스).
--
-- 본문은 **전부 일본어**다. 한국어를 넣으면 일본어 질의와 임베딩 유사도가 무너지고
-- 출력이 한국어로 샌다(CLAUDE.md 언어 규칙). `TestCorpusIsJapaneseOnly` 가 기계로 본다.

BEGIN;

-- 두 번 돌려도 같은 상태가 되게. 마이그레이션을 사람이 손으로 돌리므로(deploy/README.md §4)
-- 중간에 끊겨 다시 돌리는 일이 실제로 생긴다.
DELETE FROM kb_chunks WHERE tags && ARRAY[
    'kata_mino', 'hon_mino', 'taka_mino', 'ginkanmuri',
    'kin_yagura', 'gin_yagura', 'kata_yagura', 'fune',
    'naka_bisha', 'shiken_bisha', 'sanken_bisha', 'mukai_bisha',
    'missed_mate', 'hangs_piece', 'shallow_trap', 'unpromoted',
    'greedy_capture', 'idle_check', 'king_exposed', 'other'
];

-- ── 囲い ─────────────────────────────────────────────────────────────────────
INSERT INTO kb_chunks (title, body, tags, source_url, source_license, verified_by) VALUES

('片美濃囲い',
 '玉を2八、銀を3八、金を4八に置いた三枚の囲いです。振り飛車でもっとも早く組める形で、手数が少ないぶん急戦に間に合います。横からの攻めには強い一方、上部が薄いので端攻めや上からの押し潰しには弱いという性質があります。左金を5八に足すと本美濃囲いになります。',
 ARRAY['kata_mino', 'castle', 'mino'],
 'https://ja.wikipedia.org/wiki/美濃囲い', 'CC-BY-SA-4.0', 'engine'),

('本美濃囲い',
 '片美濃囲いに左金を5八へ足した四枚の囲いです。玉2八、銀3八、金4八、金5八の形で、振り飛車の基本となる囲いです。金が二枚横に並ぶため横からの攻めに非常に強く、寄せるのに手数がかかります。弱点は上部で、端の香や桂を絡めた攻めには注意が必要です。',
 ARRAY['hon_mino', 'castle', 'mino'],
 'https://ja.wikipedia.org/wiki/美濃囲い', 'CC-BY-SA-4.0', 'engine'),

('高美濃囲い',
 '本美濃囲いの左金を5八から4七へ進めた形です。玉2八、銀3八、金4八、金4七となり、上部が厚くなるので上からの攻めに強くなります。そのぶん5筋の横が薄くなるため、中央から攻められたときの備えが課題になります。さらに発展させると銀冠になります。',
 ARRAY['taka_mino', 'castle', 'mino'],
 'https://ja.wikipedia.org/wiki/美濃囲い', 'CC-BY-SA-4.0', 'engine'),

('銀冠',
 '銀を2七へ、右金を3八へ進めた形です。玉2八の上に銀がかぶさるため上部が非常に厚く、終盤で上から押し潰される展開に強い囲いです。美濃囲いから組み替えて到達することが多く、手数はかかりますが堅さは増します。銀が上がったぶん2八の玉の逃げ道が狭くなる点には注意します。',
 ARRAY['ginkanmuri', 'castle'],
 'https://ja.wikipedia.org/wiki/銀冠', 'CC-BY-SA-4.0', 'engine'),

('金矢倉',
 '玉を8八、左金を7八、右金を6七、左銀を7七に置いた矢倉囲いの基本形です。相居飛車の代表的な囲いで、金銀三枚が斜めに連結しているため上からの攻めに強い構えです。組むのに手数がかかるため、その間に急戦を仕掛けられないよう手順に気を配る必要があります。',
 ARRAY['kin_yagura', 'castle', 'yagura'],
 'https://ja.wikipedia.org/wiki/矢倉囲い', 'CC-BY-SA-4.0', 'engine'),

('銀矢倉',
 '金矢倉の右金を銀に置き換えた形で、玉8八、金7八、銀7七、銀6七となります。銀が二枚並ぶため上部の耐久力が高く、押し潰されにくい構えです。一方で金が一枚少ないぶん横からの攻めや詰めの局面では金矢倉より脆くなります。',
 ARRAY['gin_yagura', 'castle', 'yagura'],
 'https://ja.wikipedia.org/wiki/矢倉囲い', 'CC-BY-SA-4.0', 'engine'),

('片矢倉',
 '天野矢倉とも呼ばれる形で、玉7八、金6八、銀7七、金6七の配置です。金矢倉より玉が中央寄りにいるため、終盤で玉が広く使える利点があります。そのぶん端からの攻めに対する備えは金矢倉に劣ります。',
 ARRAY['kata_yagura', 'castle', 'yagura'],
 'https://ja.wikipedia.org/wiki/矢倉囲い', 'CC-BY-SA-4.0', 'engine'),

('舟囲い',
 '8八角、7八玉、7九銀、6九金、5八金、4八銀の形で、居飛車が振り飛車に対して用いるもっとも基本的な囲いです。三手ほどで組めるため急戦に向いており、相手が本格的に囲う前に仕掛けることができます。堅さは美濃囲いや穴熊に劣るので、長い戦いになる前に決着をつけるか、穴熊や左美濃へ発展させるのが定跡です。',
 ARRAY['fune', 'castle'],
 'https://ja.wikipedia.org/wiki/舟囲い', 'CC-BY-SA-4.0', 'engine'),

-- ── 戦法 ─────────────────────────────────────────────────────────────────────
-- 定義が飛の筋ひとつなので、옮겨온 서술이 없다. 그래서 出典은 우리 정의 파일이다.

('中飛車',
 '飛車を5八へ振る振り飛車です。盤の中央に飛車を構えるため攻めの的が分かりやすく、初心者にも扱いやすい戦法とされます。中央から一気に押し込む力が強い一方、飛車が相手の攻めの目標にもなりやすいので、玉を美濃囲いなどでしっかり囲ってから動くのが基本です。',
 ARRAY['naka_bisha', 'formation', 'furibisha'],
 'https://github.com/jovid18/show-gi/blob/main/apps/server/internal/tag/tag.go', 'engine-derived', 'engine'),

('四間飛車',
 '飛車を6八へ振る振り飛車で、振り飛車のなかでもっとも人気のある戦法です。美濃囲いと組み合わせるのが基本形で、相手の攻めを受け止めてから反撃する指し方に向いています。飛車と玉が離れているため、玉のまわりを攻められても飛車が捕まりにくいのが利点です。',
 ARRAY['shiken_bisha', 'formation', 'furibisha'],
 'https://github.com/jovid18/show-gi/blob/main/apps/server/internal/tag/tag.go', 'engine-derived', 'engine'),

('三間飛車',
 '飛車を7八へ振る振り飛車です。飛車の前の歩を伸ばして石田流に組み替えたり、そのまま相手の弱点を突いたりと、攻めの形を作りやすい戦法です。四間飛車より攻撃的で、早い動きを狙えるのが特徴です。',
 ARRAY['sanken_bisha', 'formation', 'furibisha'],
 'https://github.com/jovid18/show-gi/blob/main/apps/server/internal/tag/tag.go', 'engine-derived', 'engine'),

('向かい飛車',
 '飛車を8八へ振り、相手の飛車と正面から向き合う振り飛車です。相手の飛車の筋に自分の飛車を置くため、飛車先の歩を突き合う激しい展開になりやすい戦法です。角の交換から一気に攻め合う形が多く、深い読みの力が求められます。',
 ARRAY['mukai_bisha', 'formation', 'furibisha'],
 'https://github.com/jovid18/show-gi/blob/main/apps/server/internal/tag/tag.go', 'engine-derived', 'engine'),

-- ── 블런더 카테고리 ───────────────────────────────────────────────────────────
-- 判定は `internal/intervene` の決定的なルールで、엔진 평가치와 詰み 거리만 입력이다.

('詰みを逃す',
 '自分から相手を詰ませる手順があったのに、別の手を指してその機会を失うことです。終盤では勝率の落ち幅が飽和してしまうため、この見落としは勝率では測れません。そこで詰みまでの手数そのものを基準にして判定します。五手詰めまでは実戦によく現れ、詰将棋の基本単位でもあるので、ここで決めきれないのは学ぶ価値のある失敗です。',
 ARRAY['missed_mate', 'blunder'],
 'https://github.com/jovid18/show-gi/blob/main/apps/server/internal/intervene/category.go', 'engine-derived', 'engine'),

('タダ捨て（駒をただで渡す）',
 '相手が合法手でその駒を取れて、しかも取り返す駒がない場所に駒を置いてしまうことです。利きの数だけで数えると、紐で縛られて動けない駒まで数えてしまい、実際には取れないのに「取られます」と伝えてしまいます。そこで実際に指せる手だけを見て判定します。銀以上をタダで渡すと、もっとも緩い入門者の基準でも介入がかかります。',
 ARRAY['hangs_piece', 'blunder'],
 'https://github.com/jovid18/show-gi/blob/main/apps/server/internal/intervene/category.go', 'engine-derived', 'engine'),

('浅い得に釣られる',
 '浅く読むと得に見えるのに、深く読むと損になっている手のことです。初心者が構造的にはまりやすい罠で、他の失敗が「見えていたはずのものを見落とした」であるのに対し、これは「読みの深さが足りないために必ず起こる」種類の失敗です。深さごとの評価値の曲線をそのまま見せると、どこから形勢が逆転するのかが目で分かります。',
 ARRAY['shallow_trap', 'blunder'],
 'https://github.com/jovid18/show-gi/blob/main/apps/server/internal/intervene/category.go', 'engine-derived', 'engine'),

('成らずに動かす',
 '最善手と同じ動きなのに、成るかどうかだけが違う手です。動きが同じなのですから悪い理由は成らなかったことだけで、他の理由を挙げると説明が必ず外れます。とくに敵陣から出るときにも成れることは見落としやすい点です。駒が敵陣に入るときだけでなく、敵陣から出るときにも成ることができます。',
 ARRAY['unpromoted', 'blunder'],
 'https://github.com/jovid18/show-gi/blob/main/apps/server/internal/intervene/category.go', 'engine-derived', 'engine'),

('駒を取ることに目を奪われる',
 '駒を取ったものの、取り返されるか玉がより危なくなってしまう手です。ただ取れたというだけでは当てはまりません。取ったことが悪い理由ではないのに、それを理由として伝えてしまうと、次に同じ形が出たときに正しい手を指せなくなります。駒の損得と玉の安全は別の物差しで、終盤に近づくほど玉の安全のほうが重くなります。',
 ARRAY['greedy_capture', 'blunder'],
 'https://github.com/jovid18/show-gi/blob/main/apps/server/internal/intervene/category.go', 'engine-derived', 'engine'),

('追う手（意味のない王手）',
 '得にならない王手をかけて、そのぶん形勢を損ねる手です。王手は相手の応手が限られるので指したくなりますが、続く攻めがないまま王手をかけると、相手の玉が安全な場所へ逃げるのを手伝うことになります。王手をかける前に、その後にどう続けるのかを決めておくことが大切です。',
 ARRAY['idle_check', 'blunder'],
 'https://github.com/jovid18/show-gi/blob/main/apps/server/internal/intervene/category.go', 'engine-derived', 'engine'),

('玉のまわりの守りを放す',
 '玉の周囲八マスを守る利きが減り、同時に相手の攻めの利きが増える手です。両方を同時に見るのは、片方だけを見ると玉を自陣へ動かす正常な手まで引っかかってしまうからです。囲いは一枚欠けると急に脆くなるので、攻めに使う駒を守りから引き抜くときは、その一枚が守っていたものを確かめる必要があります。',
 ARRAY['king_exposed', 'blunder'],
 'https://github.com/jovid18/show-gi/blob/main/apps/server/internal/intervene/category.go', 'engine-derived', 'engine'),

('分類なし',
 '駒を捨てたわけでも王手をかけたわけでも玉を薄くしたわけでもなく、ただ形勢を損ねている手です。無理に分類すると説明が外れてしまい、それがこの製品でもっとも大きな失敗になります。理由を挙げられないときは挙げません。代わりに、その手のあとで相手がどう咎めてくるのかを一手ずつ見せます。',
 ARRAY['other', 'blunder'],
 'https://github.com/jovid18/show-gi/blob/main/apps/server/internal/intervene/category.go', 'engine-derived', 'engine');

COMMIT;
