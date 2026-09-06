"""내보낸 SVG 하나를 밝은 판과 어두운 판으로 가른다.

drawio 의 --theme auto 는 색을 CSS light-dark(밝은값, 어두운값) 로 남긴다. 그 함수는
읽는 사람 OS 의 설정을 따라가는데 GitHub 테마는 사이트 설정이라 둘이 어긋난다 —
그래서 여기서 미리 한쪽으로 풀고, 고르는 것은 README 의 <picture> 가 한다.

--theme dark 로 내보내면 되지 않는다. 그쪽은 light-dark() 의 두 번째 값을 안 집고
밝은 판과 같은 파일을 준다.

삽입된 원본 XML(content 속성)은 안 건드린다. 그걸 풀어 버리면 내보낸 SVG 를 draw.io 로
다시 열었을 때 반대쪽 테마의 색이 사라진다.
"""

import re
import sys


def resolve(text, index):
    """light-dark(a, b) 를 index 번째 인자로 바꾼다. 인자 안에 rgb(...) 가 들어 있어서
    괄호를 세어 가며 최상위 쉼표를 찾는다."""
    out, i = [], 0
    while True:
        j = text.find("light-dark(", i)
        if j < 0:
            out.append(text[i:])
            return "".join(out)
        out.append(text[i:j])
        k, depth, comma = j + len("light-dark("), 1, -1
        while k < len(text) and depth:
            c = text[k]
            if c == "(":
                depth += 1
            elif c == ")":
                depth -= 1
            elif c == "," and depth == 1 and comma < 0:
                comma = k
            k += 1
        args = [text[j + len("light-dark("):comma], text[comma + 1:k - 1]]
        out.append(args[index].strip())
        i = k


def main(src, light_out, dark_out):
    with open(src, encoding="utf-8") as f:
        svg = f.read()

    # content="…" 는 삽입된 원본이다. 자리를 비켜 두고 마지막에 그대로 되돌린다.
    payloads = []

    def stash(m):
        payloads.append(m.group(0))
        return "\x00%d\x00" % (len(payloads) - 1)

    body = re.sub(r'content="[^"]*"', stash, svg)

    for path, index in ((light_out, 0), (dark_out, 1)):
        text = resolve(body, index)
        text = re.sub(r"\x00(\d+)\x00", lambda m: payloads[int(m.group(1))], text)
        with open(path, "w", encoding="utf-8") as f:
            f.write(text)


if __name__ == "__main__":
    main(*sys.argv[1:4])
