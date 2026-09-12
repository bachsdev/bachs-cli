package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/bachsdev/bachs-cli/internal/api"
	"github.com/bachsdev/bachs-cli/internal/config"
	"github.com/bachsdev/bachs-cli/internal/listen"
)

func cmdEndpointsList(args []string) int {
	fs := flag.NewFlagSet("endpoints list", flag.ExitOnError)
	apiKey := fs.String("api-key", "", "Override the stored key")
	_ = fs.Parse(args)

	cfg, err := config.Load(*apiKey)
	if err != nil {
		return fail(err)
	}

	endpoints, err := api.New(cfg).ListEndpoints(context.Background())
	if err != nil {
		return fail(fmt.Errorf("could not list endpoints: %w", err))
	}
	if len(endpoints) == 0 {
		fmt.Println("No webhook destinations configured.")
		fmt.Printf(
			"%sEvents are still recorded, but nothing receives them. "+
				"Add one with: bachs endpoints create --url <url>%s\n",
			listen.Dim, listen.Reset,
		)
		return 0
	}

	for _, e := range endpoints {
		mark, colour := "✓", listen.Green
		if !e.Enabled {
			mark, colour = "-", listen.Dim
		}
		url := "(no url)"
		if e.URL != nil {
			url = *e.URL
		}
		fmt.Printf(
			"%s%s%s %-28s %s\n    %s%s · %d event type(s)%s\n",
			colour, mark, listen.Reset, e.Name, url,
			listen.Dim, e.EndpointID, len(e.EventTypes), listen.Reset,
		)
	}
	return 0
}

func cmdEndpointsCreate(args []string) int {
	fs := flag.NewFlagSet("endpoints create", flag.ExitOnError)
	url := fs.String("url", "", "HTTPS URL to deliver to (required)")
	name := fs.String("name", "", "Label for this destination")
	events := fs.String("events", "", "Comma-separated event types (required)")
	apiKey := fs.String("api-key", "", "Override the stored key")
	_ = fs.Parse(args)

	if *url == "" {
		fmt.Fprintln(os.Stderr, "error: --url is required")
		return 2
	}
	if *events == "" {
		// The API rejects an empty list anyway; failing here saves a round trip
		// and says which flag to fix.
		fmt.Fprintln(os.Stderr,
			"error: --events is required, e.g. --events collection.succeeded,refund.paid")
		return 2
	}

	var types []string
	for _, t := range strings.Split(*events, ",") {
		if trimmed := strings.TrimSpace(t); trimmed != "" {
			types = append(types, trimmed)
		}
	}

	label := *name
	if label == "" {
		label = "Created by bachs CLI"
	}

	cfg, err := config.Load(*apiKey)
	if err != nil {
		return fail(err)
	}

	created, err := api.New(cfg).CreateEndpoint(context.Background(),
		api.CreateEndpointRequest{Name: label, URL: *url, EventTypes: types})
	if err != nil {
		return fail(fmt.Errorf("could not create endpoint: %w", err))
	}

	fmt.Printf("%sCreated%s %s\n", listen.Bold, listen.Reset, created.EndpointID)
	// To stderr so `bachs endpoints create > out.txt` does not write a live
	// signing secret into a file someone later shares.
	if created.Secret != "" {
		fmt.Fprintf(os.Stderr,
			"Signing secret: %s%s%s\nStore it now. It is not shown again.\n",
			listen.Bold, created.Secret, listen.Reset)
	}
	return 0
}

func cmdEndpointsDelete(args []string) int {
	fs := flag.NewFlagSet("endpoints delete", flag.ExitOnError)
	apiKey := fs.String("api-key", "", "Override the stored key")
	_ = fs.Parse(args)

	id := fs.Arg(0)
	if id == "" {
		fmt.Fprintln(os.Stderr, "error: an endpoint id is required, e.g. whe_abc123")
		return 2
	}

	cfg, err := config.Load(*apiKey)
	if err != nil {
		return fail(err)
	}
	if err := api.New(cfg).DeleteEndpoint(context.Background(), id); err != nil {
		return fail(fmt.Errorf("could not delete endpoint: %w", err))
	}
	fmt.Printf("Deleted %s\n", id)
	return 0
}

func endpointsUsage() {
	fmt.Fprint(os.Stderr, `Usage:
  bachs endpoints list                       Your webhook destinations
  bachs endpoints create --url <url> --events a,b
  bachs endpoints delete <endpoint_id>
`)
}
