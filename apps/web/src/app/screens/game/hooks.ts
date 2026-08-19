// 대국 화면만 쓰는 훅. 화면 하나의 것이라 여기 있다 — `hooks/` 는 여러 화면이
// 같이 쓰는 자리이고(useGame·useWhatIf), 이건 이 폴더를 벗어나면 쓸 곳이 없다.

import { useEffect, useRef, useState } from 'react';

import type { StyleTag } from '@/protocol/game';

/**
 * 새로 붙은 이름을 한 번만 알린다 — 将棋ウォーズ가 하는 그것이다.
 *
 * 사이드바에 상시 띄워 놓는 것으로 먼저 만들었는데, 브라우저에서 보니 「中飛車」가 상태
 * 문구 아래에 홀로 떠서 무엇을 가리키는 말인지 알 수 없었다 — 棋譜의 소제목처럼도
 * 읽혔다. 이름 옆에 「戦法」 같은 라벨을 붙여 고칠 수도 있었지만, 그건 사이드바에
 * 설명을 하나 더 얹는 일이다.
 *
 * 사건으로 만들면 설명이 필요 없어진다. 짜는 순간 판 위에 잠깐 뜨고 사라지면
 * 「방금 내가 이걸 만들었다」가 위치와 타이밍으로 전달된다. 03-frontend.md 의 「평시엔
 * 조용하게」와도 맞는다 — 지나가면 화면에 아무것도 남지 않는다.
 *
 * 회차를 기억한다. 囲い는 깨졌다가 다시 짜이므로, 코드를 기억하지 않으면 같은
 * 이름이 여러 번 뜬다. 한 대국에서 이름 하나는 한 번이다.
 */
export function useTagAnnounce(tags: StyleTag[] | undefined, ply: number): [StyleTag | null, () => void] {
  const seen = useRef(new Set<string>());
  const [showing, setShowing] = useState<StyleTag | null>(null);

  // 첫 수 전에는 알리지 않는다. 새 대국에서 기억을 비우는 자리이기도 하다.
  useEffect(() => {
    if (ply === 0) {
      seen.current = new Set();
      setShowing(null);
    }
  }, [ply]);

  useEffect(() => {
    const fresh = tags?.find((t) => !seen.current.has(t.code));
    if (!fresh || ply === 0) return;

    seen.current.add(fresh.code);
    setShowing(fresh);
  }, [tags, ply]);

  // 언마운트를 타이머로 하지 않는다. `setTimeout` 으로 지우면 길이가 CSS 애니메이션과
  // 두 벌이 되고, 어긋나면 요소가 DOM 에 남은 채 `opacity: 0` 으로 보이지 않는다 —
  // 에러도 안 나고 화면에도 안 나온다. 애니메이션이 끝나는 것을 신호로 쓰면 길이의
  // 주인이 CSS 하나가 된다.
  return [showing, () => setShowing(null)];
}
