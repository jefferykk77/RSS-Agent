# RSS Agent Design System

## Product Intent

RSS Agent is a personal intelligence reader. Its primary loop is: scan the ranked queue, understand why an item matters, read the source without losing context, and provide feedback that improves later recommendations.

The interface is an operational product, not a marketing page. It should feel warm and editorial while remaining compact enough for repeated daily use.

## Visual Direction

The approved direction adapts the Claude warm editorial language without copying Anthropic branding:

- Warm off-white reading canvas with dark warm ink.
- Coral reserved for the primary command, focus, links and current selection.
- Serif display type for article and section headings; humanist sans for controls; monospace for technical metadata.
- Dark surfaces reserved for Agent Briefs, code blocks and the library drawer.
- Depth comes from surface contrast and hairlines, not decorative shadows.
- Letter spacing is always `0`.

## Tokens

| Token | Value | Use |
|---|---:|---|
| `canvas` | `#FAF9F5` | Main reading floor |
| `surface` | `#FFFEFB` | Queue and dialog surfaces |
| `surface-soft` | `#F4EFE7` | Selected rows and quiet controls |
| `surface-strong` | `#E9E1D5` | Score badges and active filters |
| `ink` | `#171715` | Headings and primary text |
| `body` | `#3D3B37` | Long-form body text |
| `muted` | `#716D65` | Metadata and secondary labels |
| `hairline` | `#E2DBD1` | Pane and row separation |
| `coral` | `#C7654A` | Primary action and selection |
| `coral-active` | `#A94F37` | Active action and text link |
| `dark` | `#1C1B19` | Agent Brief, code and drawer |
| `dark-elevated` | `#292724` | Controls on dark surfaces |
| `success` | `#438B68` | Healthy and completed state |
| `warning` | `#B8832D` | Limited or deferred state |
| `error` | `#B84A43` | Failed and destructive state |

Typography:

- Display: `Iowan Old Style`, `Palatino Linotype`, `Georgia`, serif.
- UI: `Inter`, `Noto Sans SC`, `PingFang SC`, `Microsoft YaHei`, system sans.
- Technical: `JetBrains Mono`, `Cascadia Mono`, `Consolas`, monospace.
- Long-form body is 18px with 1.8 line height and an 820px maximum measure.

## Application Shell

Desktop uses a 58px global bar and two panes:

1. Queue: 350-410px, independently scrollable, containing search, filters and ranked rows.
2. Reader: remaining width, independently scrollable, with content centered at 820px.

Profile, collections, sources, health and Digest history live in a 328px overlay library drawer. They do not consume permanent reading width.

At narrow widths, the queue becomes the primary screen and the reader follows in document order. No control may create horizontal page overflow.

## Core Components

- **Article row:** neutral score, title, two-line summary, source, age and recommendation text. Selection uses a coral 3px marker and a warm surface.
- **Agent Brief:** dark full-width section containing reason, conclusion, model recommendation, key points and tags.
- **Reader body:** sanitized semantic rich text preserving headings, lists, quotes, links, images, tables and code.
- **Feedback:** standard icon controls for like, dislike, save and later, followed by explicit preference chips.
- **Library drawer:** dark navigation surface with readable source health states and Digest history.
- **Dialogs:** native modal behavior with visible labels, focus handling and one clear primary action.

## Interaction Rules

- Scores are always neutral. Recommendation is expressed by membership in the Recommended view and optional text, never score color.
- All icon-only controls require an accessible name and tooltip where meaning is not obvious.
- Keyboard navigation supports `Ctrl/Cmd+K`, `/`, arrow selection, save and later shortcuts.
- Async commands expose pending, completion and failure states without shifting layout.
- Motion is limited to 150-300ms and must respect `prefers-reduced-motion`.
- Color is never the only status signal.

## Validation

- Vitest covers data and sanitization behavior.
- Playwright verifies the real Go API at desktop, compact and narrow viewports.
- Production screenshots are checked at 1440x900 and 1920x1080.
- `npm run build`, `go test ./...` and `go build ./cmd/rss-agent` must all pass before delivery.
