import type { StyleTag } from '@/protocol/game';

/**
 * 태그 축의 일본어 이름.
 *
 * 이름(`nameJa`)은 서버가 주고 축은 화면이 안다. 두 벌이 될 위험은 이름 쪽에 있고, 축은
 * 코드 넷이 전부라 옮길 것이 없다.
 *
 * 한 자리에 둔다. 따로 두면 한쪽에서 「戦法」이고 다른 쪽에서 「戦型」이 된다.
 *
 * `kind` 가 늘면 타입이 여기서 컴파일을 막는다.
 */
export const TAG_KIND_JA: Record<StyleTag['kind'], string> = {
  castle: '囲い',
  formation: '戦法',
  opening: '戦型',
  tesuji: '手筋',
};
