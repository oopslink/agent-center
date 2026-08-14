package authorization

import "strings"

type MigrationMode string

const (
	MigrationModeLegacy  MigrationMode = "legacy"
	MigrationModeShadow  MigrationMode = "shadow"
	MigrationModeEnforce MigrationMode = "enforce"
)

type FeatureFlags struct {
	Mode           MigrationMode
	ShadowCompare  bool
	CacheEffective bool
}

func DefaultFeatureFlags() FeatureFlags {
	return FeatureFlags{
		Mode:           MigrationModeEnforce,
		ShadowCompare:  true,
		CacheEffective: true,
	}
}

func FeatureFlagsFromEnv(lookup func(string) (string, bool)) FeatureFlags {
	flags := DefaultFeatureFlags()
	if lookup == nil {
		return flags
	}
	if raw, ok := lookup("AC_AUTHZ_MODE"); ok {
		switch MigrationMode(strings.ToLower(strings.TrimSpace(raw))) {
		case MigrationModeLegacy, MigrationModeShadow, MigrationModeEnforce:
			flags.Mode = MigrationMode(strings.ToLower(strings.TrimSpace(raw)))
		}
	}
	if raw, ok := lookup("AC_AUTHZ_SHADOW_COMPARE"); ok {
		flags.ShadowCompare = envBool(raw, flags.ShadowCompare)
	}
	if raw, ok := lookup("AC_AUTHZ_CACHE"); ok {
		flags.CacheEffective = envBool(raw, flags.CacheEffective)
	}
	return flags.normalized()
}

func (f FeatureFlags) normalized() FeatureFlags {
	switch f.Mode {
	case MigrationModeLegacy, MigrationModeShadow, MigrationModeEnforce:
	default:
		f.Mode = MigrationModeEnforce
	}
	return f
}

func envBool(raw string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on", "enabled":
		return true
	case "0", "false", "no", "off", "disabled":
		return false
	default:
		return fallback
	}
}
