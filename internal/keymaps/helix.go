package keymaps

import "strings"

// The helix engine's own actions and the id aliases from earlier helix-branch
// builds live here, so the shared catalog above stays as upstream has it.

func init() {
	Catalog = append(Catalog,
		Entry{"helix.gotoTabPrevious", "helix", "g p", "Previous tab (gp)"},
		Entry{"helix.gotoTabNext", "helix", "g n", "Next tab (gn)"},
	)
}

// CanonicalID maps the helix.* ids that earlier helix-branch builds used for
// the shared modal actions back to their vim.* ids. Ids that only the helix
// engine has, such as helix.gotoTabNext, stay as they are.
func CanonicalID(id string) string {
	rest, ok := strings.CutPrefix(id, "helix.")
	if !ok {
		return id
	}
	for _, e := range Catalog {
		if e.ID == id {
			return id
		}
	}
	return "vim." + rest
}

// canonicalOverrides rewrites aliased ids in config.toml overrides; the
// vim.* id wins when both are set.
func canonicalOverrides(overrides map[string]string) map[string]string {
	out := make(map[string]string, len(overrides))
	for id, binding := range overrides {
		cid := CanonicalID(id)
		if _, both := overrides[cid]; cid != id && both {
			continue
		}
		out[cid] = binding
	}
	return out
}
