# Themes

`zn tui` draws with the color scheme the desktop app is set to. Both apps read
the same `config.toml`, the same custom theme folders and the same color
tokens, so a theme picked in one shows up in the other. This page covers
choosing a theme, giving the terminal its own, writing a custom theme that
reads well in a terminal, and keeping the built-in list in step with the
desktop.

## Choosing a theme

### From the desktop app

Pick a theme in the desktop's Settings > Appearance and start `zn tui`. The
desktop saves the choice in `~/.config/zennotes/config.toml`
(`%APPDATA%\zennotes\config.toml` on Windows; `ZENNOTES_CONFIG_DIR` and
`XDG_CONFIG_HOME` move it) and zn reads it at startup:

```toml
[appearance]
theme_family = "catppuccin"      # the family
theme_mode = "dark"              # light | dark | auto
theme_id = "catppuccin-mocha"    # the variant within the family
```

Without the desktop app, write the same keys by hand; `zn config` opens the
file, creating it with comments when it is missing.

- `theme_mode = "auto"` follows the terminal background, where the desktop
  follows the operating system. zn also accepts `system` as a synonym, but the
  desktop does not, so prefer `auto` in a shared file.
- `theme_id` picks the variant. When it is missing, or disagrees with the
  family or the mode, zn uses the family's variant for the mode and keeps the
  flavor if it can: Gruvbox Hard stays Hard when the mode flips.
- `theme_family` alone is enough: `theme_family = "nord"` gives Nord in the
  current mode.

### For the terminal alone

To let the terminal differ from the desktop, name a theme under `[terminal]`.
The desktop ignores this table.

```toml
[terminal]
theme = "kanagawa-dragon"
```

The value is a family (`nord`), a variant id (`catppuccin-mocha`) or a custom
theme, by folder name (`soft-paper`) or id (`custom-soft-paper`). A family
follows `theme_mode`; a variant id brings its own mode. Left empty, zn matches
the desktop.

### Inside the app

These last for the session. zn never writes `config.toml`.

| Command | Effect |
| --- | --- |
| `:themes`, `:colorscheme`, `:colo` | A picker that previews the scheme under the cursor. Type to filter, Enter keeps it, Esc puts the old one back. |
| `:theme <family>` | The family, in the current mode: `:theme nord`. |
| `:theme <id>` | One variant, with its mode: `:theme github-dark-dimmed`. |
| `:theme <folder>` | A custom theme: `:theme soft-paper`. |
| `:theme dark` / `light` / `auto` | The mode, within the current family. |
| `:theme`, `Space z t` | Flip dark and light within the family. |

Tab completes every name after `:theme`. `:settings` shows the theme in effect
and the keys it came from.

### Built-in themes

| Family | Light variants | Dark variants | `auto` picks |
| --- | --- | --- | --- |
| `apple` | `apple-light` | `apple-dark` | `apple-light`, `apple-dark` |
| `gruvbox` | `light-hard`, `light-medium`, `light-soft` | `dark-hard`, `dark-medium`, `dark-soft` | `light-medium`, `dark-medium` |
| `catppuccin` | `catppuccin-latte` | `catppuccin-frappe`, `catppuccin-macchiato`, `catppuccin-mocha` | `catppuccin-latte`, `catppuccin-mocha` |
| `github` | `github-light`, `github-light-high-contrast` | `github-dark`, `github-dark-dimmed`, `github-dark-high-contrast` | `github-light`, `github-dark` |
| `solarized` | `solarized-light` | `solarized-dark` | `solarized-light`, `solarized-dark` |
| `one` | `one-light` | `one-dark` | `one-light`, `one-dark` |
| `nord` | `nord-light` | `nord-dark` | `nord-light`, `nord-dark` |
| `tokyo-night` | `tokyo-night-day` | `tokyo-night-storm` | `tokyo-night-day`, `tokyo-night-storm` |
| `kanagawa` | `kanagawa-lotus` | `kanagawa-wave`, `kanagawa-dragon`, `kanagawa-paper-ink` | `kanagawa-lotus`, `kanagawa-wave` |
| `black-metal` | `black-metal-day` | `black-metal` | `black-metal-day`, `black-metal` |
| `rose-pine` | `rose-pine-dawn` | `rose-pine-main`, `rose-pine-moon` | `rose-pine-dawn`, `rose-pine-main` |

The last column is what a family resolves to when no variant is saved. The
Gruvbox ids carry no family prefix because they are the desktop's original
theme; a fresh install starts on `dark-hard`.

