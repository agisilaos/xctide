package main

import "strings"

func printableVersion() string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}
