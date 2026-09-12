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

// cmdTrigger emits a sample event so a handler can be exercised without
// manufacturing a real payment.
//
// Before this, seeing a single webhook meant creating a charge or a payout.
// One debugging session resorted to sending payouts purely to generate an
// event, which is a lot of moving parts to test one handler.
func cmdTrigger(args []string) int {
	fs := flag.NewFlagSet("trigger", flag.ExitOnError)
	apiKey := fs.String("api-key", "", "Override the stored key")
	_ = fs.Parse(args)

	eventType := fs.Arg(0)
	if eventType == "" {
		fmt.Fprintln(os.Stderr,
			"error: an event type is required, e.g. bachs trigger collection.succeeded")
		return 2
	}

	cfg, err := config.Load(*apiKey)
	if err != nil {
		return fail(err)
	}
	if !cfg.IsSandbox() {
		// Fail here rather than let the server refuse, so the reason is clear:
		// a fake collection.succeeded in production could have a merchant
		// fulfil an order nobody paid for.
		fmt.Fprintln(os.Stderr,
			"error: trigger is sandbox only. Use a sk_sandbox_ key, or replay a\n"+
				"       real event with: bachs events replay <event_id>")
		return 2
	}

	res, err := api.New(cfg).Trigger(context.Background(), eventType)
	if err != nil {
		return fail(fmt.Errorf("could not trigger: %w", err))
	}

	fmt.Printf(
		"%s✓%s %s %s%s%s\n",
		listen.Green, listen.Reset, res.EventType,
		listen.Dim, res.EventID, listen.Reset,
	)
	if res.Destinations == 0 {
		// The event was emitted correctly but nothing received it. Saying so
		// here is the difference between a five-minute fix and an hour of
		// wondering why the handler never fired.
		fmt.Printf(
			"%sNo destination received it. Start `bachs listen`, or add one with\n"+
				"`bachs endpoints create`.%s\n",
			listen.Dim, listen.Reset,
		)
	} else {
		fmt.Printf(
			"%sfanned out to %d destination(s)%s\n",
			listen.Dim, res.Destinations, listen.Reset,
		)
	}
	return 0
}
