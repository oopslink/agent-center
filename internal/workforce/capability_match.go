package workforce

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultCapabilityTTL bounds how long a worker's probe projection may be used
	// for scheduling when the reporter did not provide an explicit expires_at.
	DefaultCapabilityTTL = 5 * time.Minute

	CapabilityFeatureMCP     = "mcp"
	CapabilityFeatureSkills  = "skills"
	CapabilityFeatureSession = "session"
)

type CapabilityRequirement struct {
	AgentCLI          string
	VersionConstraint string
	Features          []string
}

type CapabilityMatch struct {
	OK     bool
	Reason string
	CLI    string
}

const (
	CapabilityMatchOK              = "ok"
	CapabilityMatchWorkerOffline   = "worker_offline"
	CapabilityMatchMissingCLI      = "missing_cli"
	CapabilityMatchNotDetected     = "not_detected"
	CapabilityMatchDisabled        = "disabled"
	CapabilityMatchUnhealthy       = "unhealthy"
	CapabilityMatchStale           = "stale_capability"
	CapabilityMatchInvalidVersion  = "invalid_version"
	CapabilityMatchVersionMismatch = "version_mismatch"
	CapabilityMatchMissingFeature  = "missing_feature"
)

var semverRe = regexp.MustCompile(`^v?([0-9]+)(?:\.([0-9]+))?(?:\.([0-9]+))?([\-+][0-9A-Za-z][0-9A-Za-z.\-+]*)?$`)

// NormalizeCapabilityVersion accepts common CLI probe shapes ("1", "1.2",
// "v1.2.3") and returns a canonical semver-like "MAJOR.MINOR.PATCH" string,
// preserving prerelease/build suffixes. Empty means "unknown" and is accepted.
func NormalizeCapabilityVersion(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	m := semverRe.FindStringSubmatch(s)
	if m == nil {
		return "", fmt.Errorf("%w: %q", ErrWorkerCapabilityVersion, raw)
	}
	parts := [3]string{m[1], "0", "0"}
	if m[2] != "" {
		parts[1] = m[2]
	}
	if m[3] != "" {
		parts[2] = m[3]
	}
	return strings.Join(parts[:], ".") + m[4], nil
}

