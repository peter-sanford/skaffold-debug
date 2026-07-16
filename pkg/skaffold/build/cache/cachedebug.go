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

	"github.com/mitchellh/go-homedir"

	"github.com/GoogleContainerTools/skaffold/v2/pkg/skaffold/constants"
)

// metaKeyPrefix marks non-file hash inputs (config, build args, platforms) in the snapshot map.
// "!" sorts before any letter, digit, "." or "/", so these consistently sort before real paths;
// a real dependency path starting with "!meta! " is unrealistic enough not to worry about.
const metaKeyPrefix = "!meta! "

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

// dumpCacheDeps snapshots the current set of hash inputs for an artifact to
// <cacheDepsDir>/<artifact>.txt, diffs it against the previous snapshot (if any), and reports
// any differences to stderr as CACHEDEBUG lines. Side-effecting only; nothing here participates
// in the actual hash computation.
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

	reportCacheDepsDiff(imageName, previous, current)

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

// reportCacheDepsDiff prints which entries were added, removed, or changed since the previous
// snapshot. Nothing is printed if there's no previous snapshot to compare against, or if
// nothing changed (the common case: this is what keeps normal cache hits quiet).
func reportCacheDepsDiff(imageName string, previous, current map[string]string) {
	if previous == nil {
		return
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
		return
	}

	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(changed)

	// Use a distinct tag from the raw per-dependency CACHEDEBUG lines so this summary is easy
	// to pick out on its own, e.g. `grep 'CACHEDEBUG-DIFF'`.
	tag := "CACHEDEBUG-DIFF"

	fmt.Fprintf(os.Stderr, "%s %s: %s\n", tag, imageName, summarizeCounts(len(changed), len(added), len(removed)))

	// Column-align the symbol/path so a run with a mix of changed/added/removed reads as a
	// table instead of three differently-shaped sentences.
	maxPathLen := 0
	for _, p := range append(append(append([]string{}, changed...), added...), removed...) {
		if l := len(displayKey(p)); l > maxPathLen {
			maxPathLen = l
		}
	}

	for _, p := range changed {
		fmt.Fprintf(os.Stderr, "%s %s:   ~ %-*s  %s -> %s\n", tag, imageName, maxPathLen, displayKey(p),
			truncateForDisplay(previous[p], 12), truncateForDisplay(current[p], 12))
	}
	for _, p := range added {
		fmt.Fprintf(os.Stderr, "%s %s:   + %s\n", tag, imageName, displayKey(p))
	}
	for _, p := range removed {
		fmt.Fprintf(os.Stderr, "%s %s:   - %s\n", tag, imageName, displayKey(p))
	}
}

// summarizeCounts renders a one-line, pluralization-aware summary like
// "2 cache inputs changed since last run (2 changed, 1 added, 0 removed)".
func summarizeCounts(changed, added, removed int) string {
	total := changed + added + removed
	noun := "input"
	if total != 1 {
		noun = "inputs"
	}
	return fmt.Sprintf("%d cache %s changed since last run (%d changed, %d added, %d removed)",
		total, noun, changed, added, removed)
}

// truncateForDisplay shortens long values (e.g. a full artifact config JSON blob) so a changed
// line stays a single skimmable line instead of dumping the whole value.
func truncateForDisplay(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// displayKey strips the metadata marker off non-file keys so the report reads naturally,
// e.g. "!meta! config" -> "config".
func displayKey(key string) string {
	return strings.TrimPrefix(key, metaKeyPrefix)
}
