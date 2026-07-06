// Package skillscan resolves the agent-runtime's OBSERVED skill set from disk
// (issue-4a45e9cc). It walks the four claude-code skill LAYERS (built-in / plugin /
// user / project), reads each skill's SKILL.md YAML frontmatter (name + description),
// and computes the SHADOWED flag from layer precedence — a skill NAME present in a
// higher-precedence layer shadows the same name in every lower layer.
//
// It is filesystem-only and injectable (LayerRoots), so the daemon resolves the real
// roots best-effort while tests drive temp dirs. It knows NOTHING about the center wire
// or the agent BC — the caller maps Skill → the report payload. The precedence here
// MUST match agent.SkillLayer precedence on the center (built-in < plugin < user <
// project) so the shadow flag the runtime computes and the center's defensive recompute
// agree.
package skillscan

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Layer is one of the four claude-code skill-resolution precedence layers. The string
// values are the wire form the center stores (agent.SkillLayer).
type Layer string

const (
	LayerBuiltin Layer = "built-in"
	LayerPlugin  Layer = "plugin"
	LayerUser    Layer = "user"
	LayerProject Layer = "project"
)

// layerRank is the precedence, low→high (more local wins).
var layerRank = map[Layer]int{LayerBuiltin: 0, LayerPlugin: 1, LayerUser: 2, LayerProject: 3}

// orderedLayers is the scan order (also the tie order for same-name-same-layer dups).
var orderedLayers = []Layer{LayerBuiltin, LayerPlugin, LayerUser, LayerProject}

// Skill is one OBSERVED skill resolved on disk.
type Skill struct {
	Layer       Layer
	Name        string
	Description string
	Shadowed    bool
}

// LayerRoots maps each layer to the directory root(s) to scan. Each root is a
// skills-container dir whose immediate children are skill dirs holding a SKILL.md
// (e.g. ~/.claude/skills, <project>/.claude/skills, and — for plugins — each
// ~/.claude/plugins/<plugin>/skills). A layer may have zero, one, or many roots; a
// non-existent root is silently skipped so a missing layer just contributes nothing.
type LayerRoots struct {
	Builtin []string
	Plugin  []string
	User    []string
	Project []string
}

func (lr LayerRoots) rootsFor(l Layer) []string {
	switch l {
	case LayerBuiltin:
		return lr.Builtin
	case LayerPlugin:
		return lr.Plugin
	case LayerUser:
		return lr.User
	case LayerProject:
		return lr.Project
	}
	return nil
}

// Scan walks every root of every layer, reads each skill's SKILL.md frontmatter, and
// returns the resolved set with the shadowed flag computed from layer precedence. The
// result is sorted by layer rank (built-in→project) then name for a stable order (so
// the fingerprint is deterministic). It never errors: an unreadable root/skill is
// skipped so a partial filesystem still yields the skills it can read.
func Scan(roots LayerRoots) []Skill {
	var out []Skill
	// Track the highest rank seen per skill name to decide shadowing.
	bestRank := map[string]int{}
	for _, layer := range orderedLayers {
		rank := layerRank[layer]
		for _, root := range roots.rootsFor(layer) {
			for _, sk := range scanRoot(layer, root) {
				out = append(out, sk)
				key := strings.ToLower(sk.Name)
				if r, ok := bestRank[key]; !ok || rank > r {
					bestRank[key] = rank
				}
			}
		}
	}
	// A copy is shadowed unless it sits in the highest-ranked layer that defines its
	// name. Same-name-same-layer dups: the first-seen (scan order) stays effective.
	winnerTaken := map[string]bool{}
	for i := range out {
		key := strings.ToLower(out[i].Name)
		if layerRank[out[i].Layer] < bestRank[key] {
			out[i].Shadowed = true
			continue
		}
		if winnerTaken[key] {
			out[i].Shadowed = true
			continue
		}
		winnerTaken[key] = true
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := layerRank[out[i].Layer], layerRank[out[j].Layer]; ri != rj {
			return ri < rj
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// scanRoot reads root/<skill>/SKILL.md for each immediate child dir. A child without a
// readable SKILL.md is skipped. The name falls back to the dir name when the
// frontmatter omits it.
func scanRoot(layer Layer, root string) []Skill {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil // missing/unreadable root → nothing
	}
	var out []Skill
	for _, e := range entries {
		if !entryIsDir(root, e) {
			continue
		}
		mdPath := filepath.Join(root, e.Name(), "SKILL.md")
		name, desc, ok := readSkillMD(mdPath)
		if !ok {
			continue
		}
		if name == "" {
			name = e.Name()
		}
		out = append(out, Skill{Layer: layer, Name: name, Description: desc})
	}
	return out
}

// entryIsDir reports whether a skills-root child is a directory, FOLLOWING symlinks
// (F1). The real ~/.claude/skills layout symlinks each skill dir into a shared store
// (e.g. <name> → ../../.agents/skills/<name>); os.ReadDir's DirEntry.IsDir() is false
// for a symlink (its type is symlink, not dir), so a plain e.IsDir() silently drops
// every symlinked skill. For a symlink we os.Stat the path (Stat follows the link) and
// accept it when the target is a directory. A dangling/broken link Stats with an error
// and is skipped.
func entryIsDir(root string, e os.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(root, e.Name()))
	return err == nil && info.IsDir()
}

// skillFrontmatter is the subset of SKILL.md YAML frontmatter we surface.
type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// readSkillMD reads a SKILL.md and extracts name + description from its leading YAML
// frontmatter (delimited by a `---` line at the top and a closing `---`). Returns
// ok=false when the file can't be read; a file with no/invalid frontmatter yields
// empty name/desc with ok=true (the caller falls back to the dir name).
func readSkillMD(path string) (name, desc string, ok bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", false
	}
	fm := extractFrontmatter(b)
	if fm == "" {
		return "", "", true
	}
	var meta skillFrontmatter
	if err := yaml.Unmarshal([]byte(fm), &meta); err != nil {
		return "", "", true
	}
	return strings.TrimSpace(meta.Name), strings.TrimSpace(meta.Description), true
}

// extractFrontmatter returns the YAML block between the leading `---` and the next
// `---`, or "" when the file does not open with a frontmatter fence. Tolerates a
// leading BOM / blank lines before the opening fence.
func extractFrontmatter(b []byte) string {
	s := strings.TrimPrefix(string(b), "\ufeff")
	lines := strings.Split(s, "\n")
	// find opening fence (first non-blank line must be ---)
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) || strings.TrimSpace(lines[i]) != "---" {
		return ""
	}
	i++
	var body []string
	for ; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.Join(body, "\n")
		}
		body = append(body, lines[i])
	}
	return "" // no closing fence → not valid frontmatter
}

// Fingerprint is a stable content hash of a scanned skill set — the caller reports the
// set to the center only when the fingerprint changes ("变了才重报"). Order-independent
// via the canonical sort in Scan; includes every field that the panel renders so a
// description-only edit still re-reports.
func Fingerprint(skills []Skill) string {
	h := sha256.New()
	for _, s := range skills {
		// \x1f field sep, \x1e record sep — bytes that can't appear in the values.
		h.Write([]byte(string(s.Layer)))
		h.Write([]byte{0x1f})
		h.Write([]byte(s.Name))
		h.Write([]byte{0x1f})
		h.Write([]byte(s.Description))
		h.Write([]byte{0x1f})
		if s.Shadowed {
			h.Write([]byte{1})
		} else {
			h.Write([]byte{0})
		}
		h.Write([]byte{0x1e})
	}
	return hex.EncodeToString(h.Sum(nil))
}
