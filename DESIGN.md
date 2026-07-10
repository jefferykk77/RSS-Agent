# RSS Agent Design System

## Product Intent

RSS Agent is a personal intelligence workbench, not another feed reader and not a marketing dashboard. The primary job is to help one person finish a daily triage loop quickly:

1. See the few items worth attention.
2. Understand why each item was selected.
3. Read the supporting context without losing the queue.
4. Give feedback that improves the next digest.

The interface must feel calm, precise, and dense enough for repeated use. It should make ranking, source provenance, delivery state, and user preference legible without competing for attention.

## Reference Blend

Use these systems for behavior and hierarchy, never as brand skins:

- **Linear**: hairline separation, purposeful density, restrained use of the primary accent.
- **Superhuman**: a focused triage loop with fast feedback actions and keyboard-first movement.
- **Raycast**: command palette behavior, visible keyboard hints, compact utility controls.
- **Cohere research pages**: rule-separated editorial lists, filterable taxonomies, quiet data tables.
- **Notion**: readable long-form detail views with uncomplicated content hierarchy.

## Visual Direction

### Theme

Default to a light, cool-neutral operational surface that supports long-form reading. Provide a system-aware dark mode only after the core light interface is complete. Do not use gradients, decorative blobs, or glass effects.

### Colors

| Token | Value | Use |
|---|---|---|
| `canvas` | `#F7F9FC` | Application background |
| `surface` | `#FFFFFF` | Primary pane and dialog surface |
| `surface-muted` | `#EEF2F6` | Selected rows, neutral controls, empty states |
| `surface-hover` | `#E7EDF5` | Hovered rows and icon buttons |
| `ink` | `#1D2733` | Primary text |
| `ink-muted` | `#64748B` | Metadata and secondary labels |
| `hairline` | `#D9E1EA` | Pane boundaries and row dividers |
| `accent` | `#3F5CC7` | Primary action, selection, focus ring, links |
| `success` | `#118A6A` | Delivered, saved, healthy |
| `warning` | `#B7791F` | Attention and deferred states |
| `danger` | `#C53B3B` | Blocked, failed delivery, destructive feedback |
| `tag-ai` | `#7252B8` | AI and model taxonomy only |
| `tag-product` | `#137C8B` | Product taxonomy only |

Accent colors communicate meaning. Never use a color merely to decorate a panel.

### Typography

- UI and body: `Inter`, `Noto Sans SC`, `PingFang SC`, `Microsoft YaHei`, `system-ui`, sans-serif.
- Technical metadata: `JetBrains Mono`, `Cascadia Mono`, monospace.
- Letter spacing is always `0`.
- Use 14px for compact UI, 16px for reading UI, 18px for article leads, and 24px-28px for in-pane headings. Reserve larger type for empty states only.
- Use weight and contrast sparingly: 400 body, 500 controls, 600 titles. Avoid 700+ except a small number of page-level headings.

### Geometry and Depth

- 8px spacing system: `4, 8, 12, 16, 24, 32`.
- 6px radius for buttons and compact controls; 8px for dialogs and article media. Avoid large pill shapes except status badges.
- Elevation comes from adjacent surfaces and hairlines, not drop shadows. Dialogs may use one subtle shadow.
- Main page sections are pane layouts with borders, not floating cards. Do not nest cards inside cards.

## Application Shell

### Desktop

Use a fixed-height three-pane workbench below a 48px global bar:

1. **Navigation pane**: 232px. Profile switcher, digest views, source groups, saved items, and run history.
2. **Digest pane**: `minmax(380px, 0.9fr)`. Search, compact filter bar, digest controls, and a virtualizable ranked list.
3. **Detail pane**: `minmax(480px, 1.3fr)`. Selected article, explanation, source context, delivery history, and feedback actions.

Panes are separated with `hairline` borders. The digest list and article detail scroll independently. Do not place a hero, KPI-card grid, or landing-page content above this workbench.

### Mobile and Tablet

- Below 1024px, preserve navigation as a dismissible drawer and use a two-pane layout.
- Below 768px, show the digest list as the primary screen; open article detail as a full-screen route or sheet.
- Keep feedback actions in a fixed bottom action bar on narrow screens.
- Keep icon targets at least 40px high and never let toolbar labels wrap into unreadable controls.

## Key Views

### Today

The default view. Its header contains the selected profile, latest run timestamp, article count, and one compact `Run now` command. Show the ranked digest as rule-separated rows, not card tiles.

Each row includes:

- Score, source favicon/initial, title, source, age, and tags.
- One-line summary and one-line “why this was selected” explanation.
- Delivery and feedback state icons.
- Hover-only icon actions: save, defer, block. Include tooltips.

The selected row uses `surface-muted`, a 3px `accent` inset marker, and no oversized card treatment.

### Article Detail

Detail begins with a title, source link, publication time, score, and a compact explanation block. The explanation is always visible before the article body:

- `Why selected`
- `Key points`
- `Tags`
- `Model and cache state`

The full article body is typography-first, wide enough to read but constrained to a comfortable line length. Delivery history and feedback are secondary sections separated by rules, not widgets competing with the article.

### Sources and Profiles

Use table/list views for source management. Each source row shows health, last fetch, unread candidates, ETag/cache state, and a compact overflow menu. Profile configuration uses clear form sections with an inline preview of the active interests and exclusions.

### Run History

Use a rule-separated run log with counts, cost, model used, duration, and delivery outcomes. Failure details expand inline. Avoid charts until historical behavior genuinely needs comparison.

## Interaction Rules

- Primary actions are icon plus text only when a clear command needs a label: `Run now`, `Add source`, `Save`.
- Secondary actions use Lucide icons with accessible labels and hover tooltips.
- Feedback is direct and reversible: like, dislike, save, later, block item, block source.
- Always show the effect of feedback with a concise status change and an undo affordance.
- Use a command palette for global actions, source search, profile switching, and keyboard shortcut discovery.
- Make list navigation keyboard-first: arrows move selection, Enter opens detail, `s` saves, `l` marks later, `b` blocks, `/` focuses search.
- A disabled or unavailable channel must show its concrete reason, never a generic “something went wrong.”

## Required States

- First run with no articles.
- Fetching, analyzing, pushing, and partial delivery failure.
- No matches after filters.
- Loading article body and full-text extraction failure.
- Cached analysis and fallback model usage.
- Empty source list, unhealthy source, and muted source.
- Saved, later, blocked, and already delivered feedback states.

## Do and Don't

### Do

- Favor rows, columns, and clear boundaries over decorative cards.
- Show provenance and explanation alongside score.
- Keep the active queue visible while reading an article.
- Use compact monospaced labels for model, cache, cost, and runtime diagnostics.
- Support both mouse workflows and efficient keyboard use.

### Don't

- Do not turn this into a generic analytics dashboard with decorative charts.
- Do not use a single-hue palette, purple gradients, dark-blue surfaces, or beige canvases as the dominant theme.
- Do not use large rounded pills for ordinary controls.
- Do not hide important actions only in an overflow menu.
- Do not make the LLM chat surface the primary interaction model; the product is a digest workbench.
- Do not use unreadable low-contrast metadata or make users infer why an item was selected.
