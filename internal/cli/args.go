// Package cli is the `zn` command surface: the same commands, flags, output
// and JSON shapes as the CLI the desktop app bundles, plus `zn tui`.
package cli

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Args is a parsed command line: positionals plus repeated flags.
//
//	zn <command> [<subcommand>] [positional...] [--flag value | --flag=value | -x value]
//
// Repeated flags collect in order. Boolean flags are inferred when no value
// follows or when the next token starts with `--`.
type Args struct {
	Positionals []string
	Flags       map[string][]string
}

var shortFlagRe = regexp.MustCompile(`^-[A-Za-z][\w-]*$`)

// Parse mirrors the desktop CLI's parser, including its one subtlety: only
// a dash followed by a letter is a short flag, so `zn capture "- [ ] task"`
// keeps its task line positional.
func Parse(argv []string) Args {
	args := Args{Flags: map[string][]string{}}
	for i := 0; i < len(argv); i++ {
		token := argv[i]
		if token == "--" {
			args.Positionals = append(args.Positionals, argv[i+1:]...)
			break
		}
		if strings.HasPrefix(token, "--") {
			if eq := strings.IndexByte(token, '='); eq >= 0 {
				args.push(token[2:eq], token[eq+1:])
				continue
			}
			name := token[2:]
			if i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "--") {
				args.push(name, argv[i+1])
				i++
			} else {
				args.push(name, "true")
			}
			continue
		}
		if shortFlagRe.MatchString(token) {
			args.push(token[1:], "true")
			continue
		}
		args.Positionals = append(args.Positionals, token)
	}
	return args
}

func (a *Args) push(name, value string) {
	a.Flags[name] = append(a.Flags[name], value)
}

// String is the last value given for a flag, and whether it was given.
func (a Args) String(name string) (string, bool) {
	values := a.Flags[name]
	if len(values) == 0 {
		return "", false
	}
	return values[len(values)-1], true
}

// Str is the last value or "".
func (a Args) Str(name string) string {
	v, _ := a.String(name)
	return v
}

// Bool reads a boolean flag.
func (a Args) Bool(name string) bool {
	v, ok := a.String(name)
	if !ok {
		return false
	}
	return v == "true" || v == "1" || v == "yes"
}

// Int reads a numeric flag; ok is false when absent or not a number.
func (a Args) Int(name string) (int, bool) {
	v, present := a.String(name)
	if !present {
		return 0, false
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, false
	}
	return int(f), true
}

// IntOr reads a numeric flag with a default.
func (a Args) IntOr(name string, fallback int) int {
	if v, ok := a.Int(name); ok {
		return v
	}
	return fallback
}

// Many lists every value given for a flag.
func (a Args) Many(name string) []string {
	return a.Flags[name]
}

// Positional is the nth positional or "".
func (a Args) Positional(n int) string {
	if n < len(a.Positionals) {
		return a.Positionals[n]
	}
	return ""
}

// stdinIsTerminal is true when nothing is piped in.
func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return true
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// ReadStdin drains stdin; empty when it is a terminal.
func ReadStdin() string {
	if stdinIsTerminal() {
		return ""
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return ""
	}
	return string(bytes.ToValidUTF8(data, []byte("�")))
}

// ResolveBody applies the body conventions: `--body "literal"`, `--body -`
// (stdin), a positional fallback, then piped stdin; nil when nothing.
func (a Args) ResolveBody(fallbackPositional string) *string {
	if flag, ok := a.String("body"); ok {
		if flag == "-" {
			s := ReadStdin()
			return &s
		}
		return &flag
	}
	if fallbackPositional != "" {
		return &fallbackPositional
	}
	if !stdinIsTerminal() {
		piped := ReadStdin()
		if strings.TrimSpace(piped) != "" {
			return &piped
		}
	}
	return nil
}
