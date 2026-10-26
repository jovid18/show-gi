// 판·駒台·成り 선택처럼 여러 화면이 함께 쓰는 판 컴포넌트의 문자열.

export const ja = {
  toggle: {
    sound: { label: '駒音', title: '駒を置く音を鳴らす' },
    exposure: { label: '相手の利き', title: '相手の駒が利いていて、こちらが受けていないマスに影が落ちる' },
    flip: { label: '盤を反転', title: '盤の上下を入れかえる' },
  },
  hand: {
    them: '相手',
    you: 'あなた',
    empty: 'なし',
    // 駒 이름은 `models/piece.ts` 의 것을 그대로 받는다.
    count: (name: string, n: number) => `${name} ${n}枚`,
  },
  promotion: {
    label: '成りの選択',
    ask: '成りますか。',
    promote: '成る',
    decline: '不成',
  },
};

export const ko: typeof ja = {
  toggle: {
    sound: { label: '말 소리', title: '말을 놓는 소리를 냅니다' },
    exposure: { label: '상대의 노림', title: '상대 말이 노리고 있는데 내 쪽이 지키지 않는 칸에 그림자가 집니다' },
    flip: { label: '판 뒤집기', title: '판의 위아래를 바꿉니다' },
  },
  hand: {
    them: '상대',
    you: '당신',
    empty: '없음',
    count: (name: string, n: number) => `${name} ${n}개`,
  },
  promotion: {
    label: '승격 선택',
    ask: '승격하겠습니까?',
    promote: '승격',
    decline: '불승격',
  },
};
