package discover

import "testing"

func TestNormaliseRemote(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"git@github.com:acme/contracts.git", "acme/contracts"},
		{"https://github.com/acme/api.git", "acme/api"},
		{"https://github.com/acme/api", "acme/api"},
		{"ssh://git@github.com/otherorg/web.git", "otherorg/web"},
		{"git@gitlab.example.com:group/sub.git", "group/sub"},
		{"https://user:token@github.com/acme/api.git", "acme/api"},
		{"/srv/git/bare.git", "git/bare"},
		{"", ""},
	}
	for _, c := range cases {
		if got := NormaliseRemote(c.in); got != c.want {
			t.Errorf("NormaliseRemote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The same repo cloned over ssh on one machine and https on another must
// resolve to one identity, or its colour, sigil and grid slot would differ per
// machine.
func TestNormaliseRemoteProtocolAgnostic(t *testing.T) {
	ssh := NormaliseRemote("git@github.com:acme/api.git")
	https := NormaliseRemote("https://github.com/acme/api.git")
	if ssh != https {
		t.Fatalf("ssh %q != https %q", ssh, https)
	}
}

// Two repos sharing a basename in different orgs must not collide, which is the
// case the owner/name key exists to separate.
func TestOrgDisambiguation(t *testing.T) {
	a := NormaliseRemote("git@github.com:acme/web.git")
	b := NormaliseRemote("ssh://git@github.com/otherorg/web.git")
	if a == b {
		t.Fatalf("expected distinct keys, both %q", a)
	}
}
