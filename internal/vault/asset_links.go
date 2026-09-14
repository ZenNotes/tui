package vault

import (
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var assetWikiRe = regexp.MustCompile(`!?\[\[([^\]\n]+)\]\]`)
var assetMarkdownRe = regexp.MustCompile(`!?\[[^\]\n]*\]\((<[^>\n]+>|[^)\s]+)(?:\s+"[^"\n]*")?\)`)

// RewriteAssetLinks preserves labels and only replaces destinations that resolve
// to the requested vault file. Fenced and inline code remain literal examples.
func RewriteAssetLinks(body, note, old, next string) (string, int) {
	count := 0
	lines := strings.Split(body, "\n")
	fence := ""
	rewrite := func(segment string) string {
		replace := func(re *regexp.Regexp, s string, wiki bool) string {
			return re.ReplaceAllStringFunc(s, func(full string) string {
				m := re.FindStringSubmatchIndex(full)
				raw := full[m[2]:m[3]]
				target := strings.Trim(raw, "<>")
				tail := ""
				delimiters := "#?"
				if wiki {
					delimiters = "|#"
				}
				if i := strings.IndexAny(target, delimiters); i >= 0 {
					tail = target[i:]
					target = target[:i]
				}
				decoded, err := url.PathUnescape(target)
				if err == nil {
					target = decoded
				}
				explicitRelative := strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../")
				absolute := strings.HasPrefix(target, "/")
				target = strings.TrimPrefix(target, "/")
				fromRoot := !explicitRelative && path.Clean(target) == old
				fromNote := !absolute && path.Clean(path.Join(path.Dir(note), target)) == old
				if !fromRoot && !fromNote {
					return full
				}
				count++
				value := next + tail
				if !wiki {
					if fromNote && !fromRoot {
						rel, err := filepath.Rel(filepath.FromSlash(path.Dir(note)), filepath.FromSlash(next))
						if err != nil {
							return full
						}
						value = filepath.ToSlash(rel) + tail
					} else if absolute {
						value = "/" + value
					}
					value = "<" + strings.NewReplacer("%", "%25", ">", "%3E", "<", "%3C").Replace(value) + ">"
				}
				return full[:m[2]] + value + full[m[3]:]
			})
		}
		segment = replace(assetWikiRe, segment, true)
		return replace(assetMarkdownRe, segment, false)
	}
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			count := 0
			for count < len(trim) && trim[count] == trim[0] {
				count++
			}
			mark := trim[:count]
			if fence == "" {
				fence = mark
			} else if mark[0] == fence[0] && len(mark) >= len(fence) && strings.TrimSpace(trim[count:]) == "" {
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		// Backtick-delimited spans are untouched; multiple-tick delimiters are
		// tracked as a unit, so literal ticks inside code do not end the span.
		var out strings.Builder
		for len(line) > 0 {
			j := strings.IndexByte(line, '`')
			if j < 0 {
				out.WriteString(rewrite(line))
				break
			}
			out.WriteString(rewrite(line[:j]))
			line = line[j:]
			n := 0
			for n < len(line) && line[n] == '`' {
				n++
			}
			delim := line[:n]
			end := strings.Index(line[n:], delim)
			if end < 0 {
				out.WriteString(line)
				break
			}
			end += 2 * n
			out.WriteString(line[:end])
			line = line[end:]
		}
		lines[i] = out.String()
	}
	return strings.Join(lines, "\n"), count
}