### Quick tweaks

The desktop's Quick tweaks recolor the accent and the six syntax hues on top of
any theme. zn applies them too:

```toml
[tweaks]
"accent" = "#ff3b30"   # also: red, green, yellow, blue, purple, aqua
```

The desktop's density and corner presets live in the same table and mean
nothing in a terminal.

## Custom themes

A custom theme is a folder under `~/.config/zennotes/themes/`; the folder name
is its id. The desktop's [custom themes guide](https://zennotes.org/docs#custom-themes)
describes the format and the desktop seeds a complete example, `soft-paper/`.
zn reads the same files:

```
~/.config/zennotes/themes/my-theme/
  manifest.json   # optional: name, modes
  theme.css       # required
```

Select it with `theme_family = "custom"` and `theme_id = "custom-my-theme"`,
or `:theme my-theme`.

### What zn reads from theme.css

Only the `--z-*` color tokens, from rules that address the document root:

```css
:root {
  --z-bg: 238 230 221;          /* r g b, #rrggbb, #rgb or rgb(r, g, b) */
  --z-fg: 87 82 121;
  --z-accent: 26 125 164;
}
:root[data-theme-mode="dark"] {
  --z-bg: 48 52 70;
  --z-fg: 198 206 239;
  --z-accent: 140 170 238;
}
```

- `:root`, `html` and `body` rules apply to both modes. A rule scoped with
  `[data-theme-mode="dark"]` or `[data-theme-mode="light"]` applies to that
  mode and wins over an unscoped rule wherever it sits in the file.
- Everything else is skipped: fonts, `@font-face`, `@media` and other
  at-rules, selectors for components (`.cm-editor { … }`), `:not(…)`
  selectors, nested rules, `--z-glass-*`, `--z-shadow` and the layout tokens.
  A theme that restyles desktop components is still valid here; zn takes its
  colors and leaves the rest.
- `manifest.json` supplies the display `name` and `modes`: `"light"`,
  `"dark"`, `"both"` or a list. A single-mode theme keeps its mode whatever
  `theme_mode` says. A missing or broken manifest names the theme after its
  folder and assumes both modes.

### Which tokens the terminal uses

| Token | Painted as |
| --- | --- |
| `--z-bg` | The canvas: every cell's background. |
| `--z-fg` | Body text. |
| `--z-grey-2` | Dim text: quotes, inactive tabs, the focused pane's border, operators in code. |
| `--z-grey-1` | Muted text: borders, line numbers, frontmatter, hints, comments in code. |
| `--z-bg-2` | Selection: visual mode and selected rows. |
| `--z-bg-3` | The status bar and the cursor row of lists. |
| `--z-accent` | Headings, titles, the active tab, the mode badge, menu borders, key hints, keywords in code. A shade mixed 30% toward the canvas marks the focused selected row. |
| `--z-blue` | Links; functions in code. |
| `--z-aqua` | Tags. |
| `--z-green` | Inline code; strings. |
| `--z-yellow` | List markers, search hits, hint labels; numbers and constants. |
| `--z-purple` | Checkboxes and task metadata (`due:`, `!high`, `@waiting`); types and attributes. |
| `--z-red` | Errors and overdue dates; markup tags in code. |

`--z-bg-1`, `--z-bg-4`, `--z-bg-softer`, `--z-fg-1`, `--z-fg-2`, `--z-grey-0`,
`--z-grey-dim`, `--z-accent-soft` and `--z-accent-muted` are read but not
painted. The desktop's `--z-accent-soft` is a second hue (Apple pairs blue with
orange), which is why the terminal derives its softer accent from `--z-accent`
instead.

Fenced code uses these colors in the editor and in the preview when
`preview_style` is `auto`. Another Glamour style (`:style dracula`) brings its
own preview colors; the interface still follows the theme.

### Tokens a theme leaves out

A theme needs a background, a text color and an accent. The rest is derived the
way the desktop derives it for a small palette, where `mix(a, b, t)` moves
from `a` toward `b`:

| Missing | Becomes |
| --- | --- |
| `--z-fg` | `--z-fg-1` mixed 12% toward the muted color |
| `--z-grey-2` | `--z-fg-2`, else text mixed 30% toward the background |
| `--z-grey-0` | text mixed 52% toward the background |
| `--z-grey-1` | halfway between `--z-grey-2` and `--z-grey-0` |
| `--z-bg-1` | background mixed 5% toward the text |
| `--z-bg-3` | background mixed 16% toward the text |
| `--z-bg-2` | halfway between `--z-bg-1` and `--z-bg-3` |
| the six hues | the desktop's defaults for the mode (Gruvbox's) |

