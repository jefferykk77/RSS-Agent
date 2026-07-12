# RSS Agent Claude Reader Tokens

This prototype adapts `D:\design\awesome-design-md\design-md\claude\DESIGN.md` for a dense desktop intelligence reader. It uses no Anthropic logo or proprietary font.

## Color

| Token | Value | Role |
|---|---:|---|
| `canvas` | `#FAF9F5` | Warm reading floor |
| `surface` | `#FFFEFB` | Queue and dialog surfaces |
| `surface-soft` | `#F4EFE7` | Selected rows and search controls |
| `surface-strong` | `#E9E1D5` | Neutral score badges and active filters |
| `ink` | `#171715` | Headlines and primary text |
| `body` | `#3D3B37` | Long-form reading text |
| `muted` | `#716D65` | Metadata |
| `hairline` | `#E2DBD1` | Pane and row separators |
| `coral` | `#C7654A` | Primary action and selection marker |
| `dark` | `#1C1B19` | Agent brief, code and library drawer |
| `success` | `#438B68` | Healthy and complete |
| `warning` | `#B8832D` | Limited source |

## Typography

- Display: Iowan Old Style / Palatino / Georgia, weight 500.
- UI: Inter / Noto Sans SC / system sans, weight 400-600.
- Technical: JetBrains Mono / Cascadia Mono / Consolas.
- Letter spacing is always `0`.
- Article measure: maximum 820px; body 18px at 1.8 line height.

## Layout

- Top bar: 58px.
- Queue: 350-410px.
- Reader: remaining viewport, content centered at 820px.
- Library: 328px overlay drawer, not a permanent third pane.
- Queue and reader scroll independently.

## Interaction

- Coral indicates the single primary action and current selection only.
- Scores remain neutral regardless of recommendation state.
- All icon controls have labels or accessible names.
- Focus uses a visible coral ring.
- Motion is limited to 180-300ms state transitions and respects reduced motion.
