package cli

// Build-identity seams for the branch + built-at fields, mirroring the
// installBuildVersion / installBuildCommit pattern in handlers_install.go. The
// binary's main() threads the linker-injected main.buildBranch / main.buildBuiltAt
// (v2.8.1 version convention `${branch}-${commit}`, conventions.md §0) so
// server-side surfaces — notably GET /api/system/version backing the Settings
// version panel — can echo the same identity the build carries.

// installBuildBranch is a test seam over the linker-injected main.buildBranch.
var installBuildBranch = func() string { return "" }

// SetInstallBuildBranch threads main.buildBranch from the binary's main(). No-op
// for the empty/"unknown" sentinel so `go run` stays branch-agnostic.
func SetInstallBuildBranch(b string) {
	if b == "" || b == "unknown" {
		return
	}
	bb := b
	installBuildBranch = func() string { return bb }
}

// ResolvedBuildBranch returns the linker-injected branch, or "unknown".
func ResolvedBuildBranch() string {
	if b := installBuildBranch(); b != "" {
		return b
	}
	return "unknown"
}

// installBuildBuiltAt is a test seam over the linker-injected main.buildBuiltAt
// (RFC3339 UTC build timestamp).
var installBuildBuiltAt = func() string { return "" }

// SetInstallBuildBuiltAt threads main.buildBuiltAt from the binary's main().
func SetInstallBuildBuiltAt(t string) {
	if t == "" || t == "unknown" {
		return
	}
	bt := t
	installBuildBuiltAt = func() string { return bt }
}

// ResolvedBuildBuiltAt returns the linker-injected build timestamp, or "unknown".
func ResolvedBuildBuiltAt() string {
	if t := installBuildBuiltAt(); t != "" {
		return t
	}
	return "unknown"
}

// ResolvedBuildCommit returns the linker-injected commit (the same value the
// install command uses via installerCommit), exported so server-side surfaces
// (e.g. /api/system/version) can echo it. Falls back to "unknown".
func ResolvedBuildCommit() string {
	return installerCommit()
}
