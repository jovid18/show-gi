import { useEffect, useRef, type MouseEvent } from 'react';
import { createPortal } from 'react-dom';

/**
 * 카드 밖을 눌러도 초점이 버튼에 남는다.
 *
 * 이 모달은 뒷막 클릭을 「닫기」로 받지 않는다. 막지 않으면 그 누름이 버튼의 초점을
 * `document.body` 로 옮긴다. 카드 자신이 초점을 받을 수 없어서 카드 안의 글자도 같다.
 */
function hold(e: MouseEvent<HTMLDivElement>): void {
  if (!(e.target instanceof HTMLButtonElement)) e.preventDefault();
}

interface PromotionProps {
  /** 사람이 고른 것. 참이 成る다. */
  onChoose: (promote: boolean) => void;
}

/**
 * 成る·不成을 묻는 모달. 다섯 화면이 같은 물음을 쓴다.
 *
 * 화면을 덮는다. 판 옆에 두면 눈이 도착 칸에 있어서 물었다는 것 자체가 보이지 않고 「판이
 * 움직이지 않는다」로 읽힌다(journal §99). 답하기 전에는 판이 실제로 멈춰 있다.
 *
 * 취소가 없다. 가능한 답이 둘뿐이라 Escape 도 뒷막 클릭도 받지 않는다. 닫아 버리면 물음이
 * 사라진 채로 판이 잠긴다.
 *
 * `document.body` 로 포털한다. 개입 중에는 `.game-board` 가 z-index 50 의 쌓임 문맥을
 * 만들어서, 그 안에 두면 개입 카드(60)가 이 모달 위에 온다.
 */
export function Promotion({ onChoose }: PromotionProps) {
  const cardRef = useRef<HTMLDivElement>(null);
  const promoteRef = useRef<HTMLButtonElement>(null);

  // 초점을 여기로 가져온다. 되돌려 줄 자리는 없다. 착수를 짜는 순간 판의 버튼이 전부
  // disabled 가 되어 브라우저가 그 칸의 초점을 먼저 떨어뜨린다(journal §99).
  //
  // 뒤는 움직이지 않는다. 뒷막 위에서 굴린 것이 문서를 굴리면 「판이 멈춰 있다」고 말하는
  // 모달 뒤로 판이 흘러 나간다.
  useEffect(() => {
    promoteRef.current?.focus();

    const scroll = document.body.style.overflow;
    const pad = document.body.style.paddingRight;
    // 자리 잡는 스크롤바가 있으면 그 폭을 메운다. 메우지 않으면 잠그는 순간 판과 駒台가
    // 옆으로 뛴다. macOS 는 겹쳐 그리는 스크롤바라 0 이 나오고, 이 자리는 Windows·Linux
    // 에서만 제구실을 한다.
    const gap = window.innerWidth - document.documentElement.clientWidth;
    document.body.style.overflow = 'hidden';
    if (gap > 0) document.body.style.paddingRight = `${gap}px`;

    return () => {
      document.body.style.overflow = scroll;
      document.body.style.paddingRight = pad;
    };
  }, []);

  /**
   * Tab 을 카드 안에 가둔다. 마우스는 뒷막이 막지만 키보드는 막히지 않고, 나가면 초점이
   * 投了 에 닿는다. 그 버튼은 물음이 떠 있는 동안 잠기지 않는다.
   *
   * `document` 에서 듣는다. 카드에 걸면 초점이 이미 밖으로 나간 뒤에는 이벤트가 카드를
   * 지나지 않는데, 그게 가둬야 하는 바로 그 경우다.
   */
  useEffect(() => {
    const onKey = (e: KeyboardEvent): void => {
      if (e.key !== 'Tab') return;
      const card = cardRef.current;
      const buttons = Array.from(card?.querySelectorAll('button') ?? []);
      const first = buttons[0];
      const last = buttons[buttons.length - 1];
      if (!card || !first || !last) return;

      const here = document.activeElement;
      const inside = here instanceof Node && card.contains(here);
      if (inside && here !== (e.shiftKey ? first : last)) return;

      e.preventDefault();
      (e.shiftKey ? last : first).focus();
    };
    document.addEventListener('keydown', onKey, true);
    return () => document.removeEventListener('keydown', onKey, true);
  }, []);

  return createPortal(
    <div className="promotion-scrim" onMouseDown={hold}>
      <div className="promotion" role="dialog" aria-modal="true" aria-label="成りの選択" ref={cardRef}>
        <p className="promotion__ask">成りますか。</p>

        <div className="promotion__choices">
          <button ref={promoteRef} type="button" className="btn btn--primary" onClick={() => onChoose(true)}>
            成る
          </button>
          <button type="button" className="btn" onClick={() => onChoose(false)}>
            不成
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}
