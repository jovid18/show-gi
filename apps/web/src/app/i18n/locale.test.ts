import { describe, expect, it } from 'vitest';

import { localeFrom } from './locale';

describe('localeFrom', () => {
  it('저장된 값이 브라우저 언어보다 앞선다', () => {
    expect(localeFrom('ja', ['ko-KR'])).toBe('ja');
    expect(localeFrom('ko', ['ja-JP'])).toBe('ko');
  });

  it('저장된 값이 없으면 브라우저 첫 언어가 ko 일 때만 한국어다', () => {
    expect(localeFrom(null, ['ko-KR', 'ja'])).toBe('ko');
    expect(localeFrom(null, ['KO'])).toBe('ko');
    expect(localeFrom(null, ['en-US', 'ko'])).toBe('ja');
    expect(localeFrom(null, [])).toBe('ja');
  });

  it('모르는 저장값은 없는 것과 같다', () => {
    expect(localeFrom('en', ['ko-KR'])).toBe('ko');
  });
});
