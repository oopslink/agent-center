package files

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/idgen"
)

func TestFileURI_NewParseValidate(t *testing.T) {
	id := idgen.MustNewULID()
	uri, err := NewFileURI(id)
	if err != nil {
		t.Fatalf("NewFileURI: %v", err)
	}
	if want := "ac://files/" + id; uri.String() != want {
		t.Fatalf("uri = %q, want %q", uri, want)
	}
	if uri.ULID() != id {
		t.Fatalf("ULID() = %q, want %q", uri.ULID(), id)
	}
	if err := uri.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	parsed, err := ParseFileURI(uri.String())
	if err != nil || parsed != uri {
		t.Fatalf("ParseFileURI round-trip: %v %q", err, parsed)
	}
}

func TestFileURI_Rejects(t *testing.T) {
	cases := map[string]error{
		"":                      ErrEmptyURI,
		"http://files/x":        ErrBadScheme,
		"ac://transfers/x":      ErrBadScheme,
		"ac://files/":           ErrBadScheme,
		"ac://files/not-a-ulid": ErrBadULID,
		"ac://files/abc/def":    ErrBadScheme,
	}
	for in, wantErr := range cases {
		if _, err := ParseFileURI(in); err != wantErr {
			t.Errorf("ParseFileURI(%q) err = %v, want %v", in, err, wantErr)
		}
	}
}

func TestLocalResolver_BucketsByHashNotULID(t *testing.T) {
	r := NewLocalResolver("/root")
	id := idgen.MustNewULID()
	uri, _ := NewFileURI(id)
	p, err := r.ObjectPath(uri)
	if err != nil {
		t.Fatalf("ObjectPath: %v", err)
	}
	// Path must be {root}/objects/{h1}/{h2}/{ulid} with 2-char hex buckets.
	want := filepath.Join("/root", "objects", bucketHash(id)[0:2], bucketHash(id)[2:4], id)
	if p != want {
		t.Fatalf("ObjectPath = %q, want %q", p, want)
	}
	// Buckets derive from hash(ulid), NOT the ULID's own (time-ordered) prefix,
	// so the first directory segment should not just be the ULID prefix.
	seg := strings.Split(p, string(filepath.Separator))
	h1 := seg[len(seg)-3]
	if h1 == strings.ToLower(id[0:2]) && bucketHash(id)[0:2] != strings.ToLower(id[0:2]) {
		t.Fatalf("bucket appears to key on ULID prefix, not hash")
	}
	// Determinism: same URI -> same path.
	p2, _ := r.ObjectPath(uri)
	if p != p2 {
		t.Fatalf("resolver not deterministic: %q vs %q", p, p2)
	}
}

func TestFileScope_IsValid(t *testing.T) {
	for _, s := range []FileScope{ScopeTask, ScopeIssue, ScopeProject, ScopeConversation, ScopeAgent, ScopeTmp} {
		if !s.IsValid() {
			t.Errorf("%s should be valid", s)
		}
	}
	if FileScope("bogus").IsValid() {
		t.Error("bogus scope should be invalid")
	}
}

func TestFileReference_Validate(t *testing.T) {
	uri, _ := NewFileURI(idgen.MustNewULID())
	good := FileReference{ID: "r1", FileURI: uri, Scope: ScopeConversation, ScopeID: "conv-1"}
	if err := good.Validate(); err != nil {
		t.Fatalf("good ref invalid: %v", err)
	}
	if err := (FileReference{ID: "r2", FileURI: uri, Scope: "nope", ScopeID: "x"}).Validate(); err != ErrInvalidScope {
		t.Fatalf("want ErrInvalidScope, got %v", err)
	}
	if err := (FileReference{ID: "r3", FileURI: uri, Scope: ScopeTask, ScopeID: ""}).Validate(); err == nil {
		t.Fatalf("want error for empty scope_id")
	}
}
