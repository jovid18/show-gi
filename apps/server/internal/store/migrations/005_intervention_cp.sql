-- 물러진 수의 원본 cp를 남긴다.
--
-- `delta_win` 은 **승률 차**다 — `WinRate(best_cp) - WinRate(after_cp)`(intervene/judge.go).
-- 로지스틱을 통과한 두 값의 차라서 **되돌릴 수가 없다**: 미지수가 둘인데 식이 하나다. 그리고
-- 같은 cp 차이가 위치에 따라 다른 낙폭이 된다(0→300cp 는 0.122, 2000→2300cp 는 0.008).
--
-- 두 값은 **판정하는 순간 손에 있었다.** `Judge` 가 그걸로 낙폭을 만들고 원본을 버렸다.
-- §26이 같은 이유로 `game_moves.eval_cp` 를 붙였는데(「판정이 이미 두 국면을 재고 있으니
-- 추가 탐색이 0이다」) 그때 이 표를 안 봤다. 그 구멍을 §39 ⑥이 K를 재채점하려다 찾았다.
--
-- 두 자리가 열린다.
--   ① 되짚기 화면이 물러진 수를 최선수·실제로 둔 수와 **한 축에** 놓을 수 있다.
--      지금은 낙폭(상대값)뿐이라 절대값을 가진 다른 줄들과 못 견준다.
--   ② K 를 바꿔 물러진 수까지 다시 채점할 수 있다. §26의 「원본 cp가 남으므로 재채점할 수
--      있다」는 약속이 **통과한 수에만** 성립했던 것을 여기서 갚는다.
--
-- **추가만 하는 마이그레이션이다.** 둘 다 nullable 이라 지금 도는 서버가 모른 채 그냥 돈다.
-- **과거 행은 영원히 NULL 이다** — 버린 값은 되찾을 수 없다. 화면은 그 자리를 다시 재서
-- 채운다(review 의 whatif).

BEGIN;

ALTER TABLE interventions
    ADD COLUMN best_cp  int,
    ADD COLUMN after_cp int;

COMMENT ON COLUMN interventions.best_cp IS
    '판정 당시 최선수의 cp(수번 측 관점). 제지형만. 과거 행은 NULL';
COMMENT ON COLUMN interventions.after_cp IS
    '물러진 수를 둔 뒤의 cp(수번 측 관점). 제지형만. 과거 행은 NULL';

COMMIT;
