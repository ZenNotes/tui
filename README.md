# zennotescli

`zn`, the ZenNotes command line, rewritten in Go. One static binary that does
two things:

1. Everything the desktop app's bundled `zn` does: notes, search, folders,
   tags, tasks, vaults, databases, capture, `open`, and the MCP server, with
   the same commands, flags, JSON shapes and task ids.
2. `zn tui`: ZenNotes itself, in the terminal. Sidebar, tabs, splits, a
   Markdown preview, Tasks (list, board, calendar), Tags, Quick Notes,
   Archive, Trash, databases, templates, periodic notes, a command palette,
   and the full Vim engine.

Notes stay plain `.md` files on disk. The CLI reads the same
`~/.config/zennotes/config.toml` and `.zennotes/vault.json` as the app, so a
keymap override or a remapped Inbox folder means the same thing in both.

## Build

```
go build -o zn .
go test ./...
```

Go 1.24 or newer. The binary has no runtime dependencies; the clipboard uses
`pbcopy`, `xclip`/`xsel` or the Windows clipboard when present.

## Getting started

Three commands cover every way to get a vault in front of zn; each one
remembers its result and makes it the default, so the next `zn` or
`zn tui` needs no flags:

```
zn init ~/Notes                      # create a vault (settings file + a welcome note)
zn vault add ~/Documents/notes       # use a folder of Markdown you already have
zn connect https://notes.example.com # a self-hosted ZenNotes server; asks for the token once
zn tui                               # open it
```

