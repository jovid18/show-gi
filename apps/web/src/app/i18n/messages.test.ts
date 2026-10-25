import { describe, expect, it } from 'vitest';

import { messagesOf } from './index';

/** 문구 전부를 (경로, 값) 으로 편다. 함수는 아무 인자로 불러 돌려준 문장을 본다. */
function leaves(node: unknown, path = ''): [string, string][] {
  if (typeof node === 'string') return [[path, node]];
  if (typeof node === 'function') return [[path, String((node as (...a: unknown[]) => unknown)(1, 1))]];
  if (Array.isArray(node)) return node.flatMap((v, i) => leaves(v, `${path}[${i}]`));
  return Object.entries(node as Record<string, unknown>).flatMap(([k, v]) => leaves(v, path ? `${path}.${k}` : k));
}

const KANA = /[぀-ヿ]/;
const HANGUL = /[가-힣]/;

describe('messages', () => {
  // 괄호로 덧붙인 원어(美濃囲い)는 허용한다. 가나가 남으면 옮기다 빠뜨린 것이다.
  it('한국어 문구에 가나가 없다', () => {
    expect(leaves(messagesOf('ko')).filter(([, v]) => KANA.test(v.replace(/\([^)]*\)/g, '')))).toEqual([]);
  });

  // 기본 언어는 일본어다. 한글이 섞이면 일본인 사용자 화면에 그대로 나간다.
  it('일본어 문구에 한글이 없다', () => {
    expect(leaves(messagesOf('ja')).filter(([, v]) => HANGUL.test(v))).toEqual([]);
  });
});
