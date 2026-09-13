// Package discover turns herdr's workspaces into Muster's repos.
//
// Nothing in here knows a repo name in advance and nothing is hardcoded. Every
// repo is learned at runtime from the cwd of the panes in a workspace, and what
// is learned is persisted in Muster's own state dir, never in the plugin.
//
// Git is read directly off disk rather than through the git binary. Resolving a
// root, a remote and a branch is three file reads, where shelling out would be
// three forks per workspace per reconcile in a process whose whole reason for
// existing is to be cheap.
package discover

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ofelcan164/muster/internal/herdr"
)

// Info is the resolved identity of a working directory.
type Info struct {
	// Key is what colour, sigil and grid slot hash from.
	Key string
	// Name is the display name: "owner/name" when there is a remote, else the
	// directory basename.
	Name string
	// Root is the repository root, or the plain cwd when there is no repo.
	Root   string
	Branch string
	// WorktreePath is set when Root is a linked worktree rather than the main
	// checkout.
	WorktreePath string
	IsWorktree   bool
	IsGit        bool
}

type Resolver struct {
	mu sync.Mutex
	// cache holds the stable half of resolution (root, remote name) per cwd.
	// Branch is deliberately not cached: it changes often and costs one small
	// file read.
	cache map[string]*Info
}

func NewResolver() *Resolver { return &Resolver{cache: map[string]*Info{}} }

// Resolve identifies the repository containing cwd. hint, when non-nil, is
// herdr's own worktree view of the workspace, which supplies the branch without
// any file read at all.
func (r *Resolver) Resolve(cwd string, hint *herdr.WorkspaceWorktree) Info {
	if cwd == "" {
		return Info{Key: "unknown", Name: "unknown", Root: ""}
	}
	r.mu.Lock()
	cached, ok := r.cache[cwd]
	r.mu.Unlock()

	var info Info
	if ok {
		info = *cached
	} else {
		info = resolveUncached(cwd)
		// Only cache an answer that cannot change. A directory that is not a
		// repository becomes one the moment someone runs git init, and a cached
		// "not a repo" left that workspace grey, unkeyed and sigil-less until
		// the daemon was restarted. Re-resolving costs a handful of stats on the
		// five second tick.
		if info.IsGit {
			cp := info
			r.mu.Lock()
			r.cache[cwd] = &cp
			r.mu.Unlock()
		}
	}

	// Branch, fresh every time. herdr's own view wins when it has one.
	switch {
	case hint != nil && hint.Branch != "":
		info.Branch = hint.Branch
		if hint.IsLinkedWorktree {
			info.IsWorktree = true
			info.WorktreePath = hint.Path
		}
	case info.IsGit:
		info.Branch = readBranch(info.Root)
	}
	return info
}

func resolveUncached(cwd string) Info {
	root, gitDir, ok := findRepoRoot(cwd)
	if !ok {
		// A scratch workspace with no repository. It is keyed on its cwd so its
		// agents still get a card rather than disappearing.
		return Info{
			Key:   "dir:" + cwd,
			Name:  filepath.Base(cwd),
			Root:  cwd,
			IsGit: false,
		}
	}

	info := Info{Root: root, IsGit: true}

	// A linked worktree's .git is a file pointing into the main repo's
	// .git/worktrees/<label>. Detect it so identity can stay keyed on
	// (repo, worktree) without the UI having to care yet.
	commonDir := gitDir
	if cd := readCommonDir(gitDir); cd != "" {
		commonDir = cd
		info.IsWorktree = true
		info.WorktreePath = root
	}

	remote := remoteName(filepath.Join(commonDir, "config"))
	name := remote
	if name == "" {
		// No origin: fall back to the basename of the main checkout, not of the
		// worktree, so every worktree of a repo shares one name.
		name = filepath.Base(filepath.Dir(strings.TrimSuffix(commonDir, string(filepath.Separator))))
		if base := filepath.Base(commonDir); base != ".git" {
			name = strings.TrimSuffix(base, ".git")
		}
	}
	info.Name = name

	// Identity key is (repo, worktree) from the first commit. Two clones of the
	// same repo deliberately share a key: they are the same repo, and the card
	// header disambiguates them by path.
	info.Key = name
	if remote == "" {
		// Without a remote the name is only a directory basename, and ~/a/api
		// and ~/b/api are two different repositories. Keyed on the name alone
		// they merged into one card showing the first one's root, branch and
		// agents. The git directory is what tells them apart, and it is the same
		// directory for every worktree of the same repo.
		info.Key = "local:" + commonDir
	}
	if info.IsWorktree {
		info.Key += "@" + filepath.Base(root)
	}
	return info
}

