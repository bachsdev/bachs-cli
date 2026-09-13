package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/bachsdev/bachs-cli/internal/api"
	"github.com/bachsdev/bachs-cli/internal/config"
	"github.com/bachsdev/bachs-cli/internal/listen"
)

const sessionsUsage = `Usage:
  bachs sessions list             Forwarding sessions on this account
  bachs sessions close <id>       Close one
`

func cmdSessions(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, sessionsUsage)
		return 2
	}

	switch args[0] {
	case "list":
		return cmdSessionsList(args[1:])
	case "close":
		return cmdSessionsClose(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n%s", args[0], sessionsUsage)
		return 2
	}
}

// cmdSessionsList shows what is forwarding right now.
//
// The question this answers is "why are my events going somewhere else?", which
// happens when `bachs listen` is still running on another machine. So the
// listing leads with whether anything is actually holding the socket rather
// than with the stored status, which a killed process never gets to update.
func cmdSessionsList(args []string) int {
	fs := flag.NewFlagSet("sessions list", flag.ExitOnError)
	apiKey := fs.String("api-key", "", "Override the stored key")
	_ = fs.Parse(args)

	cfg, err := config.Load(*apiKey)
	if err != nil {
		return fail(err)
	}

	sessions, err := api.New(cfg).ListSessions(context.Background())
	if err != nil {
		return fail(fmt.Errorf("could not list sessions: %w", err))
	}
	if len(sessions) == 0 {
		fmt.Println("No forwarding sessions.")
		fmt.Printf(
			"%sStart one with: bachs listen --forward-to localhost:3000/webhooks%s\n",
			listen.Dim, listen.Reset,
		)
		return 0
	}

	for _, s := range sessions {
		mark, colour, state := "✓", listen.Green, "live"
		if !s.Live {
			mark, colour, state = "-", listen.Dim, "not connected"
		}

		name := s.DeviceName
		if name == "" {
			name = "(unnamed)"
		}

		fmt.Printf(
			"%s%s%s %-24s %s\n    %s%s · %s%s\n",
			colour, mark, listen.Reset, name, s.ForwardTo,
			listen.Dim, s.SessionID, state, listen.Reset,
		)
	}
	return 0
}

func cmdSessionsClose(args []string) int {
	fs := flag.NewFlagSet("sessions close", flag.ExitOnError)
	apiKey := fs.String("api-key", "", "Override the stored key")
	_ = fs.Parse(args)

	sessionID := fs.Arg(0)
	if sessionID == "" {
		fmt.Fprintln(os.Stderr,
			"error: a session id is required, e.g. bachs sessions close whls_8f2e...")
		fmt.Fprintf(os.Stderr, "%sList them with: bachs sessions list%s\n",
			listen.Dim, listen.Reset)
		return 2
	}

	cfg, err := config.Load(*apiKey)
	if err != nil {
		return fail(err)
	}

	if err := api.New(cfg).CloseSession(context.Background(), sessionID); err != nil {
		return fail(fmt.Errorf("could not close session: %w", err))
	}

	fmt.Printf("%s✓%s closed %s\n", listen.Green, listen.Reset, sessionID)
	return 0
}
