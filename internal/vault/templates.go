package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Custom templates are plain `.md` files in the flat `.zennotes/templates/`
// directory. This layer is parse-free: it moves raw bytes, and the template
// package owns the frontmatter format. The filename rules (slug, dedupe,
// path validation) mirror the desktop and the server byte for byte, since a
// template's id is `custom:<filename stem>`.

const templatesRelDir = ".zennotes/templates"

// ErrInvalidTemplate marks a template path outside the templates directory.
var ErrInvalidTemplate = errors.New("invalid template request")

func templateDir(root string) string {
	return filepath.Join(root, ".zennotes", "templates")
}

func templateSourcePath(name string) string {
	return templatesRelDir + "/" + name
}

func templateFilenameStem(sourcePath string) string {
	name := sourcePath[strings.LastIndex(sourcePath, "/")+1:]
	if strings.EqualFold(filepath.Ext(name), ".md") {
		return name[:len(name)-len(".md")]
	}
	return name
}

// SafeTemplateSlug keeps lowercase letters, digits and dashes; every run of
// anything else becomes one dash, and leading and trailing dashes go.
func SafeTemplateSlug(slug string) string {
	var out strings.Builder
	inRun := false
	for _, r := range strings.ToLower(slug) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			if inRun {
				out.WriteByte('-')
				inRun = false
			}
			out.WriteRune(r)
			continue
		}
		inRun = true
	}
	if inRun {
		out.WriteByte('-')
	}
	cleaned := strings.Trim(out.String(), "-")
	if cleaned == "" {
		return "template"
	}
	return cleaned
}

func (v *Vault) resolveTemplatePath(sourcePath string) (string, error) {
	abs, err := SafeJoin(v.root, sourcePath)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(templateDir(v.root), abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || strings.Contains(rel, string(filepath.Separator)) {
		return "", fmt.Errorf("%w: refusing template path outside templates dir: %s", ErrInvalidTemplate, sourcePath)
	}
	if !strings.EqualFold(filepath.Ext(rel), ".md") {
		return "", fmt.Errorf("%w: template path must be a .md file: %s", ErrInvalidTemplate, sourcePath)
	}
	return abs, nil
}

func uniqueTemplateSlug(dir, base, previousSourcePath string) string {
	prevStem := ""
	if previousSourcePath != "" {
		prevStem = templateFilenameStem(previousSourcePath)
	}
	candidate := base
	for n := 2; ; n++ {
		if candidate == prevStem {
			return candidate
		}
		if _, err := os.Lstat(filepath.Join(dir, candidate+".md")); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, n)
	}
}

// ListTemplates returns every custom template with its raw bytes.
func (v *Vault) ListTemplates() ([]CustomTemplateFile, error) {
	entries, err := os.ReadDir(templateDir(v.root))
	if errors.Is(err, os.ErrNotExist) {
		return []CustomTemplateFile{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]CustomTemplateFile, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") || !strings.EqualFold(filepath.Ext(name), ".md") {
			continue
		}
		sourcePath := templateSourcePath(name)
		abs, err := v.resolveTemplatePath(sourcePath)
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		out = append(out, CustomTemplateFile{SourcePath: sourcePath, Raw: string(raw)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SourcePath < out[j].SourcePath })
	return out, nil
}

// ReadTemplate returns one template's raw bytes.
func (v *Vault) ReadTemplate(sourcePath string) (string, error) {
	abs, err := v.resolveTemplatePath(sourcePath)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// WriteTemplate saves a template under a slug derived from the request and
// removes the file it replaces when an edit changed the slug.
func (v *Vault) WriteTemplate(input WriteTemplateInput) (CustomTemplateFile, error) {
	var previous string
	if input.PreviousSourcePath != "" {
		abs, err := v.resolveTemplatePath(input.PreviousSourcePath)
		if err != nil {
			return CustomTemplateFile{}, err
		}
		previous = abs
	}
	dir := templateDir(v.root)
	slug := uniqueTemplateSlug(dir, SafeTemplateSlug(input.Slug), input.PreviousSourcePath)
	sourcePath := templateSourcePath(slug + ".md")
	abs, err := v.resolveTemplatePath(sourcePath)
	if err != nil {
		return CustomTemplateFile{}, err
	}
	if err := writeFileAtomic(abs, []byte(input.Raw), v.fileMode, v.dirMode); err != nil {
		return CustomTemplateFile{}, err
	}
	if previous != "" && previous != abs {
		sameFile := false
		if prevInfo, statErr := os.Stat(previous); statErr == nil {
			if newInfo, statErr := os.Stat(abs); statErr == nil && os.SameFile(prevInfo, newInfo) {
				sameFile = true
			}
		}
		if !sameFile {
			if err := os.Remove(previous); err != nil && !errors.Is(err, os.ErrNotExist) {
				return CustomTemplateFile{}, err
			}
		}
	}
	return CustomTemplateFile{SourcePath: sourcePath, Raw: input.Raw}, nil
}

// DeleteTemplate removes a template; one already gone is a success.
func (v *Vault) DeleteTemplate(sourcePath string) error {
	abs, err := v.resolveTemplatePath(sourcePath)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
