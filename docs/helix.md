# Helix keys in zn

zn's editor speaks Vim by default. Set `motions = "helix"` under `[vim]` in
`config.toml` (or in Settings) and notes you open from then on use a
Helix-style engine instead: select first, then act on the selection.

```toml
[vim]
enabled = true     # modal editing; false = plain editor, for either engine
motions = "helix"  # "vim" (default) or "helix"
```

The rest of `[vim]` (`insert_escape`, `yank_to_clipboard`, the leader hints,
`wrapped_line_motions`) applies to both engines. A `[helix]` section written
by earlier builds of this branch still works when `[vim]` is absent.

## What the engine does

- Motions select as they travel: `w` selects a word and the space after it,
  so `wd` deletes a word; `b` selects the word it lands on.
- `x` selects the line (again to extend), `%` selects the note, `;` collapses
  the selection, `v` enters select mode, where motions extend it.
- `d` `c` `y` act on the selection; `p` `P` paste, `R` replaces it with the
  register.
- `n` `N` select the next match; `*` uses the selection as the pattern.
- `mi` `ma` select inside or around a text object (`miw`, `ma"`); `mm` jumps
  to the matching bracket.
- `Alt+O` (`Alt+Up`) grows the selection by context: word, paragraph,
  function in a code block, code block, heading section, note. `Alt+I`
  (`Alt+Down`) shrinks it back.
- Goto mode `g`: `gg` `ge` `gh` `gl` `gs` `gd`; view mode `z`: `zz` `zt` `zb`,
  and the folds `zf` `zo` `za` `zM` `zR`.
- `Q` records a macro, `q` replays it; `u` `U` undo and redo.
- `gn` `gp` switch tabs, next to the shared `gt` `gT` `]b` `[b`.

The app keys around the editor (the leader, `Ctrl+W` panes, the tab
sequences) are the same in both engines and are rebound under `[keymaps]`.

## Rebinding the engine's keys

The `[keys]` tables use the format of Helix's own `config.toml`, so you can
copy your `[keys.normal]`, `[keys.select]` and `[keys.insert]` tables across:

```toml
[keys.normal]
C-s = ":w"                                          # a typable command
"A-x" = ["extend_line_below", "delete_selection"]   # several commands
D = "no_op"                                         # unbind a key

[keys.normal.g]                                     # a key sequence: g a
a = "goto_last_line"

[keys.insert.j]                                     # j k leaves insert mode
k = "normal_mode"
```

- Keys use Helix's notation: `a`, `A`, `%`, `C-s`, `A-x`, `S-tab`, and the
  names `ret`, `esc`, `space`, `tab`, `backtab`, `backspace`, `del`, `left`,
  `right`, `up`, `down`, `home`, `end`, `pageup`, `pagedown`, `minus`.
- A key you do not list keeps the engine's behaviour, and a sequence that
  matches no binding reaches the engine as typed, so `[keys.normal.g]` adds to
  goto mode instead of replacing it.
- A count works with a bound key (`3D`), and keys an engine command is
  waiting for (the character after `f` or `r`) are never rebound.
- `[keys]` applies to the helix engine only; the vim engine ignores it.
- A binding zn cannot read (a command it does not know, a bad key) is
  reported when a note opens, and the rest of the table still applies.

The supported command names are listed in `helixCommands` in
[`internal/helix/keymap.go`](../internal/helix/keymap.go): the movement,
goto, view, selection and editing commands this engine implements, plus
`goto_next_buffer` / `goto_previous_buffer` (tabs) and `no_op`. A Helix
command zn does not have yet (multiple cursors, the tree-sitter motions,
the pickers) is reported rather than ignored.

## How it is built

The branch keeps upstream's files as they are, so merging `main` stays easy:

- `internal/vim` is upstream's untouched. `internal/helix` is a separate
  engine that does not import it.
- `internal/tui/engine.go` defines the editor interface the app drives, in
  vim's types, and the per-engine profile (tab keys, fold keys, word
  selection). `internal/tui/engine_helix.go` adapts the helix engine to it.
  Helix's select mode reports as vim's visual modes, so the app treats both
  selections the same way.
- Upstream files change only by one-line hooks: the buffer's editor type,
  the constructors, `blockCursor`, `modeLabel`, the engine's tab sequences,
  its key help, and `applyHelixPrefs` / `addHelixValues` / `addHelixEnums`
  in the config.
- `internal/tui/engine_contract_test.go` runs the app's expectations of an
  engine (insert and cursor shapes, `r`, Ctrl+C, `:w`, tabs, word selection)
  against both engines.
