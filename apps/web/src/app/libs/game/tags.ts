import type { Messages } from '@/i18n';
import type { StyleTag } from '@/protocol/game';

/**
 * 태그 축의 이름.
 *
 * 이름(`nameJa`)은 서버가 주고 축은 화면이 안다. 두 벌이 될 위험은 이름 쪽에 있고, 축은
 * 코드 넷이 전부라 옮길 것이 없다. 문자열은 `i18n/ns/profile.ts` 의 `tagKind` 한 자리에 둔다.
 */
export function tagKindName(t: Messages, kind: StyleTag['kind']): string {
  return t.profile.tagKind[kind];
}
