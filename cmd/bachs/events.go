package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bachsdev/bachs-cli/internal/api"
	"github.com/bachsdev/bachs-cli/internal/config"
	"github.com/bachsdev/bachs-cli/internal/listen"
)

// cmdEventsList prints recent events and how their delivery went.
//
// This exists because `bachs events replay` needs an event id and there was no
// way to get one from the terminal. Replay was documented but unreachable: you
// had to already know an id, and nothing told you one.
//
// The delivery summary is the other half. An event with zero attempts means
// nothing was configured to receive it, which looks identical to "delivery
// failed" if all you have is a handler that never fired.
func cmdEventsList(args []string) int {
	fs := flag.NewFlagSet("events list", flag.ExitOnError)
	limit := fs.Int("limit", 20, "How many events to show (max 100)")
	eventType := fs.String("type", "", "Only this event type, e.g. collection.succeeded")
	failedOnly := fs.Bool("failed", false, "Only events whose last delivery failed")
	undelivered := fs.Bool("undelivered", false, "Only events nothing was listening for")
	apiKey := fs.String("api-key", "", "Override the stored key")
	_ = fs.Parse(args)

	cfg, err := config.Load(*apiKey)
	if err != nil {
		return fail(err)
	}

	// Filtering is client-side, so fetch a wider page than asked for when a
	// filter is set. Otherwise `--type x --limit 20` silently searches only
	// the newest 20 events and reports "none" for a type that exists.
	fetch := *limit
	if *eventType != "" || *failedOnly || *undelivered {
		fetch = 100
	}

	res, err := api.New(cfg).ListEvents(context.Background(), fetch, 0)
	if err != nil {
		return fail(fmt.Errorf("could not list events: %w", err))
	}

	matched := make([]api.Event, 0, len(res.Items))
	for _, e := range res.Items {
		if *eventType != "" && e.EventType != *eventType {
			continue
		}
		if *failedOnly && e.Failed == 0 {
			continue
		}
		if *undelivered && e.Attempts != 0 {
			continue
		}
		matched = append(matched, e)
		if len(matched) >= *limit {
			break
		}
	}

	if len(matched) == 0 {
		fmt.Println("No events matched.")
		if res.Total > 0 && (*eventType != "" || *failedOnly || *undelivered) {
			fmt.Printf(
				"%s%d event(s) exist; the newest %d were searched. Widen with --limit.%s\n",
				listen.Dim, res.Total, len(res.Items), listen.Reset,
			)
		}
		return 0
	}

	for _, e := range matched {
		mark, colour := deliveryMark(e)
		fmt.Printf(
			"%s%s%s %-32s %s%s%s  %s\n",
			colour, mark, listen.Reset,
			e.EventType,
			listen.Dim, e.EventID, listen.Reset,
			deliverySummary(e),
		)
	}

	if len(matched) < res.Total {
		fmt.Printf(
			"\n%sShowing %d of %d. Replay one with: bachs events replay <event_id>%s\n",
			listen.Dim, len(matched), res.Total, listen.Reset,
		)
	}
	return 0
}

// deliveryMark reduces the delivery outcome to one glyph.
func deliveryMark(e api.Event) (string, string) {
	switch {
	case e.Attempts == 0:
		return "-", listen.Dim
	case e.Failed > 0 && e.Success == 0:
		return "✗", listen.Red
	case e.Failed > 0:
		return "!", listen.Yellow
	default:
		return "✓", listen.Green
	}
}

func deliverySummary(e api.Event) string {
	if e.Attempts == 0 {
		// The single most useful line in this command: it answers "why did my
		// handler never fire" without the reader having to guess.
		return listen.Dim + "no destination was listening" + listen.Reset
	}

	parts := []string{fmt.Sprintf("%d attempt(s)", e.Attempts)}
	if e.LastAttemptHTTPStatus != nil {
		parts = append(parts, fmt.Sprintf("last HTTP %d", *e.LastAttemptHTTPStatus))
	} else if e.LastAttemptError != nil && *e.LastAttemptError != "" {
		err := *e.LastAttemptError
		if len(err) > 44 {
			err = err[:44] + "…"
		}
		parts = append(parts, err)
	}
	if e.LastAttemptAt != nil {
		if t, perr := time.Parse(time.RFC3339, *e.LastAttemptAt); perr == nil {
			parts = append(parts, humanAge(t))
		}
	}
	return listen.Dim + strings.Join(parts, ", ") + listen.Reset
}

func humanAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// cmdEventsReplayAll redelivers every failed event in one go, for after you
// have fixed the bug that made them fail.
func cmdEventsReplayAll(args []string) int {
	fs := flag.NewFlagSet("events replay-failed", flag.ExitOnError)
	limit := fs.Int("limit", 20, "Most events to replay")
	eventType := fs.String("type", "", "Only this event type")
	dryRun := fs.Bool("dry-run", false, "List what would be replayed and stop")
	apiKey := fs.String("api-key", "", "Override the stored key")
	_ = fs.Parse(args)

	cfg, err := config.Load(*apiKey)
	if err != nil {
		return fail(err)
	}
	client := api.New(cfg)
	ctx := context.Background()

	res, err := client.ListEvents(ctx, 100, 0)
	if err != nil {
		return fail(fmt.Errorf("could not list events: %w", err))
	}

	targets := make([]api.Event, 0)
	for _, e := range res.Items {
		if e.Failed == 0 {
			continue
		}
		if *eventType != "" && e.EventType != *eventType {
			continue
		}
		targets = append(targets, e)
		if len(targets) >= *limit {
			break
		}
	}

	if len(targets) == 0 {
		fmt.Println("Nothing to replay: no failed deliveries in the events searched.")
		return 0
	}

	if *dryRun {
		fmt.Printf("Would replay %d event(s):\n", len(targets))
		for _, e := range targets {
			fmt.Printf("  %s %s\n", e.EventType, e.EventID)
		}
		return 0
	}

	var ok, bad int
	for _, e := range targets {
		if _, rerr := client.Replay(ctx, e.EventID); rerr != nil {
			bad++
			fmt.Printf(
				"%s✗%s %s %s%s%s %v\n",
				listen.Red, listen.Reset, e.EventType,
				listen.Dim, e.EventID, listen.Reset, rerr,
			)
			continue
		}
		ok++
		fmt.Printf(
			"%s✓%s %s %s%s%s queued\n",
			listen.Green, listen.Reset, e.EventType,
			listen.Dim, e.EventID, listen.Reset,
		)
	}

	fmt.Printf("\n%d queued, %d failed\n", ok, bad)
	if bad > 0 {
		return 1
	}
	return 0
}

func eventsUsage() {
	fmt.Fprint(os.Stderr, `Usage:
  bachs events list            Recent events and how their delivery went
  bachs events replay <id>     Redeliver one event
  bachs events replay-failed   Redeliver events whose delivery failed
`)
}
