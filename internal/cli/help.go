package cli

import (
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

// Version is stamped at build time (`-ldflags "-X .../cli.Version=..."`).
var Version = "0.1.0"

const (
	terminalColumnsFallback = 80
	terminalColumnsCap      = 100
	commandColumnWidth      = 26
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

type helpRow struct {
	name        string
	description string
	flags       string
}

type helpSection struct {
	heading string
	rows    []helpRow
}

var helpSections = []helpSection{
	{"NOTES", []helpRow{
		{"list", "List notes, most recent first", "--folder <f>  --tag <t>  --limit <n>  --json"},
		{"read <path>", "Print a note body to stdout", "--pretty  --style <name|json>  --meta  --json"},
		{"create", "Create a new note. Body from --body or stdin", "--title <t>  --folder inbox|quick|archive  --subpath <p>  --tag <t>  --body \"...\"|-"},
		{"write <path>", "Replace a note body. Destructive: prefer append", "--body \"...\"|-"},
		{"append <path>", "Append text to the end of a note", "--body \"...\"|-"},
		{"prepend <path>", "Insert text at the top, after frontmatter", "--body \"...\"|-"},
		{"rename <path>", "Rename a note (filename only)", "--to <new title>"},
		{"move <path>", "Move a note to a different folder", "--folder <f>  --subpath <p>"},
		{"archive <path>", "Move a note into archive/", ""},
		{"unarchive <path>", "Move it back from archive/", ""},
		{"trash <path>", "Soft-delete; reversible via restore", ""},
		{"restore <path>", "Restore a trashed note to inbox", ""},
		{"delete <path>", "Permanent delete", "--yes"},
		{"duplicate <path>", "Copy a note next to itself", ""},
	}},
	{"SEARCH", []helpRow{
		{"search <query>", "Full-text search across live notes", "--limit <n>  --json"},
		{"search-title <q>", "Match notes by title (substring)", "--json"},
		{"backlinks <path>", "Notes linking to this one via [[wikilink]]", "--json"},
	}},
	{"FOLDERS", []helpRow{
		{"folder list", "List every subfolder in the vault", "--json"},
		{"folder create <p>", "Create a subfolder, e.g. inbox/Work", ""},
		{"folder rename <p>", "Rename a subfolder in place", "--to <newPath>"},
		{"folder delete <p>", "Delete a subfolder and everything in it", "--yes"},
	}},
	{"TAGS", []helpRow{
		{"tag list", "Every #tag with its note count", "--json"},
		{"tag find <tag>", "Notes carrying this #tag", "--limit <n>  --json"},
	}},
	{"TASKS", []helpRow{
		{"task list", "Open checkbox tasks across all notes", "--unchecked  --all  --tag <t>  --include-excluded  --json"},
		{"task toggle <id>", "Flip a task checkbox by stable id", ""},
	}},
	{"COMMENTS", []helpRow{
		{"comment list <path>", "Comment threads on a note, with anchors and replies", "--all  --json"},
		{"comment add <path> \"<body>\"", "Start a thread, optionally anchored to text from the note", "--anchor <text>  --author <name>  --json"},
		{"comment reply <path> <id> \"<body>\"", "Answer in a thread", "--author <name>  --json"},
		{"comment resolve <path> <id>", "Resolve a thread (or reopen it)", "--reopen  --json"},
	}},
	{"VAULT", []helpRow{
		{"setup", "Guided first run: create a vault, use a folder, or connect to a server", ""},
		{"init [folder]", "Create a vault (default ~/Notes), remember it, make it the default", "--name <n>  --no-default  --json"},
		{"connect <url|name>", "Save a ZenNotes server and its token (asked for if not given), verify, make it the default", "--name <n>  --token <t>  --no-default  --json"},
		{"disconnect [name]", "Forget a saved server and its token", "--json"},
		{"use <name|folder|url|app>", "Make a saved vault or server the default; `app` follows the ZenNotes app again", "--json"},
		{"vault add <folder>", "Remember an existing folder as a vault", "--name <n>  --no-default  --json"},
		{"vault remove <name>", "Forget a saved vault or server (files stay)", ""},
		{"vault info", "Vault path (or server) + per-folder counts", "--json"},
		{"vault list", "Known vaults and servers; the default is marked with *", "--json"},
		{"vault mode [root|inbox]", "Show or switch the notes layout (moves the notes, rewrites favorites)", "--json"},
	}},
	{"DATABASES", []helpRow{
		{"base list", "Every database in the vault", "--json"},
		{"base create <title>", "Create an empty database", "--folder <f/sub>  --json"},
		{"base rows <base>", "List a database’s rows (title or path picks the base)", "--json"},
		{"base get <base> <row>", "One row by id or title value", "--json"},
		{"base add <base>", "Add a row; --body (or --body - for stdin) also creates its record page", "--set Field=Value …  --body <text|->  --page  --json"},
		{"base set <base> <row>", "Set fields; select options mint, the page re-mirrors", "--set Field=Value …  --json"},
		{"base convert <base>", "Turn a loose .csv into a .base folder so rows can have record pages", "--json"},
	}},
	{"CAPTURE", []helpRow{
		{"capture \"...\"", "Quick add. Pipes stdin if no positional", "--folder <f>  --tag <t>  --title <t>  --json"},
	}},
	{"OPEN", []helpRow{
		{"open <path>", "Open markdown files, or a folder / vault (a focused session), in the app", ""},
	}},
	{"TUI", []helpRow{
		{"tui", "Open ZenNotes in the terminal: sidebar, tabs, splits, Vim motions, tasks, tags", "--vault <v>  --server <s>  [path]"},
	}},
	{"MCP", []helpRow{
		{"mcp", "Start the MCP stdio server (Claude / Codex)", ""},
		{"config", "Open config.toml in $EDITOR, creating a commented starter file", "--path  --no-edit"},
	}},
}

var globalFlags = []helpRow{
	{"--vault <name|path>", "Target a specific vault or saved server (see `zn vault list`)", ""},
	{"--server <name|url>", "Target a ZenNotes server; wins over --vault", ""},
	{"--token <token>", "Auth token for --server (overrides the saved one)", ""},
	{"--json", "Emit machine-readable JSON output", ""},
	{"--no-color", "Disable ANSI color even on a TTY", ""},
	{"--help, -h", "Show this help", ""},
	{"--version", "Print the CLI version", ""},
}

var environmentRows = []helpRow{
	{"ZENNOTES_VAULT", "Default vault root when --vault is not given", ""},
	{"ZENNOTES_REMOTE_TOKEN", "Server token for CI and scripts; `zn connect` stores one for you otherwise", ""},
	{"ZENNOTES_SERVER", "Default server when neither --vault nor --server is given (otherwise zn follows the vault the app has open)", ""},
	{"ZENNOTES_CONFIG_DIR", "Override the ZenNotes config directory", ""},
	{"ZENNOTES_APP_PATH", "Path to the ZenNotes desktop app, for `zn open` when it is not installed in the usual place", ""},
	{"NO_COLOR", "Disable ANSI color (industry standard)", ""},
}

var examples = []string{
	`zn capture "Meeting takeaways" --tag work`,
	`pbpaste | zn append "inbox/Daily.md" --body -`,
	`zn search "deadline" --json | jq '.[].path'`,
	`zn list --tag idea --limit 5`,
	`zn list --vault work --limit 5`,
	`zn list --server home                # a self-hosted ZenNotes server`,
	`zn capture "from CI" --server https://notes.example.com`,
	`zn task list --unchecked --tag work`,
	`zn tui                               # the whole app, in the terminal`,
	`zn open ~/Downloads/notes.md`,
	`zn open ~/code/project/docs   # focus a folder as a session`,
}

type helpStyle struct {
	color bool
}

func colorEnabled(argv []string) bool {
	if force := os.Getenv("FORCE_COLOR"); force != "" && force != "0" {
		return true
	}
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	for _, a := range argv {
		if a == "--no-color" {
			return false
		}
	}
	return term.IsTerminal(int(os.Stdout.Fd()))
}

func (s helpStyle) wrap(code, value string) string {
	if !s.color || value == "" {
		return value
	}
	return code + value + "\x1b[0m"
}

func (s helpStyle) bold(v string) string    { return s.wrap("\x1b[1m", v) }
func (s helpStyle) dim(v string) string     { return s.wrap("\x1b[2m", v) }
func (s helpStyle) italic(v string) string  { return s.wrap("\x1b[3m", v) }
func (s helpStyle) cyan(v string) string    { return s.wrap("\x1b[36m", v) }
func (s helpStyle) yellow(v string) string  { return s.wrap("\x1b[33m", v) }
func (s helpStyle) magenta(v string) string { return s.wrap("\x1b[35m", v) }

func termWidth() int {
	cols := terminalColumnsFallback
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		cols = w
	}
	return min(max(cols, 60), terminalColumnsCap)
}

func visibleLen(value string) int {
	return utf8.RuneCountInString(ansiRe.ReplaceAllString(value, ""))
}

func padVisible(value string, width int) string {
	n := visibleLen(value)
	if n >= width {
		return value
	}
	return value + strings.Repeat(" ", width-n)
}

func wrapLines(text string, width int) []string {
	if utf8.RuneCountInString(text) <= width {
		return []string{text}
	}
	lines := []string{}
	current := ""
	for _, word := range strings.Fields(text) {
		if current == "" {
			current = word
			continue
		}
		if utf8.RuneCountInString(current+" "+word) > width {
			lines = append(lines, current)
			current = word
		} else {
			current += " " + word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func (s helpStyle) header(width int) []string {
	inner := width - 4
	tagline := "ZenNotes CLI · capture, search, edit your vault from any terminal"
	titleLine := s.bold(s.cyan("zn")) + " " + s.dim("v"+Version)
	lines := []string{titleLine}
	for _, l := range wrapLines(tagline, inner) {
		lines = append(lines, s.dim(l))
	}
	out := []string{"╭" + strings.Repeat("─", width-2) + "╮"}
	for _, l := range lines {
		out = append(out, "│ "+padVisible(l, inner)+" │")
	}
	return append(out, "╰"+strings.Repeat("─", width-2)+"╯")
}

func (s helpStyle) section(heading string, rows []helpRow, width int) []string {
	out := []string{s.bold(s.yellow(heading))}
	descWidth := width - commandColumnWidth - 2
	for _, row := range rows {
		descLines := wrapLines(row.description, descWidth)
		out = append(out, "  "+padVisible(s.magenta(row.name), commandColumnWidth)+descLines[0])
		for _, cont := range descLines[1:] {
			out = append(out, "  "+strings.Repeat(" ", commandColumnWidth)+cont)
		}
		if row.flags != "" {
			for _, line := range wrapLines(row.flags, descWidth) {
				out = append(out, "  "+strings.Repeat(" ", commandColumnWidth)+s.dim(s.cyan(line)))
			}
		}
	}
	return out
}

// RenderHelp is the pretty `zn --help`, colors gated on TTY detection and
// the NO_COLOR / FORCE_COLOR conventions.
func RenderHelp(argv []string) string {
	s := helpStyle{color: colorEnabled(argv)}
	width := termWidth()
	out := s.header(width)
	out = append(out, "", s.bold(s.yellow("USAGE")))
	out = append(out, "  "+s.cyan("zn")+" "+s.magenta("<command>")+" "+s.dim("[arguments] [flags]"), "")
	for _, sec := range helpSections {
		out = append(out, s.section(sec.heading, sec.rows, width)...)
		out = append(out, "")
	}
	out = append(out, s.section("GLOBAL FLAGS", globalFlags, width)...)
	out = append(out, "")
	out = append(out, s.section("ENVIRONMENT", environmentRows, width)...)
	out = append(out, "", s.bold(s.yellow("EXAMPLES")))
	for _, line := range examples {
		out = append(out, "  "+s.dim("$")+" "+line)
	}
	out = append(out, "")
	out = append(out, s.italic(s.dim("  Quote note paths that contain spaces. `zn tui` opens the whole app in this terminal.")))
	out = append(out, s.italic(s.dim("  Run `zn <command>` to try one out.")))
	out = append(out, "")
	return strings.Join(out, "\n")
}

// RenderVersion is `zn --version`.
func RenderVersion(argv []string) string {
	s := helpStyle{color: colorEnabled(argv)}
	return s.cyan(s.bold("zn")) + " " + s.dim("v"+Version) + "\n"
}
