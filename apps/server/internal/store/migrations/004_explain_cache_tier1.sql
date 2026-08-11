-- Tier 1 문구를 미리 만들어 넣는다 — 이 층의 개입은 런타임 LLM 왕복이 0이 된다.
--
-- **손으로 고치지 않는다.** internal/explain 의 생성기가 만든 파일이고, 고칠 일이 생기면
-- 프롬프트를 고쳐 다시 만든다:
--
--     set -a && . ../../.env && set +a
--     SHOWGI_GENERATE_TIER1=1 go test ./internal/explain/ -run GenerateTier1 -v
--
-- 문장은 실제 라우터가 쓴 것이고 런타임과 **같은 경로**(Layered.Explain)로 만들어졌다 —
-- 같은 검사(explain.clean)를 지났고, 그래서 한글도 지어낸 칸도 들어 있지 않다.
--
-- 키는 Go가 만든다(explain.Facts.Key). 행마다 붙은 주석이 해시하기 전의 그 값이고,
-- 맨 앞의 v1 이 promptVersion 이다 — **프롬프트를 고치면 그 숫자가 올라가고 아래 행은
-- 전부 아무도 찾지 않는 키가 된다.** TestTier1MigrationMatchesFacts 가 그것을 잡는다.
--
-- **추가만 하는 마이그레이션이다.** ON CONFLICT DO NOTHING 이라 이미 런타임이 만들어 둔
-- 문장을 덮지 않고("같은 실수에는 같은 설명"), 두 번 돌려도 같은 상태가 된다.
--
-- 21행인 이유는 카테고리 8 × 레벨 3 = 24 에서 hangs_piece 와 greedy_capture 가 빠지고
-- (그 둘은 분류 조건이 곧 Tier 2 조건이라 Tier 1로 오지 않는다) other 가 둘로 갈리기
-- 때문이다. 자세한 것은 internal/explain/tier1_test.go 의 tier1Facts 주석.
--
-- 이 행들은 hits=0 으로 시작한다. explain_cache 의 entries 가 이제 「만들어 둔 것 +
-- 런타임이 만든 것」이라, 히트율을 그 두 값으로만 세면 실제보다 낮게 나온다.

BEGIN;

INSERT INTO explain_cache (key, body, model) VALUES
-- v2|blunder|missed_mate|0|mate=true|known=false|moved=|cap=|atk=0|def=false|thr=
('270be7514997051f1ff395cebe45663757ec6f72a3c3de12ad6a5f966f5bbbb2',
 '自分が相手を詰ませられる手があったのに、この手では逃してしまいます。',
 'gemini-3.5-flash-lite'),
-- v2|blunder|shallow_trap|0|mate=false|known=false|moved=|cap=|atk=0|def=false|thr=
('066058c6ab829ef696177f3368ec6f26b281e990fefe8c854917600e3e209df7',
 '一手先だけ得に見えますが、その先で形勢が入れ替わります。',
 'gemini-3.5-flash-lite'),
-- v2|blunder|unpromoted|0|mate=false|known=false|moved=|cap=|atk=0|def=false|thr=
('857057db1e42ff0743c55e7ed709f624c0699a5a271c3fc7fdb8d25611622299',
 '駒の動きは正しいですが、成っていません。敵陣から出る時も成ることができます。',
 'gemini-3.5-flash-lite'),
-- v2|blunder|idle_check|0|mate=false|known=false|moved=|cap=|atk=0|def=false|thr=
('8451dc8dc4773bb48425a70daf86155903b4b126fd21b56e0523f456b24e113e',
 '王手がかかりますが、そのあとの続きがありません。相手に手番を渡してしまいます。',
 'gemini-3.5-flash-lite'),
-- v2|blunder|king_exposed|0|mate=false|known=false|moved=|cap=|atk=0|def=false|thr=
('54a7f9d62b45d1c0c50ad679730185313285973bcf2c84c3283d7baf118a1dd5',
 '玉のまわりの守りが減り、相手の攻めの利きが増えました。',
 'gemini-3.5-flash-lite'),
-- v2|blunder|other|0|mate=false|known=false|moved=|cap=|atk=0|def=false|thr=
('4e97fca2c307242e74e30ca3ee426eb7a7dc796487b32362a8b8ddc71379526f',
 '形勢を大きく損ねる手です。指すのをやめましょう。',
 'gemini-3.5-flash-lite'),
-- v2|blunder|other|0|mate=false|known=true|moved=|cap=|atk=0|def=false|thr=
('cba73832fc57eaf8c1ec449482bac0d90c52e7bfe41579f44e0d06d1a66b2980',
 '形勢を大きく損ねる手です。指すのをやめましょう。',
 'gemini-3.5-flash-lite'),
