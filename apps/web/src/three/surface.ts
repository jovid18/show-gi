// 판의 **표면**. 나뭇결·격자·그늘을 셰이더 한 장으로 그린다.
//
// **판을 원근으로 바꾸지 않는다.** 카메라는 정사영이고 정면이다(docs/03-frontend.md §1) —
// 쇼기 駒는 전부 같은 오각형이고 한자로만 갈리므로, 원근을 넣는 순간 뒷줄이 뭉개져
// 학습 앱이 자기 목적과 싸운다. 「시점이 움직였다」는 신호는 개입 때 `.game-board` 가
// CSS로 판째 기울여서 이미 준다 — 여기서 카메라를 또 기울이면 **DOM 駒와 이 표면이
// 어긋난다.** 그래서 이 캔버스는 칸과 픽셀이 맞는 평면이고, 그 위의 駒·빛·광선은 전부
// 지금까지대로 DOM이다.
//
// three.js를 쓰는 값은 여기 하나에서 나온다 — **利き을 스칼라 필드로 합성하는 것.**
// 9×9 매수를 텍스처로 올리고 GPU가 칸 사이를 메워, 그늘이 칸 하나가 아니라 판에 드리운
// 한 겹으로 읽힌다. DOM으로는 칸마다 상자를 81개 겹치는 것 말고는 방법이 없다.

import {
  ClampToEdgeWrapping,
  DataTexture,
  LinearFilter,
  Mesh,
  OrthographicCamera,
  PlaneGeometry,
  RGBAFormat,
  Scene,
  ShaderMaterial,
  Vector2,
  Vector3,
  WebGLRenderer,
} from 'three';

const BOARD_SIZE = 9;

/** 판의 자. `.board` 의 padding(1px)과 칸 사이 gap(1px)이 CSS와 같은 값이어야 한다. */
export interface Layout {
  /** 캔버스의 CSS 픽셀 크기 = `.board` 의 padding box. */
  width: number;
  height: number;
  /** 칸 하나의 변. CSS `--sq` 를 **재서** 받는다 — 화면 폭에 따라 변한다. */
  cell: number;
  /** 칸 사이의 선이자 판의 안쪽 여백. 지금 CSS로는 둘 다 1px이다. */
  gap: number;
}

/** 판이 쓰는 색. **CSS 변수에서 읽어 온다** — 여기 베끼면 팔레트가 두 벌이 된다. */
export interface Palette {
  board: Vector3;
  grid: Vector3;
}

/**
 * 그늘이 밀려드는 모양.
 *
 * 개입(회상)에서는 **물러진 수가 간 칸**에서 퍼져 나간다 — 「네가 두려던 그 자리가
 * 이랬다」가 순서로 읽힌다. 평시 토글에서는 판 한가운데에서 퍼진다.
 */
export interface Reveal {
  /** 중심. 캔버스 왼쪽 위에서 잰 CSS 픽셀이다. */
  x: number;
  y: number;
  /** 0이면 아직 아무것도 안 보이고, 판 대각선보다 크면 전부 보인다. */
  radius: number;
}

interface Uniforms {
  uSize: { value: Vector2 };
  uCell: { value: number };
  uGap: { value: number };
  uBoard: { value: Vector3 };
  uGrid: { value: Vector3 };
  uField: { value: DataTexture };
  uAmount: { value: number };
  uDepth: { value: number };
  uRevealCenter: { value: Vector2 };
  uRevealRadius: { value: number };
  [name: string]: { value: unknown };
}

const VERTEX = /* glsl */ `
  varying vec2 vUv;
  void main() {
    vUv = uv;
    gl_Position = vec4(position.xy, 0.0, 1.0);
  }
`;

