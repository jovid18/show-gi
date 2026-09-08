#!/usr/bin/env python3
"""마크다운 굵게를 세고, 규약이 걷으라고 한 자리를 걷는다.

CLAUDE.md 「굵게는 머리말 자리에만 쓴다」의 검사기다. 인자 없이 부르면 세기만 하고,
--fix 면 고친다. 코드 펜스와 인라인 코드는 건드리지 않는다.

남기는 자리는 셋뿐이다 — 불릿·단락·표 칸이 굵게로 시작하는 머리말. 문장 속 굵게는
전부 걷는다. 개수를 세지 않고 자리를 보는 이유는, 굵게가 스무 개인 정의 목록은
읽히지만 굵게가 다섯 개인 한 문단은 안 읽히기 때문이다.
"""
import collections
import os
import re
import sys

ROOTS = ('docs', 'apps', 'deploy', 'infra', 'tools', '.claude')
SKIP = ('node_modules', 'docs/images', '.git', 'dist')
머리말_상한 = 35  # 보이는 길이로 잰다. 넘으면 라벨이 아니라 문장이다

BOLD = re.compile(r'\*\*(.+?)\*\*')
LIST = re.compile(r'^(?:[-*+]|\d+\.)\s')
FENCE = re.compile(r'^\s*(```|~~~)')
HEADING = re.compile(r'^#{1,6}\s')

남긴다 = '머리말'


def 파일들(root='.'):
    for dp, dns, fns in os.walk(root):
        dns[:] = [d for d in dns if not any(s in os.path.join(dp, d) for s in SKIP)]
        rel = os.path.relpath(dp, root)
        if rel != '.' and not rel.startswith(ROOTS):
            continue
        for fn in sorted(fns):
            if fn.endswith('.md'):
                yield os.path.relpath(os.path.join(dp, fn), root)


def 가린_줄(line):
    """인라인 코드를 같은 길이의 널로 덮은 줄. 자리 번호가 원본과 그대로 맞는다.

    구간으로 자르지 않고 덮는 이유는 굵게가 코드를 품는 자리가 있어서다 —
    `**`positions` 가 식는다**` 를 자르면 여는 별표와 닫는 별표가 다른 구간에 떨어진다.
    """
    return re.sub(r'`[^`]*`', lambda m: '\x00' * len(m.group(0)), line)


def 보이는_길이(s):
    """마크업을 뺀 길이. 인라인 코드는 한 글자로, 링크는 글자만 남기고 센다.

    원문 길이로 재면 `[docs/06-status.md](docs/06-status.md)를 먼저 읽는다` 가 57자로
    잡혀서, 눈에는 짧은 라벨이 문장으로 걸린다.
    """
    s = re.sub(r'`[^`]*`', '\x00', s)
    s = re.sub(r'\[([^\]]*)\]\([^)]*\)', r'\1', s)
    return len(s)


def 머리말_자리(line):
    """이 줄에서 머리말 굵게가 시작될 수 있는 자리. 불릿 뒤·단락 첫 글자·표 칸이 열린 뒤."""
    s = line.lstrip()
    여백 = len(line) - len(s)
    if s.startswith('|'):
        return {m.end() for m in re.finditer(r'\|\s*', line)}
    불릿 = LIST.match(s)
    if 불릿:
        return {여백 + 불릿.end()}
    return {여백}


def 갈래(m, line):
    s = line.lstrip()
    n = 보이는_길이(line[m.start(1):m.end(1)])
    if HEADING.match(s):
        return '제목 안'
    if s.startswith('>'):
        return '인용 안'
    if m.start() in 머리말_자리(line):
        return 남긴다 if n <= 머리말_상한 else '머리말(긺)'
    return '표 칸 문장 속' if s.startswith('|') else '문장 속'


def 훑기(path):
    """(줄번호, 갈래, 굵게 내용) 을 낸다."""
    fence = False
    for ln, line in enumerate(open(path, encoding='utf-8'), 1):
        if FENCE.match(line):
            fence = not fence
            continue
        if fence:
            continue
        for m in BOLD.finditer(가린_줄(line)):
            yield ln, 갈래(m, line), line[m.start(1):m.end(1)]


def 고치기(path):
    fence = False
    out, 걷은수 = [], collections.Counter()
    for line in open(path, encoding='utf-8'):
        if FENCE.match(line):
            fence = not fence
            out.append(line)
            continue
        if fence:
            out.append(line)
            continue
        조각, i = [], 0
        for m in BOLD.finditer(가린_줄(line)):
            조각.append(line[i:m.start()])
            안 = line[m.start(1):m.end(1)]
            g = 갈래(m, line)
            if g == 남긴다:
                조각.append('**' + 안 + '**')
            else:
                조각.append(안)
                걷은수[g] += 1
            i = m.end()
        조각.append(line[i:])
        out.append(''.join(조각))
    if sum(걷은수.values()):
        open(path, 'w', encoding='utf-8').write(''.join(out))
    return 걷은수


def main():
    fix = '--fix' in sys.argv
    총, 파일별 = collections.Counter(), collections.Counter()
    for p in 파일들():
        if fix:
            c = 고치기(p)
            총.update(c)
            if sum(c.values()):
                파일별[p] = sum(c.values())
        else:
            for _, g, _ in 훑기(p):
                총[g] += 1
                파일별[p] += 1
    print(f'{"걷은 굵게" if fix else "굵게":22}{"곳":>7}')
    for k, v in 총.most_common():
        print(f'{k:22}{v:>7}')
    print(f'{"합계":22}{sum(총.values()):>7}  파일 {len(파일별)}개')
    if not fix:
        for p, n in 파일별.most_common(8):
            print(f'  {n:5}  {p}')
    return 1 if (not fix and sum(v for k, v in 총.items() if k != 남긴다)) else 0


if __name__ == '__main__':
    sys.exit(main())
