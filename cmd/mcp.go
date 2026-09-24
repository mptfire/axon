package cmd

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/urfave/cli/v2"
	"heckel.io/ntfy/v2/log"
	"heckel.io/ntfy/v2/mcp"
)

func init() {
	commands = append(commands, cmdMCP)
}

var flagsMCP = []cli.Flag{
	&cli.StringFlag{Name: "server", Aliases: []string{"s"}, EnvVars: []string{"NTFY_SERVER"}, Value: "https://ntfy.sh", Usage: "ntfy server base URL"},
	&cli.StringFlag{Name: "token", Aliases: []string{"k"}, EnvVars: []string{"NTFY_TOKEN"}, Usage: "access token used to auth against the server (tk_...)"},
}

var cmdMCP = &cli.Command{
	Name:      "mcp",
	Usage:     "Run the MCP server for AI agents (stdio)",
	UsageText: "ntfy mcp [--server URL] [--token TK]",
	Action:    execMCP,
	Category:  categoryClient,
	Flags:     flagsMCP,
	Before:    initLogFunc,
	Description: `Run a Model Context Protocol (MCP) server that lets AI agents use a ntfy server
as their notification transport. The agent speaks JSON-RPC over stdin/stdout.

Tools: publish, read_messages, subscribe_wait, list_subscriptions, plan_subscription.

The MCP layer is a thin client of the public ntfy API: it inherits all rate limits,
access control and token scopes of the given credentials.

Examples:
  ntfy mcp                                   # talk to https://ntfy.sh, anonymous
  ntfy mcp --server https://ntfy.example.com # self-hosted server
  ntfy mcp --token tk_abc123                 # authenticated (account tools enabled)

Claude Desktop (claude_desktop_config.json):
  {
    "mcpServers": {
      "ntfy": { "command": "ntfy", "args": ["mcp", "--server", "https://ntfy.example.com", "--token", "tk_..."] }
    }
  }`,
}

func execMCP(c *cli.Context) error {
	if c.NArg() > 0 {
		return errors.New("no arguments expected, see 'ntfy mcp --help' for help")
	}
	// stdout is the MCP transport: logs must never pollute it
	log.SetOutput(os.Stderr)
	server := mcp.New(mcp.Config{
		ServiceBaseURL: strings.TrimSuffix(c.String("server"), "/"),
		AccessToken:    strings.TrimSpace(c.String("token")),
		Version:        c.App.Version,
	})
	return server.Serve(context.Background(), os.Stdin, os.Stdout)
}