// 색 계산을 **감마 공간에서 그대로** 한다. 옆에 붙어 있는 것이 전부 CSS로 칠한 면이고
// (칸의 빛·직전 수·회상 탈색) CSS는 sRGB 값끼리 섞으므로, 여기서만 선형으로 옮겨
// 계산하면 같은 --board 가 두 가지 색으로 보인다. 물리적으로 옳은 쪽보다 **이어 붙었을
// 때 한 판으로 보이는 쪽**이다.
const FRAGMENT = /* glsl */ `
  precision highp float;

  varying vec2 vUv;

  uniform vec2 uSize;
  uniform float uCell;
  uniform float uGap;
  uniform vec3 uBoard;
  uniform vec3 uGrid;
  uniform sampler2D uField;
  uniform float uAmount;
  uniform float uDepth;
  uniform vec2 uRevealCenter;
  uniform float uRevealRadius;

  float hash(vec2 p) {
    return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453);
  }

  float vnoise(vec2 p) {
    vec2 i = floor(p);
    vec2 f = fract(p);
    f = f * f * (3.0 - 2.0 * f);
    return mix(
      mix(hash(i), hash(i + vec2(1.0, 0.0)), f.x),
      mix(hash(i + vec2(0.0, 1.0)), hash(i + vec2(1.0, 1.0)), f.x),
      f.y
    );
  }

  // 榧 판은 柾目 — 결이 **세로로** 곧게 간다. 그래서 y를 눌러 늘인다.
  // 세기는 아주 얕다. 나뭇결이 눈에 띄면 그 위의 한자가 읽기 어려워진다.
  float grain(vec2 p) {
    float g = vnoise(p * vec2(0.42, 0.014));
    g += 0.5 * vnoise(p * vec2(1.7, 0.05));
    g += 0.25 * vnoise(p * vec2(6.0, 0.2));
    return g / 1.75;
  }

  void main() {
    // CSS와 같은 방향으로 센다 — 왼쪽 위가 원점이다.
    vec2 p = vec2(vUv.x, 1.0 - vUv.y) * uSize;

    float span = uCell + uGap;
    vec2 local = (p - uGap) / span;
    vec2 cell = clamp(floor(local), 0.0, ${BOARD_SIZE - 1}.0);
    vec2 inside = (local - cell) * span;

    // 칸 안인가, 칸 사이의 선인가. 판의 안쪽 여백도 선과 같은 색이다 —
    // 거기서 local 이 격자 밖으로 나가므로 clamp 뒤에 inside 가 칸을 벗어난다.
    float face = step(0.0, inside.x) * step(inside.x, uCell) * step(0.0, inside.y) * step(inside.y, uCell);

    vec3 wood = uBoard * (0.965 + grain(p) * 0.075);

    // 판이 빛을 받는다. 왼쪽 위가 조금 밝고 가장자리가 조금 죽는다 —
    // 이 한 겹이 없으면 셰이더로 그린 판이 색종이처럼 평평해 보인다.
    vec2 n = p / uSize;
    wood *= 1.0 + 0.045 * (1.0 - distance(n, vec2(0.28, 0.2)));
    wood *= 1.0 - 0.055 * smoothstep(0.55, 1.0, distance(n, vec2(0.5)));

    vec3 color = mix(uGrid, wood, face);

    // ── 그늘 ──────────────────────────────────────────────
    // 두 번 읽는다. **칸 값**은 「이 칸이 몇 매인가」라서 또렷해야 하고,
    // **번진 값**은 칸 사이를 메워 그늘이 판에 드리운 한 겹으로 보이게 한다.
    // 번진 것만 쓰면 어느 칸인지 흐려지고, 칸 값만 쓰면 81개의 상자가 된다.
    float sharp = texture2D(uField, (cell + 0.5) / ${BOARD_SIZE}.0).r;
    float spread = texture2D(uField, clamp(local / ${BOARD_SIZE}.0, 0.0, 1.0)).r;
    // 텍스처가 0~1로 돌려주므로 **매수로 되돌린다.** 깊이 곡선이 매수 위에 놓여 있어야
    // 이 줄만 고쳐서 세기를 조절할 수 있다.
    float net = (sharp * 0.72 + spread * 0.28) * 255.0;

    float reveal = 1.0 - smoothstep(uRevealRadius - 64.0, uRevealRadius, distance(p, uRevealCenter));
    float amount = uAmount * reveal;

    // 매수가 늘수록 깊어지되 포화한다. 3매와 4매의 차이가 1매와 2매의 차이만큼
    // 벌어지면, 판에서 제일 중요한 「닿는가 안 닿는가」가 깊은 쪽에 묻힌다.
    float depth = 1.0 - exp(-0.62 * net);
    color *= 1.0 - uDepth * depth * amount;

    gl_FragColor = vec4(color, 1.0);
  }
`;

/**
 * 판 표면 하나. 캔버스 하나를 잡고 산다.
 *
 * **부르지 않으면 아무것도 안 그린다.** 상시 rAF 루프를 두지 않는 것은 판이 대부분의
 * 시간 동안 가만히 있기 때문이고, 그 시간에 GPU를 돌리면 모바일에서 배터리만 먹는다.
 */
export class BoardSurface {
  private readonly renderer: WebGLRenderer;
  private readonly scene = new Scene();
  private readonly camera = new OrthographicCamera(-1, 1, 1, -1, 0, 1);
  private readonly material: ShaderMaterial;
  private readonly texture: DataTexture;
  private readonly data = new Uint8Array(BOARD_SIZE * BOARD_SIZE * 4);
  /** 셰이더에 넘기는 값들. `material.uniforms` 를 이름으로 뒤지면 오타가 런타임까지 간다. */
  private readonly uniforms: Uniforms;
  private disposed = false;

