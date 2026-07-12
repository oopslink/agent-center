package centergit

import (
	"errors"
	"testing"
)

func TestRepoRefValidate(t *testing.T) {
	tests := []struct {
		name    string
		ref     RepoRef
		wantErr bool
	}{
		{"agent ok", AgentRepo("agent-1"), false},
		{"team ok", TeamRepo("team-alpha"), false},
		{"global ok", GlobalRepo(), false},
		{"global with id", RepoRef{Kind: RepoKindGlobal, ID: "x"}, true},
		{"agent empty id", AgentRepo(""), true},
		{"team traversal", TeamRepo(".."), true},
		{"agent leading dot", AgentRepo(".hidden"), true},
		{"agent slash", AgentRepo("a/b"), true},
		{"agent backslash", AgentRepo("a\\b"), true},
		{"unknown kind", RepoRef{Kind: "nope", ID: "x"}, true},
		{"too long", AgentRepo(string(make([]byte, 129))), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.ref.Validate()
			if tc.wantErr != (err != nil) {
				t.Fatalf("Validate()=%v, wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr && err != nil && !errors.Is(err, ErrInvalidRepoRef) {
				t.Fatalf("want ErrInvalidRepoRef, got %v", err)
			}
		})
	}
}

func TestRepoRefDirNameAndString(t *testing.T) {
	cases := []struct {
		ref      RepoRef
		wantDir  string
		wantName string
	}{
		{AgentRepo("a1"), "agent/a1.git", "agent:a1"},
		{TeamRepo("t1"), "team/t1.git", "team:t1"},
		{GlobalRepo(), "global.git", "global"},
	}
	for _, c := range cases {
		if got := c.ref.dirName(); got != c.wantDir {
			t.Errorf("dirName()=%q want %q", got, c.wantDir)
		}
		if got := c.ref.String(); got != c.wantName {
			t.Errorf("String()=%q want %q", got, c.wantName)
		}
	}
}

func TestParseRepoPath(t *testing.T) {
	tests := []struct {
		in      string
		wantRef RepoRef
		wantSub string
		wantErr bool
	}{
		{"/agent/a1.git/info/refs", AgentRepo("a1"), "info/refs", false},
		{"agent/a1.git/git-upload-pack", AgentRepo("a1"), "git-upload-pack", false},
		{"/team/t1.git/git-receive-pack", TeamRepo("t1"), "git-receive-pack", false},
		{"/global.git/info/refs", GlobalRepo(), "info/refs", false},
		{"/global.git/objects/info/packs", GlobalRepo(), "objects/info/packs", false},
		{"/unknown/x.git/info/refs", RepoRef{}, "", true},
		{"/agent/x/info/refs", RepoRef{}, "", true}, // missing .git
		{"/agent", RepoRef{}, "", true},
		{"/team/../.git/info/refs", RepoRef{}, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			ref, sub, err := parseRepoPath(tc.in)
			if tc.wantErr != (err != nil) {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if ref != tc.wantRef || sub != tc.wantSub {
				t.Fatalf("got (%v,%q) want (%v,%q)", ref, sub, tc.wantRef, tc.wantSub)
			}
		})
	}
}
