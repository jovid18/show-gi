-- 대인전 대기열. 정한 것과 그 근거는 journal §92 · §98.
--
-- **실행은 사람이 DB 클라이언트로 직접 한다** — 배포도 compose도 안 돌린다(deploy/README.md §4).
--
-- **되돌릴 수 있다.** 표 하나를 더할 뿐이고, 이 파일 앞의 서버 이미지는 이 표를 안 읽는다 —
-- 순서를 어느 쪽으로 잡아도 안 깨지고, 병렬 워크트리에서도 안전하다(CLAUDE.md).

BEGIN;

-- 큐가 표인 이유는 방과 성질이 반대이기 때문이다. 방은 초당 수십 번 바뀌는 상태머신이라
-- goroutine 소유가 맞고(match.Table), 큐는 행 몇 개가 몇 초에 한 번 바뀌는데 모든
-- 인스턴스가 같은 것을 봐야 한다(journal §92).
CREATE TABLE match_queue (
    -- 한 사람이 줄에 한 번만 선다. 그래서 「줄에 선다」가 멱등이고, 화면의 재시도가
    -- 그대로 heartbeat 가 된다(queue.StaleAfter).
    user_id bigint PRIMARY KEY REFERENCES users ON DELETE CASCADE,
    -- 줄에 설 때 읽은 레이팅이다. 서 있는 동안 안 바뀐다 — 대기 중에 그 사람의 판이
    -- 끝날 수가 없다. 여기 있는 이유는 짝짓기가 skill_profile 을 조인하지 않게 하는 것
    -- 하나다: 레이팅이 없는 사람은 시드가 들어오고(rating.SeedFromLoss), 안 둔 시간만큼
    -- 되돌린 불확실성도 이미 얹혀 있다(rating.Inflate).
    rating    double precision NOT NULL,
    deviation double precision NOT NULL,
    -- 줄에 선 시각. 밴드가 이 값으로 넓어지고 같은 밴드 안의 순서를 정한다(internal/queue).
    joined_at timestamptz NOT NULL DEFAULT now(),
    -- 마지막으로 다시 물어본 시각. 이 값이 낡으면 줄에서 빠진다 — 탭을 닫은 사람을
    -- 걷어내는 장치가 이것뿐이다.
    seen_at timestamptz NOT NULL DEFAULT now(),
    -- 짝이 잡히면 채워진다. NULL 이 「아직 기다린다」다.
    --
    -- 방 id 를 여기 적는 것이 짝짓기의 결과 전부다. 방 자체는 짝을 지은 인스턴스의
    -- 메모리에 선다(match.Hub) — 그래서 이 값은 「거기로 가라」는 쪽지이고, 두 사람이
    -- 각자 자기 행에서 그것을 읽는다.
    room_id text,
    -- 그 방에서 이 사람이 잡을 쪽. 큐는 平手 확정 · 先手 랜덤이다(journal §92).
    color text CHECK (color IN ('b', 'w')),
    -- 짝이 잡힌 시각. 안 찾아간 자리를 걷는 데 쓴다(queue.PickupTTL).
    matched_at timestamptz
);

-- 부분 인덱스다. 짝짓기가 보는 것은 아직 안 잡힌 행뿐이고, 잡힌 행은 주인이 자기
-- user_id 로 한 번 읽고 지운다.
--
-- 정렬 칸이 joined_at 이다. 고르는 규칙이 밴드 안에서 FIFO 라(internal/queue) 질의가
-- 이 순서로 잠근다.
CREATE INDEX match_queue_waiting_idx ON match_queue (joined_at) WHERE room_id IS NULL;

COMMIT;