// findRepoRoot walks up from dir looking for a .git entry, returning the
// containing directory and the resolved git directory.
func findRepoRoot(dir string) (root, gitDir string, ok bool) {
	d := filepath.Clean(dir)
	for {
		candidate := filepath.Join(d, ".git")
		st, err := os.Stat(candidate)
		if err == nil {
			if st.IsDir() {
				return d, candidate, true
			}
			// .git is a file: "gitdir: <path>", used by linked worktrees and
			// submodules.
			if gd := readGitdirFile(candidate); gd != "" {
				if !filepath.IsAbs(gd) {
					gd = filepath.Join(d, gd)
				}
				return d, filepath.Clean(gd), true
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", "", false
		}
		d = parent
	}
}

func readGitdirFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(b))
	return strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
}

// readCommonDir returns the main repository's git dir when gitDir belongs to a
// linked worktree, else "".
func readCommonDir(gitDir string) string {
	b, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return ""
	}
	cd := strings.TrimSpace(string(b))
	if cd == "" {
		return ""
	}
	if !filepath.IsAbs(cd) {
		cd = filepath.Join(gitDir, cd)
	}
	return filepath.Clean(cd)
}

func readBranch(root string) string {
	_, gitDir, ok := findRepoRoot(root)
	if !ok {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(b))
	if ref, found := strings.CutPrefix(head, "ref: refs/heads/"); found {
		return ref
	}
	if len(head) >= 7 {
		return head[:7] // detached HEAD
	}
	return ""
}

// remoteName parses a git config file and normalises the origin URL to
// "owner/name". Any host and any protocol reduce to the same key, so the same
// repo cloned over ssh on one machine and https on another still matches.
func remoteName(configPath string) string {
	f, err := os.Open(configPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	var inOrigin bool
	var url string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			inOrigin = strings.HasPrefix(line, `[remote "origin"]`)
			continue
		}
		if !inOrigin {
			continue
		}
		if v, found := strings.CutPrefix(line, "url"); found {
			if _, after, ok := strings.Cut(v, "="); ok {
				url = strings.TrimSpace(after)
				break
			}
		}
	}
	return NormaliseRemote(url)
}

// NormaliseRemote reduces a git remote URL to "owner/name".
func NormaliseRemote(url string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return ""
	}
	// Slash first: a URL written "…/api.git/" still has to reduce to "acme/api",
	// and trimming ".git" from something ending in "/" does nothing at all.
	url = strings.TrimSuffix(url, "/")
	url = strings.TrimSuffix(url, ".git")

	// scp-style: git@host:owner/name
	if !strings.Contains(url, "://") {
		if _, after, ok := strings.Cut(url, ":"); ok && !strings.HasPrefix(url, "/") {
			url = after
		}
	} else {
		if _, after, ok := strings.Cut(url, "://"); ok {
			// Drop userinfo@host, keep the path.
			if _, path, ok := strings.Cut(after, "/"); ok {
				url = path
			}
		}
	}

	parts := strings.Split(strings.Trim(url, "/"), "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	if len(parts) == 1 && parts[0] != "" {
		return parts[0]
	}
	return ""
}
