import type { StyleTag } from '@/protocol/game';

/**
 * 태그 축의 일본어 이름.
 *
 * 이름(`nameJa`)은 서버가 주고 축은 화면이 안다. 서버가 주는 것은 판에 뜨는 그 이름
 * 하나이고(tag.Tag), 축은 코드 넷이 전부라 옮길 것이 없다 — 두 벌이 되는 쪽은 이름이지
 * 이 표가 아니다.
 *
 * 한 자리에 둔다. 대국 화면의 이름 알림과 마이페이지의 「組んだ形」이 같은 말을 써야
 * 한다 — 갈라 두면 한쪽에서 「戦法」이고 다른 쪽에서 「戦型」이 된다.
 *
 * `kind` 가 늘면 타입이 여기서 컴파일을 막는다 — 서버에 축을 추가하고 화면을 안 고치는
 * 자리를 이것이 잡는다.
 */
export const TAG_KIND_JA: Record<StyleTag['kind'], string> = {
  castle: '囲い',
  formation: '戦法',
  opening: '戦型',
  tesuji: '手筋',
};