  constructor(canvas: HTMLCanvasElement, palette: Palette) {
    this.renderer = new WebGLRenderer({ canvas, antialias: false, alpha: false });
    // 모바일에서 3배로 그릴 이유가 없다(docs/03-frontend.md §1 구현 실무).
    this.renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));

    this.texture = new DataTexture(this.data, BOARD_SIZE, BOARD_SIZE, RGBAFormat);
    // **가장자리를 물고 늘어지게 둔다.** 반복이면 판 왼쪽 끝의 그늘이 오른쪽 끝에서
    // 번져 나와, 아무 일도 없는 칸에 상대의 利き이 있는 것처럼 보인다.
    this.texture.wrapS = ClampToEdgeWrapping;
    this.texture.wrapT = ClampToEdgeWrapping;
    this.texture.minFilter = LinearFilter;
    this.texture.magFilter = LinearFilter;
    this.texture.needsUpdate = true;

    this.uniforms = {
      uSize: { value: new Vector2(1, 1) },
      uCell: { value: 1 },
      uGap: { value: 1 },
      uBoard: { value: palette.board },
      uGrid: { value: palette.grid },
      uField: { value: this.texture },
      uAmount: { value: 0 },
      // 부르는 쪽이 정한다(`useBoardSurface`). 판이 회상 중에 낮아져 있느냐를 여기서는
      // 알 수 없고, 세기가 그 밝기에 매여 있다.
      uDepth: { value: 0 },
      uRevealCenter: { value: new Vector2(0, 0) },
      uRevealRadius: { value: 0 },
    };

    this.material = new ShaderMaterial({
      vertexShader: VERTEX,
      fragmentShader: FRAGMENT,
      depthTest: false,
      depthWrite: false,
      uniforms: this.uniforms,
    });

    this.scene.add(new Mesh(new PlaneGeometry(2, 2), this.material));
  }

  /** 판의 자가 바뀌었다. `--sq` 는 화면 폭을 따라 변하므로 그때마다 다시 받는다. */
  setLayout({ width, height, cell, gap }: Layout): void {
    this.renderer.setSize(width, height, false);
    this.uniforms.uSize.value.set(width, height);
    this.uniforms.uCell.value = cell;
    this.uniforms.uGap.value = gap;
  }

  /**
   * 칸마다의 그늘 깊이(매수). 81개를 그대로 받는다.
   *
   * 값을 0~255로 쓰지 않고 매수 그대로 넣는 것은 셰이더에서 **다시 매수로 읽기**
   * 위해서다 — 여기서 0~1로 눌러 두면 「몇 매인가」가 이 줄에서 사라져, 깊이 곡선을
   * 고치려면 두 파일을 같이 고쳐야 한다.
   */
  setField(depth: Uint8Array): void {
    for (let i = 0; i < BOARD_SIZE * BOARD_SIZE; i += 1) this.data[i * 4] = depth[i] ?? 0;
    this.texture.needsUpdate = true;
  }

  /** 그늘이 제일 깊을 때의 세기. `PLAIN_DEPTH` 와 `RECALL_DEPTH` 중 하나다. */
  setDepth(depth: number): void {
    this.uniforms.uDepth.value = depth;
  }

  /** 0이면 판만, 1이면 그늘까지. 그 사이는 밀려드는 중이다. */
  setAmount(amount: number, reveal: Reveal): void {
    this.uniforms.uAmount.value = amount;
    this.uniforms.uRevealCenter.value.set(reveal.x, reveal.y);
    this.uniforms.uRevealRadius.value = reveal.radius;
  }

  render(): void {
    if (this.disposed) return;
    this.renderer.render(this.scene, this.camera);
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    this.texture.dispose();
    this.material.dispose();
    // 컨텍스트를 안 놓으면 판이 사라졌다 다시 뜨는 것을 몇 번 반복한 뒤
    // 브라우저가 가장 오래된 WebGL 컨텍스트를 죽인다 — 그때 판이 검게 남는다.
    this.renderer.dispose();
    this.renderer.forceContextLoss();
  }
}

/**
 * `.board` 에서 색을 읽는다. **팔레트의 정본은 CSS 변수다** — 판의 나무색을 바꾸는 날
 * CSS만 고치면 셰이더가 따라온다.
 */
export function paletteOf(element: Element): Palette {
  const style = getComputedStyle(element);
  return {
    board: colorOf(style.getPropertyValue('--board'), 0.796, 0.671, 0.471),
    grid: colorOf(style.getPropertyValue('--grid'), 0.169, 0.125, 0.082),
  };
}

/** `#cbab78` 과 `203 171 120` 을 둘 다 받는다. 못 읽으면 넘겨받은 값으로 버틴다. */
function colorOf(raw: string, r: number, g: number, b: number): Vector3 {
  const text = raw.trim();

  const hex = /^#([0-9a-f]{6})$/i.exec(text)?.[1];
  if (hex) {
    const n = parseInt(hex, 16);
    return new Vector3(((n >> 16) & 255) / 255, ((n >> 8) & 255) / 255, (n & 255) / 255);
  }

  const [red, green, blue] = text.split(/[\s,]+/).map(Number);
  if (Number.isFinite(red) && Number.isFinite(green) && Number.isFinite(blue)) {
    return new Vector3(red! / 255, green! / 255, blue! / 255);
  }

  return new Vector3(r, g, b);
}
