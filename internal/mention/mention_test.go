package mention

import "testing"

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
