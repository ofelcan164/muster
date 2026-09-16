package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ofelcan164/muster/internal/herdr"
)

// cmdUpdate installs the newest release over this one. herdr has no plugin
// update command, and reinstalling runs no hook, so this is `herdr plugin
// install` at the newest tag followed by the startup hook's `install --auto`,
// which rewrites the skill and badge if the new build renders them differently.
//
// The install moves this checkout aside and deletes it. That is fine for a
// process already running from it: the binary stays mapped, and the follow-up
// runs the new build by its absolute path. The daemon restarts itself once it
// sees the new musterd, and an open overlay keeps the old build until reopened.
func cmdUpdate() int {
	id := os.Getenv("HERDR_PLUGIN_ID")
	if id == "" {
		id = "muster"
	}
	plugins, err := herdr.NewClient("").Plugins()
	if err != nil {
		fmt.Fprintf(os.Stderr, "muster update: %v\n", err)
		return 1
	}
	var me *herdr.Plugin
	for i := range plugins {
		if plugins[i].PluginID == id {
			me = &plugins[i]
		}
	}
	if me == nil {
		fmt.Fprintf(os.Stderr, "muster update: herdr has no plugin %q installed\n", id)
		return 1
	}
	src := me.Source
	if src.Kind != "github" {
		fmt.Fprintf(os.Stderr, "muster update: %s is a %s checkout at %s, pull and rebuild it there\n",
			id, src.Kind, me.PluginRoot)
		return 1
	}

	repo := src.Owner + "/" + src.Repo
	out, err := exec.Command("git", "ls-remote", "--tags", "--refs", "https://github.com/"+repo).Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "muster update: listing releases of %s: %v\n", repo, err)
		return 1
	}
	latest := newestTag(string(out))
	if latest == "" {
		fmt.Fprintf(os.Stderr, "muster update: %s has no release tags\n", repo)
		return 1
	}
	if !newer(latest, me.Version) {
		fmt.Printf("already up to date: %s, newest release is %s\n", me.Version, latest)
		return 0
	}

	bin := herdrBin()
	if bin == "" {
		fmt.Fprintln(os.Stderr, "muster update: HERDR_BIN_PATH is not set, run this through herdr")
		return 1
	}
	fmt.Printf("updating %s from %s to %s\n", id, me.Version, latest)
	install := exec.Command(bin, "plugin", "install", repo, "--ref", latest, "--yes")
	install.Stdout, install.Stderr = os.Stdout, os.Stderr
	if err := install.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "muster update: herdr plugin install: %v\n", err)
		return 1
	}

	hook := exec.Command(filepath.Join(me.PluginRoot, "bin", "muster"), "install", "--auto")
	hook.Stdout, hook.Stderr = os.Stdout, os.Stderr
	if err := hook.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "muster update: install --auto: %v\n", err)
		return 1
	}
	fmt.Printf("updated to %s, reopen Muster to use it\n", latest)
	return 0
}

// newestTag picks the highest vX.Y.Z tag out of `git ls-remote --tags` output.
// Anything else, pre-releases included, is not a release.
func newestTag(lsRemote string) string {
	best := ""
	for _, line := range strings.Split(lsRemote, "\n") {
		_, ref, ok := strings.Cut(line, "\trefs/tags/")
		if !ok || !strings.HasPrefix(ref, "v") {
			continue
		}
		if _, ok := parseVersion(ref); ok && (best == "" || newer(ref, best)) {
			best = ref
		}
	}
	return best
}

// newer reports whether version a is above b. A leading v is optional on both,
// and b failing to parse counts as older, so a broken manifest still updates.
func newer(a, b string) bool {
	va, _ := parseVersion(a)
	vb, ok := parseVersion(b)
	if !ok {
		return true
	}
	for i := range va {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	return false
}

func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}
