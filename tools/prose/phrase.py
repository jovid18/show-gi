#!/usr/bin/env python3
"""코드 주석에서 규약이 금지한 맺음 문구를 찾는다.

CLAUDE.md 「결론을 먼저 평서문으로 쓴다」의 검사기다. 문서 산문은 안 본다 —
「A는 B라는 뜻이다」가 무엇을 뜻하는지 설명하는 자리에서는 정상 한국어이고,
그 판단을 정규식에 맡길 수 없다. 주석은 이미 전량 정리했으므로 예외가 없다.
"""
import re
import subprocess
import sys

SKIP = ('internal/store/db/', 'internal/store/migrations/')
금지 = {
    '「~것이 요점이다」': r'것이\s*요점이다',
    '「~이 설계의 전부다」': r'(?:설계의|[이가은는])\s*전부다(?=\s*[—.」)]|\s*$)',
    '「~라는 뜻이다」': r'(?:라는|다는)\s*뜻이다',
    '「~면 끝이다」': r'(?:면|으면)\s*끝이다',
    '「그 순간 ~ 무너진다」': r'그\s*순간\s*\S*\s*(?:깨진|무너|사라진|틀린)',
}


def 주석_줄(path):
    """주석 줄만 낸다. 문자열 리터럴 속 한국어는 UI 문구라 여기서 안 본다."""
    여는말 = ('//', '*', '/*') if path.endswith(('.ts', '.tsx')) else ('//', '--', '#')
    for ln, line in enumerate(open(path, encoding='utf-8', errors='replace'), 1):
        s = line.strip()
        if s.startswith(여는말):
            yield ln, s


def main():
    files = subprocess.run(['git', 'ls-files'], capture_output=True, text=True).stdout.split()
    걸림 = 0
    for f in files:
        if any(s in f for s in SKIP) or not f.endswith(('.go', '.ts', '.tsx', '.tf', '.sql')):
            continue
        for ln, s in 주석_줄(f):
            for 이름, pat in 금지.items():
                if re.search(pat, s):
                    print(f'{f}:{ln}  {이름}\n    {s[:120]}')
                    걸림 += 1
    if 걸림:
        print(f'\n{걸림}곳. 결론을 평서문으로 쓴다 — CLAUDE.md 「주석은 짧은 평서문으로 쓴다」')
    return 1 if 걸림 else 0


if __name__ == '__main__':
    sys.exit(main())
