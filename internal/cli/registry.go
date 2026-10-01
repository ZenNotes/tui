package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The help catalog is also the command/flag catalog used by parsing and shell
// completion. Compatibility spellings that predate the help live here too.
type commandSpec struct {
	Name, Usage, Summary string
	Flags                map[string]bool // true means a value is required
}

func globalFlagSpecs() map[string]bool {
	return map[string]bool{"vault": true, "server": true, "token": true, "workspace-source": true,
		"json": false, "no-color": false, "no-input": false, "help": false, "version": false}
}

func commandSpecs() map[string]commandSpec {
	out := map[string]commandSpec{}
	for _, section := range helpSections {
		for _, row := range section.rows {
			parts := strings.Fields(row.name)
			name := parts[0]
			if len(parts) > 1 && !strings.ContainsAny(parts[1], "<[\"") {
				name += " " + parts[1]
			}
			flags := map[string]bool{}
			for _, field := range strings.Fields(row.flags) {
				if strings.HasPrefix(field, "--") {
					flag := strings.Trim(strings.TrimPrefix(field, "--"), ",")
					flags[flag] = !valuelessFlags[flag]
				}
			}
			// These long forms are accepted by the desktop's bundled CLI.
			if strings.Contains(row.name, "<path>") && !strings.HasPrefix(name, "comment ") && name != "open" || strings.HasPrefix(name, "folder ") && name != "folder list" || name == "backlinks" {
				flags["path"] = true
			}
			if strings.HasPrefix(name, "search") {
				flags["query"], flags["limit"] = true, true
			}
			if name == "task toggle" || name == "comment reply" || name == "comment resolve" {
				flags["id"] = true
			}
			if strings.HasPrefix(name, "comment ") && name != "comment list" {
				flags["body"] = true
			}
			if name == "tag find" {
				flags["tag"] = true
			}
			if name == "base create" {
				flags["title"] = true
			}
			if name == "use" {
				flags["name"], flags["no-default"] = true, false
			}
			out[name] = commandSpec{Name: name, Usage: row.name, Summary: row.description, Flags: flags}
		}
	}
	for alias, name := range map[string]string{"vault rm": "vault remove", "vault use": "use"} {
		spec := out[name]
		spec.Name, spec.Usage = alias, alias+" <name>"
		out[alias] = spec
	}
	out["version"] = commandSpec{Name: "version", Usage: "version", Summary: "Print the CLI version"}
	out["__complete"] = commandSpec{Name: "__complete", Usage: "__complete"}
	return out
}

func parseCommand(argv []string) (string, Args, error) {
	args := Args{Flags: map[string][]string{}}
	if len(argv) > 0 && argv[0] == "help" {
		args.push("help", "true")
		argv = argv[1:]
	}
	global := globalFlagSpecs()
	i := 0
	for i < len(argv) && strings.HasPrefix(argv[i], "-") {
		next, err := parseFlag(argv, i, global, &args)
		if err != nil {
			return "", args, err
		}
		i = next
	}
	if i == len(argv) {
		return "", args, nil
	}
	root := argv[i]
	i++
	specs := commandSpecs()
	allowed := globalFlagSpecs()
	found := false
	for name, spec := range specs {
		if name == root || strings.HasPrefix(name, root+" ") {
			found = true
			for flag, takesValue := range spec.Flags {
				allowed[flag] = takesValue
			}
		}
	}
	if !found {
		return root, args, fmt.Errorf("unknown command %q.%s Run `zn --help` for commands", root, suggestion(root, commandRoots()))
	}
	for i < len(argv) {
		if argv[i] == "--" {
			args.Positionals = append(args.Positionals, argv[i+1:]...)
			break
		}
		if strings.HasPrefix(argv[i], "--") || shortFlagRe.MatchString(argv[i]) {
			next, err := parseFlag(argv, i, allowed, &args)
			if err != nil {
				return root, args, err
			}
			i = next
		} else {
			args.Positionals = append(args.Positionals, argv[i])
			i++
		}
	}
	name := root
	if len(args.Positionals) > 0 {
		if _, ok := specs[root+" "+args.Positionals[0]]; ok {
			name += " " + args.Positionals[0]
			args.Positionals = args.Positionals[1:]
		}
	}
	spec, ok := specs[name]
	if !ok {
		if len(args.Positionals) == 0 || args.Bool("help") {
			args.push("help", "true")
			return name, args, nil
		}
		return name, args, fmt.Errorf("unknown %s command %q. Run `zn %s --help`", name, args.Positionals[0], name)
	}
	for flag := range args.Flags {
		if _, ok := global[flag]; ok {
			continue
		}
		if _, ok := spec.Flags[flag]; !ok {
			return name, args, fmt.Errorf("--%s is not a flag for `zn %s`. Run `zn %s --help`", flag, name, name)
		}
	}
	if !args.Bool("help") && !(args.Bool("version") && !spec.Flags["version"]) {
		if err := validatePositionals(spec, args); err != nil {
			return name, args, err
		}
	}
	return name, args, nil
}

