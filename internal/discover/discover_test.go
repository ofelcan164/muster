package discover

import (
	"os"
	"path/filepath"
	"testing"
)

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

// gitRepo writes a repository at dir, with origin set when remote is not empty.
// Written by hand rather than shelled out to git, which keeps the test fast and
// independent of whatever git is installed.
func gitRepo(t *testing.T, dir, remote, branch string) string {
	t.Helper()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := "[core]\n\trepositoryformatversion = 0\n"
	if remote != "" {
		config += "[remote \"origin\"]\n\turl = " + remote + "\n"
	}
	write(t, filepath.Join(gitDir, "config"), config)
	write(t, filepath.Join(gitDir, "HEAD"), "ref: refs/heads/"+branch+"\n")
	return dir
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Two local-only repos that happen to share a directory name are two repos.
// Keyed on the basename alone they merged into one card, which showed the
// first one's root, branch and agents for both.
func TestLocalReposWithTheSameNameStayApart(t *testing.T) {
	home := t.TempDir()
	a := gitRepo(t, filepath.Join(home, "work", "api"), "", "main")
	b := gitRepo(t, filepath.Join(home, "spikes", "api"), "", "wip")

	r := NewResolver()
	ia, ib := r.Resolve(a, nil), r.Resolve(b, nil)
	if ia.Key == ib.Key {
		t.Fatalf("both repos keyed %q", ia.Key)
	}
	if ia.Name != "api" || ib.Name != "api" {
		t.Errorf("display names changed: %q and %q, want both \"api\"", ia.Name, ib.Name)
	}
	if ia.Branch != "main" || ib.Branch != "wip" {
		t.Errorf("branches %q and %q, want main and wip", ia.Branch, ib.Branch)
	}
}

// A repo with a remote is keyed on it, so the same clone in two places is one
// repo wherever it sits.
func TestRemoteKeyedReposIgnorePath(t *testing.T) {
	home := t.TempDir()
	a := gitRepo(t, filepath.Join(home, "one", "api"), "git@github.com:acme/api.git", "main")
	b := gitRepo(t, filepath.Join(home, "two", "api"), "https://github.com/acme/api", "main")

	r := NewResolver()
	if ka, kb := r.Resolve(a, nil).Key, r.Resolve(b, nil).Key; ka != kb {
		t.Errorf("two clones of one repo keyed %q and %q", ka, kb)
	}
}

// A workspace opened before git init showed as a scratch directory. The answer
// was cached forever, so it stayed grey and unkeyed until the daemon restarted.
func TestGitInitIsPickedUpWithoutARestart(t *testing.T) {
	dir := t.TempDir()
	r := NewResolver()

	if info := r.Resolve(dir, nil); info.IsGit {
		t.Fatal("an empty directory resolved as a repository")
	}
	gitRepo(t, dir, "git@github.com:acme/fresh.git", "main")

	info := r.Resolve(dir, nil)
	if !info.IsGit {
		t.Fatal("git init was not noticed: the cached answer won")
	}
	if info.Key != "acme/fresh" || info.Branch != "main" {
		t.Errorf("key %q branch %q, want acme/fresh on main", info.Key, info.Branch)
	}
}

// A linked worktree keys to its own card while sharing the repo's name, and its
// gitdir pointer is relative as often as not.
func TestWorktreeResolvesThroughItsGitdirFile(t *testing.T) {
	home := t.TempDir()
	main := gitRepo(t, filepath.Join(home, "api"), "git@github.com:acme/api.git", "main")
	tree := filepath.Join(home, "api-feature")

	// What `git worktree add` writes: a .git file pointing at the main repo's
	// worktrees directory, and a commondir pointing back out of it.
	wtDir := filepath.Join(main, ".git", "worktrees", "api-feature")
	write(t, filepath.Join(wtDir, "commondir"), "../..\n")
	write(t, filepath.Join(wtDir, "HEAD"), "ref: refs/heads/feature\n")
	if err := os.MkdirAll(tree, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(tree, ".git"), "gitdir: "+wtDir+"\n")

	info := NewResolver().Resolve(tree, nil)
	if !info.IsWorktree {
		t.Fatal("a linked worktree was not recognised as one")
	}
	if info.Name != "acme/api" {
		t.Errorf("name %q, want the repo's own name", info.Name)
	}
	if info.Key == "acme/api" {
		t.Error("the worktree shares the main checkout's key, so they share one card")
	}
}
