import { describe, expect, it } from 'vitest';

import { messagesOf } from './index';

/** 문구 전부를 (경로, 값) 으로 편다. 함수는 `args` 로 불러 돌려준 문장을 본다. */
function leaves(node: unknown, args: unknown[], path = ''): [string, string][] {
  if (typeof node === 'string') return [[path, node]];
  if (typeof node === 'function') return [[path, String((node as (...a: unknown[]) => unknown)(...args))]];
  if (Array.isArray(node)) return node.flatMap((v, i) => leaves(v, args, `${path}[${i}]`));
  return Object.entries(node as Record<string, unknown>).flatMap(([k, v]) =>
    leaves(v, args, path ? `${path}.${k}` : k),
  );
}

/** boolean 인자의 양쪽 갈래를 다 보도록 참·거짓으로 한 번씩 부른다. */
const everyBranch = (node: unknown) => [...leaves(node, [1, 1]), ...leaves(node, [0, 0])];

const KANA = /[぀-ヿ]/;
const HANGUL = /[가-힣]/;

describe('messages', () => {
  // 괄호로 덧붙인 원어(美濃囲い)는 허용한다. 가나가 남으면 옮기다 빠뜨린 것이다.
  it('한국어 문구에 가나가 없다', () => {
    expect(everyBranch(messagesOf('ko')).filter(([, v]) => KANA.test(v.replace(/\([^)]*\)/g, '')))).toEqual([]);
  });

  // 기본 언어는 일본어다. 한글이 섞이면 일본인 사용자 화면에 그대로 나간다.
  it('일본어 문구에 한글이 없다', () => {
    expect(everyBranch(messagesOf('ja')).filter(([, v]) => HANGUL.test(v))).toEqual([]);
  });

  // `typeof ja` 는 인자를 덜 받는 함수를 막지 못한다. 일본어가 쓰는 인자를 한국어가 빠뜨리면
  // 화면에서 숫자가 소리 없이 사라지므로, 인자마다 다른 표식을 넣어 양쪽 출력을 맞춰 본다.
  it('한국어 문구가 일본어 문구의 인자를 모두 쓴다', () => {
    const marks = ['90001', '90002'];
    const ko = new Map(leaves(messagesOf('ko'), marks));
    const dropped = leaves(messagesOf('ja'), marks).filter(([path, v]) =>
      marks.some((m) => v.includes(m) && !ko.get(path)?.includes(m)),
    );
    expect(dropped).toEqual([]);
  });
});
