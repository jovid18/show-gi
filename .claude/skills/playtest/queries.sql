-- 플레이테스트 리포트용 질의. **읽기 전용이다.**
--
-- 쓰기 문(INSERT·UPDATE·DELETE·DDL)을 이 파일에 넣지 않는다. 대국 데이터는 앱이 쓰는
-- 것이지 우리가 쓰는 것이 아니고, 여기 있는 것은 prod에 그대로 돈다.
--
--   :from  회차 시작 시각 (UTC). 스킬 §1에서 적어둔 값
--
-- psql 이면 -v from="2026-08-11T09:00:00Z" 로 넘긴다. 그냥 손으로 박아 넣어도 된다.

\set from '2026-01-01T00:00:00Z'

-- 1. 이 회차의 판 ------------------------------------------------------------
select id,
       my_color,
       started_at,
       finished_at,
       result,
       round(extract(epoch from (finished_at - started_at)) / 60) as minutes
  from games
 where started_at >= :'from'
 order by started_at;

-- 2. 판별 手数 ---------------------------------------------------------------
select g.id,
       g.result,
       count(m.ply) as plies
  from games g
  left join game_moves m on m.game_id = g.id
 where g.started_at >= :'from'
 group by g.id, g.result
 order by g.id;

-- 3. 개입 전문 ---------------------------------------------------------------
select i.game_id,
       i.ply,
       i.category,
       round(i.delta_win::numeric, 3) as dw,
       i.retracted_usi
  from interventions i
  join games g on g.id = i.game_id
 where g.started_at >= :'from'
 order by i.game_id, i.ply, i.id;

-- 4. 카테고리 분포 — 판별과 합계 ---------------------------------------------
select coalesce(i.game_id::text, '합계') as game,
       i.category,
       count(*) as n
  from interventions i
  join games g on g.id = i.game_id
 where g.started_at >= :'from'
 group by grouping sets ((i.game_id, i.category), (i.category))
 order by game, n desc;

-- 5. 한 국면에서 몇 번 막혔나 — 힌트 계단이 열리는 조건과 대응된다 -----------
select i.game_id,
       i.ply,
       count(*) as retracted_at_this_ply
  from interventions i
  join games g on g.id = i.game_id
 where g.started_at >= :'from'
 group by i.game_id, i.ply
having count(*) > 1
 order by count(*) desc, i.game_id, i.ply;

-- 6. 08-playtest.md §11에서 관측된 것들 — prod에서도 같은지 --------------------
-- 로컬에서는 positions 가 테스트 픽스처 2행뿐이었고 game_moves.sfen_key·eval_cp 가
-- 전부 NULL 이었다. 달라졌으면 그것이 이번 회차의 발견이다.

select count(*) as positions_rows,
       count(*) filter (where sfen_key like 'test/%') as test_fixtures
  from positions;

select count(*) as moves,
       count(sfen_key) as sfen_key_not_null,
       count(eval_cp) as eval_cp_not_null
  from game_moves m
  join games g on g.id = m.game_id
 where g.started_at >= :'from';
