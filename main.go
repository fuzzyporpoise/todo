// Package main is the entry point for the todo CLI.
package main

import (
	_ "embed"
	"strings"

	cmd "go.fuzzyporpoise.dev/todo/cmd/todo"
)

//go:embed VERSION
var versionFile string

// version and buildTime are set by GoReleaser via ldflags at build time.
var (
	version   string
	buildTime string
)

// use embedded VERSION file for local `go install`d version
func init() {
	if version == "" {
		version = strings.TrimSpace(versionFile)
	}
}

func main() {
	cmd.SetVersion(version)
	cmd.SetBuildTime(buildTime)
	cmd.Run()
}