func validatePositionals(spec commandSpec, args Args) error {
	// Search/capture join words and open accepts multiple files. Completion is
	// an internal transport for arbitrary shell words, including empty strings.
	switch spec.Name {
	case "search", "search-title", "capture", "open", "__complete":
		return nil
	}
	fields := strings.Fields(strings.TrimPrefix(spec.Usage, spec.Name))
	minimum, maximum := 0, len(fields)
	for _, field := range fields {
		if strings.Contains(field, "<") {
			minimum++
		}
	}
	switch spec.Name {
	case "create", "tui":
		minimum, maximum = 0, 1
	case "use", "vault use":
		minimum = 0
	}
	if spec.Flags["path"] && args.Str("path") != "" {
		minimum--
	}
	if spec.Name == "task toggle" && args.Str("id") != "" {
		minimum--
	}
	if spec.Name == "tag find" && args.Str("tag") != "" {
		minimum--
	}
	if spec.Name == "base create" && args.Str("title") != "" {
		minimum--
	}
	if strings.HasPrefix(spec.Name, "comment ") {
		if args.Str("id") != "" {
			minimum--
		}
		if _, ok := args.String("body"); ok {
			minimum--
		}
	}
	if len(args.Positionals) < minimum || len(args.Positionals) > maximum {
		return fmt.Errorf("usage: zn %s (see `zn %s --help`)", spec.Usage, spec.Name)
	}
	return nil
}

func parseFlag(argv []string, i int, allowed map[string]bool, args *Args) (int, error) {
	token := argv[i]
	name, value, assigned := strings.Cut(strings.TrimPrefix(token, "--"), "=")
	if !strings.HasPrefix(token, "--") {
		switch token {
		case "-h":
			name = "help"
		case "-n":
			name = "new-window"
		default:
			return i, fmt.Errorf("unknown flag %q", token)
		}
	}
	takesValue, ok := allowed[name]
	if !ok {
		choices := make([]string, 0, len(allowed))
		for flag := range allowed {
			choices = append(choices, flag)
		}
		return i, fmt.Errorf("unknown flag --%s.%s", name, suggestion(name, choices))
	}
	if takesValue && !assigned {
		if i+1 >= len(argv) || startsAFlag(argv[i+1]) {
			return i, fmt.Errorf("--%s needs a value", name)
		}
		i++
		value = argv[i]
	} else if !assigned {
		value = "true"
	}
	if !takesValue && value != "true" && value != "false" && value != "1" && value != "0" && value != "yes" && value != "no" {
		return i, fmt.Errorf("--%s expects true or false", name)
	}
	if name == "limit" {
		if _, err := strconv.Atoi(value); err != nil {
			return i, fmt.Errorf("--limit needs an integer, got %q", value)
		}
	}
	args.push(name, value)
	return i + 1, nil
}

func commandRoots() []string {
	seen := map[string]bool{"help": true}
	for name := range commandSpecs() {
		if strings.HasPrefix(name, "__") {
			continue
		}
		root, _, _ := strings.Cut(name, " ")
		seen[root] = true
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func requestedJSON(argv []string) bool {
	want := false
	for _, arg := range argv {
		if arg == "--" {
			break
		}
		if arg == "--json" || arg == "--json=true" || arg == "--json=1" || arg == "--json=yes" {
			want = true
		} else if arg == "--json=false" || arg == "--json=0" || arg == "--json=no" {
			want = false
		}
	}
	return want
}

func suggestion(input string, choices []string) string {
	if len(input) > 64 {
		return ""
	}
	sort.Strings(choices)
	best, distance := "", 3
	for _, choice := range choices {
		if d := editDistance(input, choice); d < distance {
			best, distance = choice, d
		}
	}
	if best == "" {
		return ""
	}
	return " Did you mean " + strconv.Quote(best) + "?"
}

// editDistance counts insertions, deletions, substitutions and swaps of
// adjacent characters, so `lsit` is one edit from `list` rather than two
// (which tied it with `init` and lost on alphabetical order).
func editDistance(a, b string) int {
	rows, cols := len(a)+1, len(b)+1
	d := make([][]int, rows)
	for i := range d {
		d[i] = make([]int, cols)
		d[i][0] = i
	}
	for j := 0; j < cols; j++ {
		d[0][j] = j
	}
	for i := 1; i < rows; i++ {
		for j := 1; j < cols; j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[rows-1][cols-1]
}