If the background, the text or the accent is missing too, Gruvbox's stands in
for the mode. Note that the desktop falls back differently: there a missing
token inherits Gruvbox Light's value even in dark mode, so a theme that looks
right in zn with few tokens may need more of them on the desktop.

### Contrast in a terminal

A palette tuned for a backlit window does not always survive thin monospace
text: several well-known themes use `--z-bg-3` as a border color that is
nearly the text color, and light themes often carry pastel hues at 2:1 on
white. zn moves a color only when it falls under a floor, only as far as
needed, and leaves the rest of the palette as written:

| Role | Floor against the canvas |
| --- | --- |
| Dim text (`--z-grey-2`) | 5:1 |
| Muted text (`--z-grey-1`) | 4.5:1 |
| The accent and the six hues | 3.5:1 |
| Text on an accent block (active tab, selected row) | 4:1, by taking the text or the background color, whichever reads, and shifting the block if it must |
| Text on `--z-bg-2` and `--z-bg-3` | 60% of the body text's contrast, at most 4.5:1; the surface moves toward the canvas |

Colors move toward `--z-fg`, so they keep their hue. The first three floors
assume body text at 7:1 and shrink in proportion when a theme's body text is
softer (Solarized is 4.1:1), so muted text stays below body text instead of
catching up with it.

To see exactly what you wrote, keep `--z-grey-1` at 4.5:1 or more, the hues
at 3.5:1, and `--z-bg-3` a surface color rather than a border color.

## Troubleshooting

**The theme did not change.** zn reads `config.toml` at startup; restart it
after changing the theme in the desktop. A `[terminal] theme` overrides
`[appearance]`. `:settings` shows both.

**A message says a custom theme has no readable theme.css, or names an unknown
theme.** zn falls back to a built-in theme (or, for a bad `[terminal] theme`,
to the `[appearance]` one) and says why in the footer. Check the folder name
under `themes/` against `theme_id` (`custom-` plus the folder name) and that
`theme.css` exists.

**`auto` picks the wrong mode.** zn asks the terminal for its background once,
at startup, and assumes dark when the terminal does not answer (some
multiplexers and remote sessions). It asks only when the mode is `auto` at
startup, so `:theme auto` in a session that started in a fixed mode also
assumes dark. Set `theme_mode` to `light` or `dark`, or use `:theme light`.

**Colors look banded or wrong.** The palettes are 24-bit. On a terminal
without truecolor they are approximated with 256 or 16 colors. Check that
`COLORTERM=truecolor` is set, and inside tmux enable RGB, for example
`set -as terminal-features ",xterm-256color:RGB"`. With `NO_COLOR` set, zn
draws without color.

## Keeping the built-in themes in step with the desktop

The colors are not written by hand. `internal/themes/builtin.css` holds one
flattened block per theme, generated from the desktop's stylesheet, and
`internal/themes/registry.go` mirrors the desktop's theme list. When the
desktop adds, removes or recolors a theme:

1. Update `Builtin` in `internal/themes/registry.go` from `THEMES` in the
   desktop's `packages/app-core/src/lib/themes.ts` (id, family, mode, variant),
   and `familyDefaults` from its `resolveAuto`.
2. Regenerate the colors from a checkout of `ZenNotes/zennotes`:

   ```sh
   go run ./internal/themes/gen /path/to/zennotes/packages/app-core/src/styles/index.css
   ```

   The generator resolves the desktop's cascade (the bare `:root` holds
   Gruvbox Light, the Gruvbox darks share a block, each variant overrides a
   few tokens) and refuses a theme that lacks a block or a token, so a
   half-applied change cannot slip in. Themes that exist only in the
   stylesheet and not in `THEMES` (the GitHub colorblind variants) are left
   out on purpose: the desktop cannot select them either.
3. Run `go test ./internal/themes ./internal/tui`. The tests check that every
   registry theme has colors that agree with its mode, and that each one
   clears the contrast floors above once mapped to the terminal.
4. Update the table on this page and the family list in `:help` and in
   `DefaultConfigTOML`.

How tokens map to terminal roles lives in `themeFromPalette` in
`internal/tui/theme.go`; the custom theme reader in `internal/themes/custom.go`
and `css.go`.