func NormalizeCapabilityFeatures(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, f := range in {
		f = strings.ToLower(strings.TrimSpace(f))
		if f == "" {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

func (c Capability) EffectiveFeatures() []string {
	features := append([]string(nil), c.Features...)
	if c.SupportsMCP {
		features = append(features, CapabilityFeatureMCP)
	}
	if c.SupportsSkills {
		features = append(features, CapabilityFeatureSkills)
	}
	if c.SupportsSession {
		features = append(features, CapabilityFeatureSession)
	}
	return NormalizeCapabilityFeatures(features)
}

func (c Capability) FreshAt(now time.Time) bool {
	now = now.UTC()
	if !c.ExpiresAt.IsZero() && !now.Before(c.ExpiresAt.UTC()) {
		return false
	}
	if !c.ScannedAt.IsZero() && c.ExpiresAt.IsZero() && !now.Before(c.ScannedAt.UTC().Add(DefaultCapabilityTTL)) {
		return false
	}
	return true
}

func (c Capability) HealthyAt(now time.Time) bool {
	if !c.Detected || !c.Enabled {
		return false
	}
	// Legacy capability JSON had no health field. Treat an absent health window as
	// healthy so old rows remain schedulable until a fresh report supplies health.
	if (!c.ScannedAt.IsZero() || !c.ExpiresAt.IsZero()) && !c.Healthy {
		return false
	}
	return c.FreshAt(now)
}

func (w *Worker) CapabilityMatches(req CapabilityRequirement, now time.Time) CapabilityMatch {
	if w == nil || w.Status() != WorkerOnline {
		return CapabilityMatch{OK: false, Reason: CapabilityMatchWorkerOffline, CLI: req.AgentCLI}
	}
	cli := strings.TrimSpace(req.AgentCLI)
	if cli == "" {
		return CapabilityMatch{OK: true, Reason: CapabilityMatchOK}
	}
	cap, ok := w.CapabilityForCLI(cli)
	if !ok {
		return CapabilityMatch{OK: false, Reason: CapabilityMatchMissingCLI, CLI: cli}
	}
	return cap.Matches(req, now)
}

func (c Capability) Matches(req CapabilityRequirement, now time.Time) CapabilityMatch {
	cli := strings.TrimSpace(req.AgentCLI)
	if cli == "" {
		cli = strings.TrimSpace(c.AgentCLI)
	}
	if !c.Detected {
		return CapabilityMatch{OK: false, Reason: CapabilityMatchNotDetected, CLI: cli}
	}
	if !c.Enabled {
		return CapabilityMatch{OK: false, Reason: CapabilityMatchDisabled, CLI: cli}
	}
	if (!c.ScannedAt.IsZero() || !c.ExpiresAt.IsZero()) && !c.Healthy {
		return CapabilityMatch{OK: false, Reason: CapabilityMatchUnhealthy, CLI: cli}
	}
	if !c.FreshAt(now) {
		return CapabilityMatch{OK: false, Reason: CapabilityMatchStale, CLI: cli}
	}
	if strings.TrimSpace(req.VersionConstraint) != "" {
		ok, err := VersionSatisfies(c.Version, req.VersionConstraint)
		if err != nil {
			return CapabilityMatch{OK: false, Reason: CapabilityMatchInvalidVersion, CLI: cli}
		}
		if !ok {
			return CapabilityMatch{OK: false, Reason: CapabilityMatchVersionMismatch, CLI: cli}
		}
	}
	if missing := missingFeatures(c.EffectiveFeatures(), req.Features); len(missing) > 0 {
		return CapabilityMatch{OK: false, Reason: CapabilityMatchMissingFeature, CLI: cli}
	}
	return CapabilityMatch{OK: true, Reason: CapabilityMatchOK, CLI: cli}
}

func VersionSatisfies(version, constraint string) (bool, error) {
	v, err := parseComparableVersion(version)
	if err != nil {
		return false, err
	}
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return true, nil
	}
	parts := strings.FieldsFunc(constraint, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
	for _, p := range parts {
		if p == "" {
			continue
		}
		op := "=="
		raw := p
		for _, candidate := range []string{">=", "<=", "==", "=", ">", "<"} {
			if strings.HasPrefix(p, candidate) {
				op = candidate
				raw = strings.TrimSpace(strings.TrimPrefix(p, candidate))
				break
			}
		}
		want, err := parseComparableVersion(raw)
		if err != nil {
			return false, err
		}
		cmp := compareVersionTriplet(v, want)
		switch op {
		case "=", "==":
			if cmp != 0 {
				return false, nil
			}
		case ">=":
			if cmp < 0 {
				return false, nil
			}
		case ">":
			if cmp <= 0 {
				return false, nil
			}
		case "<=":
			if cmp > 0 {
				return false, nil
			}
		case "<":
			if cmp >= 0 {
				return false, nil
			}
		}
	}
	return true, nil
}

func missingFeatures(have, need []string) []string {
	need = NormalizeCapabilityFeatures(need)
	if len(need) == 0 {
		return nil
	}
	haveSet := map[string]struct{}{}
	for _, f := range NormalizeCapabilityFeatures(have) {
		haveSet[f] = struct{}{}
	}
	var missing []string
	for _, f := range need {
		if _, ok := haveSet[f]; !ok {
			missing = append(missing, f)
		}
	}
	return missing
}

func capabilityCLIKey(c Capability) string {
	return strings.TrimSpace(c.AgentCLI)
}

func mergeReportedCapability(in Capability, previous *Capability) Capability {
	out := in
	out.AgentCLI = capabilityCLIKey(in)
	out.Features = NormalizeCapabilityFeatures(out.Features)
	if norm, err := NormalizeCapabilityVersion(out.Version); err == nil {
		out.Version = norm
	}
	if previous != nil {
		if !out.ScannedAt.IsZero() && !previous.ScannedAt.IsZero() && out.ScannedAt.Before(previous.ScannedAt) {
			return *previous
		}
		out.Enabled = previous.Enabled
	}
	if !out.Detected {
		out.Enabled = false
	} else if previous == nil && !out.Enabled {
		out.Enabled = out.Detected
	}
	return out
}

func parseComparableVersion(raw string) ([3]int, error) {
	norm, err := NormalizeCapabilityVersion(raw)
	if err != nil {
		return [3]int{}, err
	}
	if norm == "" {
		return [3]int{}, fmt.Errorf("%w: empty", ErrWorkerCapabilityVersion)
	}
	core := norm
	if i := strings.IndexAny(core, "-+"); i >= 0 {
		core = core[:i]
	}
	pieces := strings.Split(core, ".")
	if len(pieces) != 3 {
		return [3]int{}, fmt.Errorf("%w: %q", ErrWorkerCapabilityVersion, raw)
	}
	var out [3]int
	for i, p := range pieces {
		n, err := strconv.Atoi(p)
		if err != nil {
			return [3]int{}, fmt.Errorf("%w: %q", ErrWorkerCapabilityVersion, raw)
		}
		out[i] = n
	}
	return out, nil
}

func compareVersionTriplet(a, b [3]int) int {
	for i := 0; i < 3; i++ {
		switch {
		case a[i] < b[i]:
			return -1
		case a[i] > b[i]:
			return 1
		}
	}
	return 0
}
