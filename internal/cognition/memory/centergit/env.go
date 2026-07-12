package centergit

// safeDefaultPath is a deterministic minimal PATH that finds git on common
// dev / CI images (mirrors internal/cognition/memory so behaviour is identical).
func safeDefaultPath() string {
	return "/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin"
}

// baseGitEnv returns a hermetic environment for git invocations: no global /
// system config leaks in, no prompts, English output for stable error parsing.
// author{Name,Email} may be empty for plumbing that creates no commits
// (init/config); when set they populate GIT_AUTHOR_* / GIT_COMMITTER_*.
func baseGitEnv(homeOverride, authorName, authorEmail string) []string {
	env := []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"LANGUAGE=en",
		"LC_ALL=en_US.UTF-8",
		"PATH=" + safeDefaultPath(),
	}
	if authorName != "" {
		env = append(env,
			"GIT_AUTHOR_NAME="+authorName,
			"GIT_COMMITTER_NAME="+authorName,
		)
	}
	if authorEmail != "" {
		env = append(env,
			"GIT_AUTHOR_EMAIL="+authorEmail,
			"GIT_COMMITTER_EMAIL="+authorEmail,
		)
	}
	if homeOverride != "" {
		env = append(env, "HOME="+homeOverride, "XDG_CONFIG_HOME="+homeOverride)
	}
	return env
}
