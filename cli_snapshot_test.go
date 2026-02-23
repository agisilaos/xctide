package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpSnapshotRoot(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	assertGoldenBytes(t, filepath.Join("docs", "help", "root.txt"), []byte(strings.TrimSpace(buf.String())))
}

func TestCompletionSnapshots(t *testing.T) {
	cases := []struct {
		shell  string
		golden string
	}{
		{shell: "bash", golden: filepath.Join("testdata", "help", "completion-bash.golden")},
		{shell: "zsh", golden: filepath.Join("testdata", "help", "completion-zsh.golden")},
		{shell: "fish", golden: filepath.Join("testdata", "help", "completion-fish.golden")},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.shell, func(t *testing.T) {
			script, err := completionScript(tc.shell)
			if err != nil {
				t.Fatalf("completionScript(%q) returned error: %v", tc.shell, err)
			}
			assertGoldenBytes(t, tc.golden, []byte(strings.TrimSpace(script)))
		})
	}
}