`zn setup` asks those three questions interactively, and `zn tui` offers
the same wizard when nothing is configured yet. `zn use <name>` switches
the default between saved vaults and servers (`zn use app` follows the
ZenNotes desktop app's vault again), `zn vault list` shows everything with
the default starred, and `zn disconnect <name>` forgets a server.

The list lives in `~/.config/zennotes/workspaces.toml`; server tokens go
to `credentials.toml` next to it, readable only by you. Scripts and CI can
skip the store with `ZENNOTES_REMOTE_TOKEN` and `--server <url>`.

Inside the TUI, `:vault` (or `Space v`, as in the desktop app) lists every
saved vault and server, with entries to add a folder or connect to a
server; `:server` connects (the token prompt is masked and the token is
stored), and `:local` returns to the default local vault. Buffers are
saved before a switch, each vault keeps its own tabs and sidebar state, and
the vault you switch to becomes the default for the next launch.

At launch, the resolution order is:

1. `--server <name|url>` (plus `--token`, `ZENNOTES_REMOTE_TOKEN`, or the
   stored token) talks to a self-hosted ZenNotes server.
2. `--vault <name|path>` picks a saved vault, a known one, or a directory.
   Notes can live under an `inbox` folder (the classic layout) or straight
   at the vault root; the layout is detected from the files, and
   `zn vault mode root` or `zn vault mode inbox` moves a vault between the
   two (favorites are rewritten, nothing is overwritten).
3. `ZENNOTES_VAULT` or `ZENNOTES_SERVER` in the environment.
4. zn's own default (`zn use`, `zn connect`, `zn init`, or a switch in the TUI).
5. The workspace the desktop app is currently on (its `zennotes.config.json`).

`ZENNOTES_CONFIG_DIR` relocates every config file the CLI touches, which is
what tests and automation use. `ZENNOTES_APP_PATH` points `zn open` at a
ZenNotes app bundle when it is not in the usual place.

## Commands

```
zn list | read | write | create | append | prepend
zn rename | move | archive | unarchive | trash | restore | duplicate | delete
zn search <query> | search-title <q> | backlinks <path>
zn folder list | create | rename | delete
zn tag list | find
zn task list | toggle
zn setup | init [folder] | connect <url> | disconnect | use <name|app>
zn vault info | list | mode [root|inbox] | add <folder> | remove <name>
zn base list | create | get | rows | add | set   (.base folders and loose .csv files)
zn read <path> --pretty          render a note in the terminal (--style dark|dracula|…|file.json)
zn config                        open config.toml in $EDITOR (created with comments when missing)
zn capture "..."                 quick note from an argument or stdin
zn open <path>                   hand a note or folder to the desktop app
zn mcp                           MCP stdio server for Claude, Codex and friends
zn tui [note]                    the terminal UI
```

Every command takes `--json`; `zn --help` lists the flags of each one. The
command names, flags, JSON shapes and task ids match the desktop's bundled
`zn`, so scripts written for one work with the other.

## The terminal UI

```
zn tui                              # current workspace
zn tui --vault ~/notes              # a directory
zn tui --server work --token …      # a saved server profile
zn tui "Project Plan.md"      # open a note straight away
```

Vim mode follows `[vim] enabled` in config.toml. With it on, the whole app is
modal; with it off the editor is a plain editor and lists answer only to
arrows, Enter and Escape (modifier chords keep working).

### Getting around

On macOS, Option only reaches the app as Alt when the terminal is set up
for it (Ghostty: `macos-option-as-alt = true`; iTerm2: "Esc+"; Terminal:
"Use Option as Meta"). Every `Alt` binding below has a leader or `Ctrl+W`
equivalent, and preview mode always returns to the editor with `Esc`.

| Keys | What |
| --- | --- |
| `Alt+H` `Alt+J` `Alt+K` `Alt+L` | Focus sidebar, panes and side panel |
| `Ctrl+B` | Toggle the sidebar |
| `Alt+.` | Zen mode |
| `Alt+1` … `Alt+9` | Jump to a tab |
| `Alt+E` `Alt+S` `Alt+P` | Editor, split, preview mode for the tab |
| `Alt+Z` | Word wrap |
| `Ctrl+P` | Search notes (Enter creates the note when nothing matches, `Ctrl+D` trashes the selected one) |
| `Ctrl+N` | New note in the current folder |
| `Ctrl+S` | Save now (notes autosave a moment after you stop typing) |
| `Ctrl+O` `Ctrl+I` `Alt+Left` `Alt+Right` | Jump list, across notes |
| `Ctrl+W` then `v` `s` `h` `j` `k` `l` `q` `o` `w` `=` `<` `>` | Panes |
| `:` | Ex commands (see below); `Space ;` or `Ctrl+G` opens the command palette |

### Leader (`Space`)

`o` open buffers, `f` search notes, `s t` vault text, `e` sidebar, `p`
outline, `c` calendar, `v` switch vault, `k` Kanban board, `z p/s/e/v` view modes (preview, split, editor,
toggle; also `Alt+P`, `Alt+S`, `Alt+E`), `v w/n/z/t` wrap, line numbers, zen,
theme, `l f/y/s/r/m/d/a/c/o` note actions (format, copy,
favorite, rename, move, trash, archive, copy link, open in the desktop app),
`q` quick capture, `t` new from template, `i` insert template, `d`/`w`/`m`
daily, weekly, monthly note, `h` hint mode, `x` tasks, `n` new note, `;`
command palette, `?` manual. Which-key hints appear after the configured
delay.

### Embeds, diagrams and code

- **Pictures**: `![[photo.png]]` and `![alt](path.jpg)` paint inline in
  Kitty and Ghostty through the Kitty graphics protocol (Unicode
  placeholders, so scrolling, splits and tmux with `allow-passthrough on`
  keep working); other terminals get a card. `[terminal] images = auto |
  kitty | off` in config.toml.
- **Files**: PDF, audio, video, drawings and other attachments render as a
  card; Enter opens them with the system handler on a local vault and
  copies the path on a server. YouTube and Vimeo links on their own line
  become cards too.
- **Transclusion**: `![[Note]]` renders the note inline, three levels deep.
- **Mermaid**: ```` ```mermaid ```` blocks draw as text through a pure-Go
  renderer (flowchart, sequence, state, class, ER); with `mmdc` on PATH and
  pictures on, they render as images in the background.
- **Math**: `$$` blocks render as pictures through `typst` when it is on
  PATH and pictures are on, else as a quiet source block.
- **Code**: fenced blocks are syntax-highlighted in the editor as well as
  the preview (chroma, the preview's gruvbox theme).

### Tabs, panes and Home

- Every pane is a framed box with its tab strip above it: the active tab is
  a filled block, the others sit dimmed beside it, and `+` opens a new
  note. The frame's top edge names what the pane shows (`editor`,
  `preview`, `database`, `tasks`) and turns accent-colored on the focused
  pane; the outline, links and calendar panels get the same frame. `:zen`
  drops all chrome.
- Tabs and split panes: `gt`/`gT`, `]b`/`[b`, `Alt+1` to `Alt+9`, the
  `Ctrl+W` family for splits, a closed-tab stack (`:tabreopen`).
- Pinned tabs: `:pin` toggles; pinned tabs sit first in the strip, survive
  `:tabonly`, and refuse to close until unpinned. `:tabmove -1|+1|first|last`
  reorders. The strip scrolls to keep the active tab visible when it
  overflows. Right click a tab for the same actions.
- Home is a dashboard: the day's numbers, today's note and its tasks with
  overdue ones first, recent notes, a week strip with task counts and
  daily-note dots, favorites and tags. `j`/`k` and Enter move through it,
  `x` toggles a task, `h`/`l` pick a day on the week strip.
- The status bar shows a colored mode pill, the note's folder, cursor
  position, progress and word count; an empty pane shows a welcome card
  with the main shortcuts and recent notes.
- Typing `[[` in insert mode opens the note picker and inserts `[[Title]]`
  (`:link` from normal mode). With `rolloverUnfinishedTasks` on in
  `vault.json`, a new daily note pulls the open tasks of the previous one.

### Editor

The engine is a full Vim: counts, registers, marks, macros, dot-repeat,
undo/redo, text objects (`iw aw is ap i( a" it il …`), visual, visual line
and visual block (with `I`/`A`), `/` `?` `n` `N` `*` `#`, `:s` `:g` `:sort`
`:norm` with ranges, `Ctrl+A`/`Ctrl+X`, `gq`, and heading motions `]]` `[[`.
Markdown extras: `Ctrl+L` toggles the checkbox on the line, `gd` follows the
link under the cursor (missing notes are created on confirm), `gx` opens a
URL, `gy` copies a link, `zc zo zM zR` fold headings, Enter continues lists
and checkboxes, Tab/Shift+Tab indent list items, `jk` leaves insert mode.

### Ex commands

Tab on the `:` line cycles through matching commands, then through the
arguments a command understands (note titles for `:e`, folders for
`:move`, styles, themes, templates, views, keymap ids, tags, databases),
Shift+Tab goes back, and Up/Down recall history, in the editor and in the
prompt that the sidebar and views open.


`:w :q :wq :qa :wa :bd :bn :bp :e :new :vs :sp :only :move :rename :delete
:archive :restore :duplicate :favorite :copylink :format :tasks :tags :trash
:quick :archived :home :help :settings :template :insert :daily :weekly
:monthly :newtask :filter :outline :connections :calendar :sidebar :zen
:editmode :splitmode :previewmode :fold :unfold :foldall :unfoldall :db
:dbnew :bind :unbind :keymaps :commands :reload :wrap :nu :theme :vim
:vault :server :local :rollover :copypath :reveal :forward :inprogress
:cancel :taskfile :tabcloseright :tabmenu :unarchive :purge :assets :ref
:notesort :donestyle :whichkey :tabs :quickdate :template save :website
:version`. `:help <topic>` filters the built-in manual.

### Views

- **Tasks**: three layouts of the same view. Open one directly with
  `:tasks list|kanban|calendar`, `:kanban`, `:taskcalendar`, `Space x`,
  `Space k`, or the `list`, `kanban` and `calendar` rows under Tasks in the
  sidebar; inside the view `v` cycles list, board and calendar. `x` toggles, Enter opens the
  note at the line, `p d w c /` set priority, due date, waiting, cancelled,
  in progress, `e s < >` edit text, set a field, shift the due date, `J K`
  reorder inside the note, `n` adds a task to the daily note, `f` filters
  (`#tag`, `note:x`, `due:today|overdue|none`, `priority:high`, `status:x`,
  `is:open|done|waiting`, `-term`), `m` opens the task menu (forward to
  today's daily note lives there). On the board `H`/`L` move a card and `g`
  cycles the grouping (status, priority, due, any `@field`). On the calendar
  `h j k l [ ] t` move, Enter lists the day's tasks, `o` opens the daily note.
- **Tags**: `Tab` multi-selects, `a` toggles any/all, `c` clears, `r`/`x`
  rename or remove a tag everywhere (code stays untouched).
- **Quick Notes / Archive / Trash**: `n` new quick note, `u` unarchive, `r`
  restore, `x` delete forever, `E` empty the trash.
- **Databases**: every `.base` folder and every loose `.csv` file. Typed
  cells (text, number, checkbox, date, select, multi-select, note links)
  with pickers, `[[` note linking inside text cells, row selection and bulk
  delete, duplicate, a row menu, record pages (`o`, folders only;
  `:dbconvert` turns a loose file into a folder), a header row for fields
  (rename, retype, move, hide, delete, add), saved filters and sorts,
  multiple table and board views, board cards moved with `H`/`L`, columns
  added with `a`, option colors, and a raw CSV toggle.
- **Home**, **Help**, **Settings** (read-only view of the effective prefs).
- Side panels: Outline (`Space p`), Connections with backlinks and unresolved
  links (`:connections`), Calendar with daily-note dots (`Space c`).

### Themes and the reading view

The reading view (preview and split mode) renders through Glamour, the
same renderer Glow uses. `preview_style` under `[terminal]` in
`config.toml` picks a style: `auto` (the `zennotes` style, derived from the
interface palette in its dark or light variant), Glamour's `dark`, `light`,
`dracula`, `tokyo-night`, `pink`, `ascii` and `notty`, a path to your own
Glamour JSON style sheet, or `zen` for the built-in renderer. Code blocks are syntax highlighted by the style's chroma
theme. `:style <name>` switches for the session and `:style` alone opens a
picker.

The rest of the interface follows `theme_mode` under `[appearance]`:
`dark`, `light`, or `system`, which reads the terminal's background color
at startup. `:theme` toggles it.

### Editing elsewhere, mouse, help

- `Space l e` (or `:editor`) opens the note in `$VISUAL` or `$EDITOR` at
  the cursor line and reloads it when the editor exits; remote notes go
  through a temporary file. `Ctrl+Z` suspends to the shell.
- The mouse works everywhere and is on by default: click to focus and
  select, double click to open a note or toggle a task, right click for the
  same menus `m` opens, wheel to scroll, drag in the editor to select
  (Vim visual mode), drag the sidebar edge or a split divider to resize,
  click tabs (middle click closes one), Ctrl+click a link to follow it,
  and click rows in palettes and menus. `mouse = false` under
  `[terminal]` or `:mouse` turns it off, which hands text selection back
  to the terminal (most terminals also select natively with Shift or
  Option held).
- `?` in any list or view shows the keys for that surface, resolved through
  your `[keymaps]` overrides; the help line under the status bar is built
  the same way. `:help` is the full manual.

### What stays in the desktop app

Workflows, Atlas, sharing, cloud sync, Harper grammar checks, image resizing
and rendered math or diagrams. `Space l o` (or `:desktop`) hands the current
note to the app through its `zennotes://` link.

## Demo recording

`docs/demo/` holds a small demo vault, a config, the keystroke choreography
(`demo-drive.sh`) and `record-demo.sh`, which runs the TUI in tmux, records
the terminal stream with asciinema, renders it with agg (Gruvbox colors, SF
Mono) and encodes mp4 files with ffmpeg. No screen capture is involved, so
it runs unattended. Output: `docs/media/zn-tui-demo.cast`,
`zn-tui-demo.mp4`, a 1280px version and a poster frame.

```
brew install tmux asciinema agg ffmpeg
docs/demo/record-demo.sh
```

`record-kanban.sh` records the board on its own: `kanban-vault/` seeds
tasks across five status columns (`@status:review` and `@status:blocked`
join To Do, In Progress and Done through `kanban_statuses` in
`kanban-config/config.toml`), and `kanban-drive.sh` moves cards between
columns, regroups by priority, due date and `@owner`, filters by tag, adds
a card and finishes one. Output: `docs/media/zn-tui-kanban.mp4`, a 1280px
version, a poster frame and the `.cast`.

## Layout of the code

```
main.go                  entry point
internal/cli             argument parsing, every zn command, help text
internal/vault           vault semantics: layout, parsing, tasks, edits, templates
internal/backend         local and remote backends behind one interface
internal/remote          HTTP client for the ZenNotes server
internal/database        databases: .base folders (CSV + schema.json) and loose .csv files (sidecar .csv.base.json)
internal/periodic        daily, weekly and monthly note patterns
internal/templates       built-in and custom templates
internal/search          note search scoring
internal/config          config.toml, zennotes.config.json, directories
internal/keymaps         the bindable-action catalog and overrides
internal/mcp             the MCP stdio server (official go-sdk)
internal/vim             the Vim engine, host-agnostic
internal/tui             the terminal UI (Bubble Tea)
```