-- v2|blunder|missed_mate|1|mate=true|known=false|moved=|cap=|atk=0|def=false|thr=
('794d88fc0260f7162f937e2c21f43051e73cd1e9a4084ad362f931e4b20797d0',
 '相手を詰ませられる手があったのに、この手では逃してしまいます。',
 'gemini-3.5-flash-lite'),
-- v2|blunder|shallow_trap|1|mate=false|known=false|moved=|cap=|atk=0|def=false|thr=
('e0321834581db91384946a8626c846441d11d849d3e2037f74b7b30fc37c3546',
 '一手先だけが得に見えますが、その先で形勢が入れ替わります。',
 'gemini-3.5-flash-lite'),
-- v2|blunder|unpromoted|1|mate=false|known=false|moved=|cap=|atk=0|def=false|thr=
('94eb4041ea44b7514a23225390736386f856651279ac54a3198e3e6c535efd43',
 '敵陣から出る手も成ることができます。成るのを見落としています。',
 'gemini-3.5-flash-lite'),
-- v2|blunder|idle_check|1|mate=false|known=false|moved=|cap=|atk=0|def=false|thr=
('6c5d7b4c8d760e21c255b5b91938dd72d120eb02918400f8a4c29cd27ebf42e9',
 '王手はかかりますが、そのあとの続きがありません。相手に手番を渡してしまいます。',
 'gemini-3.5-flash-lite'),
-- v2|blunder|king_exposed|1|mate=false|known=false|moved=|cap=|atk=0|def=false|thr=
('63dcb1176df765aed93334c81ee44ba2254fbd009d45b67e36667426ffa15845',
 '玉のまわりの守りが減って、相手の攻めの利きが増えました。',
 'gemini-3.5-flash-lite'),
-- v2|blunder|other|1|mate=false|known=false|moved=|cap=|atk=0|def=false|thr=
('8a1c8dc0e27e8a9d5c26c75e6262198073346bb3b0d5b4fbbbdddc6f3fb2bb28',
 'その手は形勢を大きく損ねます。別の手を考えましょう。',
 'gemini-3.5-flash-lite'),
-- v2|blunder|other|1|mate=false|known=true|moved=|cap=|atk=0|def=false|thr=
('d71b2345ac7e75d2747d4ebf0f843af25137982f32e86bd52a9cfd77b09563a1',
 'その手は形勢を大きく損ねます。別の手を考えましょう。',
 'gemini-3.5-flash-lite'),
-- v2|blunder|missed_mate|2|mate=true|known=false|moved=|cap=|atk=0|def=false|thr=
('5eedd2c2061ca19be800fa3a9fde7c9debf1bd89a45b3127fbb03eeda11d7ffb',
 '自分が相手を詰ませられる手があったのに、この手で逃してしまいます。',
 'gemini-3.5-flash-lite'),
-- v2|blunder|shallow_trap|2|mate=false|known=false|moved=|cap=|atk=0|def=false|thr=
('e20fde2c4a3b12f24b4c57a1b7bee39d74aa8419ad11a0d225f7c9cb9240fd41',
 '一手先だけ得に見えますが、その先で形勢が入れ替わります。',
 'gemini-3.5-flash-lite'),
-- v2|blunder|unpromoted|2|mate=false|known=false|moved=|cap=|atk=0|def=false|thr=
('0e6761f9c649639d5277351bee77b6a87350560c8ab1463ed2eb533a316e5978',
 '動きは正しいですが成っていません。敵陣から出る手も成れるのを見落としています。',
 'gemini-3.5-flash-lite'),
-- v2|blunder|idle_check|2|mate=false|known=false|moved=|cap=|atk=0|def=false|thr=
('29ec9d113a4db6ea32a81fd2ed1d1af7fc17ca7e8ac06dd3dfa98ee14b2496e4',
 '王手になりますが、続きがありません。相手に手番を渡してしまいます。',
 'gemini-3.5-flash-lite'),
-- v2|blunder|king_exposed|2|mate=false|known=false|moved=|cap=|atk=0|def=false|thr=
('6f0dd89f8d670c4f61c9877768792db513a5c9d2e1eaf2cc6675ed5bd4a33f75',
 '自玉のまわりの守りが減り、相手の攻めの利きが増えました。',
 'gemini-3.5-flash-lite'),
-- v2|blunder|other|2|mate=false|known=false|moved=|cap=|atk=0|def=false|thr=
('e4ed7f9469e4f6e5cdc2e28a8c106e82c68fc0822c73a17e128b788c559c63ee',
 'この手は形勢を大きく損ねます。理由は特定できていません。',
 'gemini-3.5-flash-lite'),
-- v2|blunder|other|2|mate=false|known=true|moved=|cap=|atk=0|def=false|thr=
('f45f77081d5fbe0fa4f66571f665d6c890bad8049f0fc7354d8a2d20fcff6470',
 'この手は形勢を大きく損ねます。理由は特定できていません。',
 'gemini-3.5-flash-lite')
ON CONFLICT (key) DO NOTHING;

COMMIT;
