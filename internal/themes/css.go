package themes

import (
	"regexp"
	"strings"
)

// rule is one CSS block reduced to what a palette needs: its selectors and
// the --z-* custom properties it sets (keyed without the prefix).
type rule struct {
	selectors []string
	tokens    map[string]string
}

const tokenPrefix = "--z-"

// parseRules reads a stylesheet just far enough to find token declarations.
// Comments go first; at-rules (@font-face, @media, @import) and nested
// blocks are skipped, since a theme's tokens live in plain top-level rules.
func parseRules(css string) []rule {
	css = stripComments(css)
	var rules []rule
	i := 0
	for i < len(css) {
		start := i
		for i < len(css) && css[i] != '{' && css[i] != ';' {
			i = skipString(css, i)
		}
		if i >= len(css) {
			break
		}
		prelude := strings.TrimSpace(css[start:i])
		if css[i] == ';' {
			i++
			continue
		}
		end := matchBrace(css, i)
		body := css[i+1 : end]
		i = end + 1
		if prelude == "" || strings.HasPrefix(prelude, "@") {
			continue
		}
		tokens := parseTokens(body)
		if len(tokens) == 0 {
			continue
		}
		r := rule{tokens: tokens}
		for _, sel := range splitTopLevel(prelude, ',') {
			if sel = strings.Join(strings.Fields(sel), " "); sel != "" {
				r.selectors = append(r.selectors, sel)
			}
		}
		rules = append(rules, r)
	}
	return rules
}

func stripComments(css string) string {
	var b strings.Builder
	for {
		open := strings.Index(css, "/*")
		if open < 0 {
			b.WriteString(css)
			return b.String()
		}
		b.WriteString(css[:open])
		b.WriteByte(' ')
		end := strings.Index(css[open+2:], "*/")
		if end < 0 {
			return b.String()
		}
		css = css[open+2+end+2:]
	}
}

// skipString returns the index after the character at i, or after the whole
// quoted string that starts there.
func skipString(s string, i int) int {
	q := s[i]
	if q != '"' && q != '\'' {
		return i + 1
	}
	for i++; i < len(s); i++ {
		if s[i] == '\\' {
			i++
		} else if s[i] == q {
			return i + 1
		}
	}
	return len(s)
}

// matchBrace returns the index of the brace closing the one at open, or the
// end of the text when the block never closes.
func matchBrace(s string, open int) int {
	depth := 0
	for i := open; i < len(s); {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		case '"', '\'':
			i = skipString(s, i)
			continue
		}
		i++
	}
	return len(s) - 1
}

// splitTopLevel splits on sep outside parentheses, brackets, braces and
// strings, so `url(a;b)` and `[data-x="a,b"]` stay whole.
func splitTopLevel(s string, sep byte) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); {
		switch c := s[i]; {
		case c == '"' || c == '\'':
			i = skipString(s, i)
			continue
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
		case c == sep && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
		i++
	}
	return append(out, s[start:])
}

func parseTokens(body string) map[string]string {
	tokens := map[string]string{}
	for _, decl := range splitTopLevel(body, ';') {
		if strings.ContainsRune(decl, '{') {
			continue
		}
		name, value, ok := strings.Cut(decl, ":")
		name = strings.TrimSpace(name)
		if !ok || !strings.HasPrefix(name, tokenPrefix) {
			continue
		}
		value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "!important"))
		tokens[strings.TrimPrefix(name, tokenPrefix)] = value
	}
	return tokens
}

var (
	attrRe     = regexp.MustCompile(`\[[^\]]*\]`)
	modeAttrRe = regexp.MustCompile(`\[\s*data-theme-mode\s*=\s*["']?(light|dark)["']?\s*\]`)
)

// selectorMode says when a custom theme's selector applies: "" for both
// modes, "light" or "dark" when it is scoped with [data-theme-mode], and
// ok=false when it does not address the document root at all.
func selectorMode(sel string) (mode string, ok bool) {
	if strings.Contains(sel, ":not(") {
		return "", false
	}
	if m := modeAttrRe.FindStringSubmatch(sel); m != nil {
		mode = m[1]
	}
	switch strings.TrimSpace(attrRe.ReplaceAllString(sel, "")) {
	case "", ":root", "html", "html:root", "body":
		return mode, true
	}
	return "", false
}
