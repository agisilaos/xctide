package main

import "testing"

func TestPrintableVersion(t *testing.T) {
	original := version
	t.Cleanup(func() { version = original })

	version = "v0.2.0"
	if got := printableVersion(); got != "0.2.0" {
		t.Fatalf("printableVersion() = %q, want %q", got, "0.2.0")
	}

	version = "0.3.1"
	if got := printableVersion(); got != "0.3.1" {
		t.Fatalf("printableVersion() = %q, want %q", got, "0.3.1")
	}
}
