/*
Copyright 2019 The Skaffold Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cache

// CACHEDEBUG: this file is part of a local patch (not upstream Skaffold) that snapshots every
// input feeding an artifact's build-cache hash to disk, so a later run can report exactly which
// input(s) changed instead of just "Not found. Building".

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/mitchellh/go-homedir"

	"github.com/GoogleContainerTools/skaffold/v2/pkg/skaffold/constants"
)

// metaKeyPrefix marks non-file hash inputs (config, build args, platforms) in the snapshot map.
// "!" sorts before any letter, digit, "." or "/", so these consistently sort before real paths;
// a real dependency path starting with "!meta! " is unrealistic enough not to worry about.
const metaKeyPrefix = "!meta! "

// cacheDebugVerbose reports whether the raw, per-input CACHEDEBUG lines (one per dependency
// file, plus buildArgs/config/platforms/finalHash) should print to stderr. Off by default —
// there's one of these lines per dependency file per artifact per build, which is a lot of
// noise for something you only want when actively digging into a specific cache miss. Enable
// with SKAFFOLD_CACHEDEBUG=1 (or any non-empty value).
//
// This does NOT affect the cache-deps/ snapshot files or the stdout "(changes: ...)" summary —
// those always run regardless, since they're what make a plain "Not found. Building" actionable
// without turning on verbose logging first.
func cacheDebugVerbose() bool {
	return os.Getenv("SKAFFOLD_CACHEDEBUG") != ""
}

// debugf prints a CACHEDEBUG line to stderr, gated on cacheDebugVerbose().
func debugf(format string, args ...any) {
	if !cacheDebugVerbose() {
		return
	}
	fmt.Fprintf(os.Stderr, format, args...)
}

// cacheDepsDir is where per-artifact dependency snapshots are written, one file per artifact.
//
// Defaults to <home>/.skaffold/cache-deps, next to skaffold's own cache file
// (<home>/.skaffold/cache) — deliberately NOT relative to the current working directory. Most
// artifacts' `dependencies.paths` include "." or the project root, so a relative default (e.g.
// "./cache-deps") would make the snapshot file watch itself: every rewrite changes its own
// hash, which then shows up as a changed dependency on the very next run, forever. Override
// with SKAFFOLD_CACHE_DEBUG_DIR if you really want it elsewhere (e.g. inside the project, for a
// throwaway audit) — just make sure to add it to your artifacts' `ignore` list if so.
func cacheDepsDir() string {
	if dir := os.Getenv("SKAFFOLD_CACHE_DEBUG_DIR"); dir != "" {
		return dir
	}
	if home, err := homedir.Dir(); err == nil {
		return filepath.Join(home, constants.DefaultSkaffoldDir, "cache-deps")
	}
	// Fall back to CWD only if we can't resolve a home directory at all.
	return "cache-deps"
}

// changeSummaries holds the most recent cache-deps change summary per artifact, written here
// (during hash computation, in singleArtifactHash via dumpCacheDeps) and consumed once from
// lookup.go right after the hash is known, so it can ride along on the needsBuilding result and
// get printed on stdout next to "Not found. Building" instead of going to stderr — visible even
// when stderr is redirected to /dev/null.
var changeSummaries sync.Map // map[string]string, image name -> change summary

// takeCacheDepsChangeSummary returns and clears the change summary recorded for imageName, if
// any. "Take" (get-and-delete) rather than "get" so a stale summary can't leak into a later,
// unrelated cache lookup for the same artifact within a long-running `skaffold dev` loop.
func takeCacheDepsChangeSummary(imageName string) string {
	v, ok := changeSummaries.LoadAndDelete(imageName)
	if !ok {
		return ""
	}
	return v.(string)
}

// dumpCacheDeps snapshots the current set of hash inputs for an artifact to
// <cacheDepsDir>/<artifact>.txt and diffs it against the previous snapshot (if any). Side-effecting
// only; nothing here participates in the actual hash computation. Errors here (e.g. can't create
// the snapshot dir) are reported to stderr since they're about the debug tooling itself, not
// about why the cache missed.
func dumpCacheDeps(imageName string, current map[string]string) {
	dir := cacheDepsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "CACHEDEBUG %s: could not create %s: %v\n", imageName, dir, err)
		return
	}

	path := filepath.Join(dir, sanitizeArtifactFilename(imageName)+".txt")

	previous, err := readCacheDeps(path)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "CACHEDEBUG %s: could not read previous snapshot %s: %v\n", imageName, path, err)
	}

	if summary := buildCacheDepsChangeSummary(previous, current); summary != "" {
		changeSummaries.Store(imageName, summary)
	}

	if err := writeCacheDeps(path, current); err != nil {
		fmt.Fprintf(os.Stderr, "CACHEDEBUG %s: could not write snapshot %s: %v\n", imageName, path, err)
	}
}

// sanitizeArtifactFilename makes an image name safe to use as a filename. Image names can
// contain "/" (e.g. a registry path); ":" shows up if a tag ever ends up in ImageName.
func sanitizeArtifactFilename(imageName string) string {
	replacer := strings.NewReplacer("/", "_", ":", "_")
	return replacer.Replace(imageName)
}

// readCacheDeps parses a previously written snapshot file back into a path -> hash map.
// Format is one entry per line: "<path>\t<hash>". Tab-separated (rather than space-separated)
// because the "hash" side can be raw metadata content (e.g. the artifact's config JSON) that
// isn't guaranteed to be free of spaces, but is guaranteed to be free of tabs and newlines.
func readCacheDeps(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(f)
	// dependency lists can be long; grow past bufio's default 64KB line limit just in case
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		result[parts[0]] = parts[1]
	}
	return result, scanner.Err()
}

func writeCacheDeps(path string, current map[string]string) error {
	keys := make([]string, 0, len(current))
	for k := range current {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s\t%s\n", k, current[k])
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// changeSummaryIndent is the indentation for each entry line. Fixed rather than aligned to the
// artifact name's own "<name>: " prefix (retrieve.go prints that part, and doesn't know it here).
const changeSummaryIndent = "      "

// buildCacheDepsChangeSummary returns a multi-line, indented block listing what changed since
// the previous snapshot — one entry per line, e.g.:
//
//	    changes:
//	      ~ app.py
//	      + new.txt
//	      - old.txt
//
// or "" if there's no previous snapshot to compare against, or nothing changed (the common case:
// this is what keeps normal cache hits quiet). Meant to be printed on stdout right after "Not
// found. Building" — see lookup.go/retrieve.go — so it's visible even with stderr redirected away.
func buildCacheDepsChangeSummary(previous, current map[string]string) string {
	if previous == nil {
		return ""
	}

	var added, removed, changed []string
	for path, hash := range current {
		oldHash, existed := previous[path]
		switch {
		case !existed:
			added = append(added, path)
		case oldHash != hash:
			changed = append(changed, path)
		}
	}
	for path := range previous {
		if _, stillExists := current[path]; !stillExists {
			removed = append(removed, path)
		}
	}

	if len(added) == 0 && len(removed) == 0 && len(changed) == 0 {
		return ""
	}

	sort.Strings(changed)
	sort.Strings(added)
	sort.Strings(removed)

	var entries []string
	for _, p := range changed {
		entries = append(entries, "~ "+displayKey(p))
	}
	for _, p := range added {
		entries = append(entries, "+ "+displayKey(p))
	}
	for _, p := range removed {
		entries = append(entries, "- "+displayKey(p))
	}

	var b strings.Builder
	b.WriteString("    changes:\n")
	for _, e := range entries {
		b.WriteString(changeSummaryIndent)
		b.WriteString(e)
		b.WriteString("\n")
	}
	return b.String()
}

// displayKey strips the metadata marker off non-file keys so the summary reads naturally,
// e.g. "!meta! config" -> "config".
func displayKey(key string) string {
	return strings.TrimPrefix(key, metaKeyPrefix)
}
