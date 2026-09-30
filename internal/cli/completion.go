package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/config"
	"github.com/ZenNotes/tui/internal/server"
)

func cmdCompletion(args Args) error {
	scripts := map[string]string{
		"bash": `_zn_complete() {
	  local reply
	  COMPREPLY=()
	  while IFS= read -r reply; do COMPREPLY+=("$reply"); done < <(zn __complete -- "${COMP_WORDS[@]:1:COMP_CWORD}")
}
complete -o bashdefault -o default -F _zn_complete zn
`,
		"zsh": `#compdef zn
_zn() {
  local -a replies
  replies=("${(@f)$(zn __complete -- "${words[@]:1:$((CURRENT - 1))}")}")
  compadd -a replies
}
compdef _zn zn
`,
		"fish": `complete -c zn -f -a '(zn __complete -- (commandline -opc)[2..-1] (commandline -ct))'
`,
		"powershell": `Register-ArgumentCompleter -Native -CommandName zn -ScriptBlock {
  param($wordToComplete, $commandAst, $cursorPosition)
  $parts = @($commandAst.CommandElements | Select-Object -Skip 1 | ForEach-Object { $_.Extent.Text.Trim([char]39, [char]34) })
  if ($wordToComplete -eq '') { $parts += '' }
  zn __complete -- @parts | ForEach-Object {
    $quoted = "'" + $_.Replace("'", "''") + "'"
    [System.Management.Automation.CompletionResult]::new($quoted, $_, 'ParameterValue', $_)
  }
}
`,
	}
	script, ok := scripts[args.Positional(0)]
	if !ok {
		return fmt.Errorf("choose a shell: zn completion bash|zsh|fish|powershell")
	}
	fmt.Fprint(stdout, script)
	return nil
}

func completionCandidates(words []string) []string {
	prefix, before := "", []string{}
	if len(words) > 0 {
		prefix, before = words[len(words)-1], words[:len(words)-1]
	}
	candidates := []string{}
	previous := ""
	if len(before) > 0 {
		previous = before[len(before)-1]
	}
	switch previous {
	case "--server", "disconnect":
		for _, s := range config.LoadWorkspaces().Servers {
			candidates = append(candidates, s.Name)
		}
	case "--vault", "use":
		candidates = config.LoadWorkspaces().Names()
		for _, v := range config.KnownVaults() {
			candidates = append(candidates, v.Name)
		}
		if previous == "use" {
			candidates = append(candidates, "app")
		}
	case "--workspace-source":
		candidates = []string{"app", "terminal"}
	case "completion":
		candidates = []string{"bash", "zsh", "fish", "powershell"}
	default:
		name, args, _ := parseCommand(before)
		specs := commandSpecs()
		if strings.HasPrefix(prefix, "-") {
			flags := globalFlagSpecs()
			for flag, takesValue := range specs[name].Flags {
				flags[flag] = takesValue
			}
			for flag := range flags {
				candidates = append(candidates, "--"+flag)
			}
		} else if name == "" {
			candidates = commandRoots()
		} else {
			for key := range specs {
				if strings.HasPrefix(key, name+" ") {
					candidates = append(candidates, strings.TrimPrefix(key, name+" "))
				}
			}
			if strings.HasPrefix(name, "server ") && len(args.Positionals) == 0 {
				if instances, err := server.DefaultManager().List(); err == nil {
					for _, i := range instances {
						candidates = append(candidates, i.Name)
					}
				}
			} else if name == "config get" || name == "config set" {
				if len(args.Positionals) == 0 {
					for section, values := range config.ValueTables(config.DefaultPrefs()) {
						for key := range values {
							candidates = append(candidates, section+"."+key)
						}
					}
				}
			} else if _, hasPath := specs[name].Flags["path"]; hasPath || name == "tui" {
				// Completion never contacts a remote server. The shell can still
				// provide normal filesystem completion when no local vault exists.
				target, err := ResolveTargetFromArgs(args)
				if err == nil && target.Kind == backend.KindLocal {
					if b, err := OpenBackend(target); err == nil {
						notes, _ := b.ListNotes(context.Background())
						for _, n := range notes {
							candidates = append(candidates, n.Path)
						}
					}
				}
			}
		}
	}
	seen := map[string]bool{}
	var result []string
	for _, value := range candidates {
		if strings.HasPrefix(value, prefix) && !seen[value] && !strings.ContainsAny(value, "\r\n\t\x00") {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
