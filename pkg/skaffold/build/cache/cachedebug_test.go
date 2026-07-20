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

// CACHEDEBUG: tests for the local cache-deps snapshot/diff patch (not upstream Skaffold).

import (
	"strings"
	"testing"

	"github.com/GoogleContainerTools/skaffold/v2/testutil"
)

func TestBuildCacheDepsChangeSummary(t *testing.T) {
	tests := []struct {
		description string
		previous    map[string]string
		current     map[string]string
		expectEmpty bool
		mustContain []string
	}{
		{
			description: "no previous snapshot",
			previous:    nil,
			current:     map[string]string{"app.py": "abc"},
			expectEmpty: true,
		},
		{
			description: "nothing changed",
			previous:    map[string]string{"app.py": "abc"},
			current:     map[string]string{"app.py": "abc"},
			expectEmpty: true,
		},
		{
			description: "file changed",
			previous:    map[string]string{"app.py": "abc"},
			current:     map[string]string{"app.py": "def"},
			mustContain: []string{"~ app.py"},
		},
		{
			description: "file added",
			previous:    map[string]string{"app.py": "abc"},
			current:     map[string]string{"app.py": "abc", "new.txt": "xyz"},
			mustContain: []string{"+ new.txt"},
		},
		{
			description: "file removed",
			previous:    map[string]string{"app.py": "abc", "old.txt": "xyz"},
			current:     map[string]string{"app.py": "abc"},
			mustContain: []string{"- old.txt"},
		},
		{
			description: "mixed changes all listed",
			previous:    map[string]string{"a": "1", "b": "2", "c": "3"},
			current:     map[string]string{"a": "1-changed", "b": "2", "d": "4"},
			mustContain: []string{"~ a", "+ d", "- c"},
		},
	}
	for _, test := range tests {
		testutil.Run(t, test.description, func(t *testutil.T) {
			summary := buildCacheDepsChangeSummary(test.previous, test.current)
			if test.expectEmpty {
				t.CheckDeepEqual("", summary)
				return
			}
			for _, s := range test.mustContain {
				if !strings.Contains(summary, s) {
					t.Errorf("expected summary to contain %q, got:\n%s", s, summary)
				}
			}
		})
	}
}

func TestNoChangeReason(t *testing.T) {
	tests := []struct {
		description string
		diag        cacheDiagnostics
		entry       ImageDetails
		mustContain string
	}{
		{
			description: "has a real change summary: passed through unchanged",
			diag:        cacheDiagnostics{changeSummary: "    changes:\n      ~ app.py\n", hadPreviousSnapshot: true},
			entry:       ImageDetails{ID: "some-id"},
			mustContain: "changes:",
		},
		{
			description: "no previous snapshot: first tracked run",
			diag:        cacheDiagnostics{changeSummary: "", hadPreviousSnapshot: false},
			entry:       ImageDetails{},
			mustContain: "first tracked run",
		},
		{
			description: "previous snapshot unchanged, hash never cached: required-artifact hint",
			diag:        cacheDiagnostics{changeSummary: "", hadPreviousSnapshot: true},
			entry:       ImageDetails{}, // zero value: this hash was never recorded in artifactCache
			mustContain: "required/dependent artifact",
		},
		{
			description: "previous snapshot unchanged, hash was cached: image-gone hint",
			diag:        cacheDiagnostics{changeSummary: "", hadPreviousSnapshot: true},
			entry:       ImageDetails{ID: "some-id"}, // non-zero: this hash WAS recorded before
			mustContain: "no longer exists",
		},
		{
			description: "previous snapshot unchanged, only a digest recorded: still counts as cached",
			diag:        cacheDiagnostics{changeSummary: "", hadPreviousSnapshot: true},
			entry:       ImageDetails{Digest: "sha256:abc"},
			mustContain: "no longer exists",
		},
	}
	for _, test := range tests {
		testutil.Run(t, test.description, func(t *testutil.T) {
			got := noChangeReason(test.diag, test.entry)
			if !strings.Contains(got, test.mustContain) {
				t.Errorf("expected result to contain %q, got:\n%s", test.mustContain, got)
			}
		})
	}
}
