package main

import (
	"fmt"
	"github.com/urfave/cli/v2"
	"go/build"
	"heckel.io/ntfy/v2/cmd"
	"os"
	"runtime"
	"strings"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	cli.AppHelpTemplate += buildHelp()

	app := cmd.New()
	app.Version = version

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func buildHelp() string {
	if len(commit) > 7 {
		commit = commit[:7]
	}
	var tags string
	if len(build.Default.BuildTags) > 0 {
		tags = ", with tags " + strings.Join(build.Default.BuildTags, ", ")
	}
	return fmt.Sprintf(`
Try 'ntfy COMMAND --help' or https://ntfy.sh/docs/ for more information.

To report a bug, open an issue on GitHub: https://github.com/binwiederhier/ntfy/issues.
If you want to chat, simply join the Discord server (https://discord.gg/cT7ECsZj9w), or
the Matrix room (https://matrix.to/#/#ntfy:matrix.org).

ntfy %s (%s), runtime %s, built at %s%s
Copyright (C) Philipp C. Heckel, licensed under Apache License 2.0 & GPLv2
`, version, commit, runtime.Version(), date, tags)
}
