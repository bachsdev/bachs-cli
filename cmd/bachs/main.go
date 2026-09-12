// Command bachs is the Bachs command-line interface.
//
//	bachs login   --api-key sk_sandbox_...
//	bachs whoami
//	bachs listen  --forward-to localhost:3000/webhooks [--events a,b]
//	bachs events replay <evt_id>
//
// A note on flag naming, because this is the point where it is cheap to get
// right. --events takes event type names and is the only way to select them;
// there is deliberately no parallel vocabulary for a second kind of event.
// Stripe grew one (--thin-events, --forward-thin-to, --forward-thin-connect-to)
// and spent years unwinding it. If Bachs ever ships a second payload shape, it
// extends --events.
//
// Deliberately dependency-light: stdlib flag rather than a CLI framework. This
// binary ships to machines we do not control, and the command surface is small
// enough that a framework would add supply chain for no user-visible gain.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bachsdev/bachs-cli/internal/api"
	"github.com/bachsdev/bachs-cli/internal/config"
	"github.com/bachsdev/bachs-cli/internal/listen"
)

// version is overridden at build time: -ldflags "-X main.version=1.2.3"
var version = "dev"

const usage = `bachs — Bachs command-line interface

Usage:
  bachs <command> [flags]

Commands:
  login          Store an API key
  whoami         Show the active environment
  listen         Forward live events to a local port
  events         List past events and redeliver them
  endpoints      Manage your webhook destinations
  trigger        Emit a sample event (sandbox only)

Run "bachs <command> --help" for details on a command.
`

func main() {
	api.Version = version
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Print(usage)
		return 2
	}

	switch args[0] {
	case "-h", "--help", "help":
		fmt.Print(usage)
		printResourceGroups()
		return 0
	case "-v", "--version", "version":
		fmt.Printf("bachs %s\n", version)
		return 0
	case "login":
		return cmdLogin(args[1:])
	case "whoami":
		return cmdWhoami(args[1:])
	case "listen":
		return cmdListen(args[1:])
	case "events":
		return cmdEvents(args[1:])
	case "endpoints":
		return cmdEndpoints(args[1:])
	case "trigger":
		return cmdTrigger(args[1:])
	default:
		// Anything else may name a generated resource group, e.g.
		// `bachs products list`. Checked last so a hand-written command of the
		// same name always wins.
		if isResourceGroup(args[0]) {
			return cmdResource(args[0], args[1:])
		}
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", args[0], usage)
		printResourceGroups()
		return 2
	}
}

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	return 1
}

func cmdLogin(args []string) int {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	apiKey := fs.String("api-key", "", "API key (sk_sandbox_... or sk_live_...)")
	_ = fs.Parse(args)

	if *apiKey == "" {
		fmt.Fprintln(os.Stderr, "error: --api-key is required")
		return 2
	}

	path, err := config.Save(*apiKey)
	if err != nil {
		return fail(err)
	}

	env := "live"
	if strings.HasPrefix(*apiKey, "sk_sandbox_") {
		env = "sandbox"
	}
	fmt.Printf("Saved %s credentials to %s\n", env, path)
	if env == "live" {
		fmt.Println("Note: this is a live key. `bachs listen` will forward real events.")
	}
	return 0
}

func cmdWhoami(args []string) int {
	fs := flag.NewFlagSet("whoami", flag.ExitOnError)
	apiKey := fs.String("api-key", "", "Override the stored key")
	_ = fs.Parse(args)

	cfg, err := config.Load(*apiKey)
	if err != nil {
		return fail(err)
	}

	env := "live"
	if cfg.IsSandbox() {
		env = "sandbox"
	}
	fmt.Printf("Environment: %s\n", env)
	fmt.Printf("API base:    %s\n", cfg.BaseURL)
	fmt.Printf("Key:         %s\n", cfg.Redacted())
	return 0
}

func cmdListen(args []string) int {
	fs := flag.NewFlagSet("listen", flag.ExitOnError)
	forwardTo := fs.String(
		"forward-to", "", "Local target, e.g. localhost:3000/webhooks (required)",
	)
	events := fs.String(
		"events", "",
		"Comma-separated event types, e.g. collection.succeeded,refund.paid",
	)
	all := fs.Bool("all", false, "Forward every event type (the default)")
	deviceName := fs.String("device-name", "", "Label for this session in the dashboard")
	apiKey := fs.String("api-key", "", "Override the stored key")
	fs.StringVar(forwardTo, "f", "", "Short for --forward-to")
	fs.StringVar(events, "e", "", "Short for --events")
	_ = fs.Parse(args)

	if *forwardTo == "" {
		fmt.Fprintln(os.Stderr, "error: --forward-to is required")
		return 2
	}
	if *events != "" && *all {
		// Silently preferring one would leave the user wondering why the
		// filter they passed did nothing.
		fmt.Fprintln(os.Stderr, "error: pass either --events or --all, not both")
		return 2
	}

	cfg, err := config.Load(*apiKey)
	if err != nil {
		return fail(err)
	}

	var selected []string
	if !*all && *events != "" {
		for _, item := range strings.Split(*events, ",") {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				selected = append(selected, trimmed)
			}
		}
	}

	// Ctrl-C should release the session, not abandon it, so the signal
	// cancels a context the shutdown path hangs off.
	ctx, stop := signal.NotifyContext(
		context.Background(), os.Interrupt, syscall.SIGTERM,
	)
	defer stop()

	err = listen.Run(ctx, cfg, listen.Options{
		ForwardTo:  *forwardTo,
		Events:     selected,
		DeviceName: *deviceName,
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		return fail(err)
	}
	fmt.Println("\nStopped.")
	return 0
}

func cmdEvents(args []string) int {
	if len(args) == 0 {
		eventsUsage()
		return 2
	}
	switch args[0] {
	case "list":
		return cmdEventsList(args[1:])
	case "replay-failed":
		return cmdEventsReplayAll(args[1:])
	case "replay":
		// falls through to the single-event replay below
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", args[0])
		eventsUsage()
		return 2
	}

	fs := flag.NewFlagSet("events replay", flag.ExitOnError)
	apiKey := fs.String("api-key", "", "Override the stored key")
	// Accepted for symmetry with `listen`. Replay targets registered endpoints
	// and any live session server-side, so nothing local is involved.
	_ = fs.String("forward-to", "", "Accepted for symmetry; replay is server-side")
	_ = fs.Parse(args[1:])

	eventID := fs.Arg(0)
	if eventID == "" {
		fmt.Fprintln(os.Stderr, "error: an event id is required, e.g. evt_3ab4e0d5...")
		return 2
	}

	cfg, err := config.Load(*apiKey)
	if err != nil {
		return fail(err)
	}

	res, err := api.New(cfg).Replay(context.Background(), eventID)
	if err != nil {
		return fail(fmt.Errorf("replay failed: %w", err))
	}

	fmt.Printf(
		"queued %s %s attempt %s\n",
		res.EventType, res.EventID, res.AttemptID,
	)
	return 0
}

func cmdEndpoints(args []string) int {
	if len(args) == 0 {
		endpointsUsage()
		return 2
	}
	switch args[0] {
	case "list":
		return cmdEndpointsList(args[1:])
	case "create":
		return cmdEndpointsCreate(args[1:])
	case "delete":
		return cmdEndpointsDelete(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", args[0])
		endpointsUsage()
		return 2
	}
}
