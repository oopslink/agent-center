package mention

import (
	"reflect"
	"testing"
)

func TestPresent(t *testing.T) {
	cases := []struct {
		text, name string
		want       bool
	}{
		{"hey @hayang can you help", "hayang", true},
		{"@hayang", "hayang", true},
		{"ping @hayang.", "hayang", true},
		{"see @hayanglee shelf", "hayang", false}, // token boundary
		{"email hayang@host", "hayang", false},    // not a leading-@ mention
		{"no mention here", "hayang", false},
		{"cc @HaYang twice @hayang", "hayang", true}, // case-insensitive + multiple
		{"yo @bot-2 deploy", "bot-2", true},          // hyphen in name
		{"  trim  ", "  hayang  ", false},            // name trimmed, absent
		{"anything", "", false},                      // empty name never present
	}
	for _, c := range cases {
		if got := Present(c.text, c.name); got != c.want {
			t.Errorf("Present(%q,%q)=%v want %v", c.text, c.name, got, c.want)
		}
	}
}

func TestTokenPresent(t *testing.T) {
	if !TokenPresent("hi @bot", "@bot") {
		t.Fatal("expected @bot present")
	}
	if TokenPresent("hi @bottom", "@bot") {
		t.Fatal("@bot must not match @bottom")
	}
}

func TestMentionsAll(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"hey @all please review", true},
		{"@all", true},
		{"ping @ALL now", true},     // case-insensitive
		{"done, @all.", true},       // trailing punctuation = token boundary
		{"@allies assemble", false}, // token boundary (not @all)
		{"@allocated", false},
		{"email all@host", false}, // not a leading-@ mention
		{"no broadcast here", false},
		{"cc @bob and @all", true},
	}
	for _, c := range cases {
		if got := MentionsAll(c.text); got != c.want {
			t.Errorf("MentionsAll(%q)=%v want %v", c.text, got, c.want)
		}
	}
}

func TestExtractTokens(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"hey @bob and @alice", []string{"bob", "alice"}},
		{"@all please", []string{"all"}},
		{"cc @Bob and @bob again", []string{"bob"}}, // dedup + lowercase, order kept
		{"email user@host.com only", nil},           // mid-token @ is not a mention
		{"@agent-center-ba6bc42a re-review", []string{"agent-center-ba6bc42a"}},
		{"ref agent:agent-ba6bc42a here", nil}, // colon-ref is not an @token
		{"lonely @ sign", nil},
		{"plain text", nil},
	}
	for _, c := range cases {
		if got := ExtractTokens(c.text); !reflect.DeepEqual(got, c.want) {
			t.Errorf("ExtractTokens(%q)=%v want %v", c.text, got, c.want)
		}
	}
}

func TestContainsRef(t *testing.T) {
	cases := []struct {
		text, ref string
		want      bool
	}{
		{"please @agent:agent-ba6bc42a help", "agent:agent-ba6bc42a", true},
		{"bare agent:agent-ba6bc42a ref", "agent:agent-ba6bc42a", true},
		{"AGENT:AGENT-BA6BC42A loud", "agent:agent-ba6bc42a", true},    // case-insensitive
		{"xagent:agent-ba6bc42ab tail", "agent:agent-ba6bc42a", false}, // both-side boundary
		{"no ref here", "agent:agent-ba6bc42a", false},
		{"anything", "", false},
	}
	for _, c := range cases {
		if got := ContainsRef(c.text, c.ref); got != c.want {
			t.Errorf("ContainsRef(%q,%q)=%v want %v", c.text, c.ref, got, c.want)
		}
	}
}

func TestTokenMatchesID(t *testing.T) {
	cases := []struct {
		token, idForm string
		want          bool
	}{
		{"agent-center-ba6bc42a", "agent-ba6bc42a", true},                  // token contains member-id fragment
		{"agent-ba6bc42a", "agent-ba6bc42a", true},                         // exact member-id
		{"ba6bc42a", "agent-ba6bc42a", true},                               // bare fragment token
		{"01jabcdefghijklmnopqrstuvw", "01JABCDEFGHIJKLMNOPQRSTUVW", true}, // entity ULID, case-insensitive
		{"agent-center-dev", "agent-ba6bc42a", false},                      // unrelated token
		{"ba6", "agent-ba6", false},                                        // fragment too short (<6)
		{"", "agent-ba6bc42a", false},
		{"agent-center-ba6bc42a", "", false},
	}
	for _, c := range cases {
		if got := TokenMatchesID(c.token, c.idForm); got != c.want {
			t.Errorf("TokenMatchesID(%q,%q)=%v want %v", c.token, c.idForm, got, c.want)
		}
	}
}
