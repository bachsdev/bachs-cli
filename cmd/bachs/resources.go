package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/bachsdev/bachs-cli/internal/api"
	"github.com/bachsdev/bachs-cli/internal/config"
	"github.com/bachsdev/bachs-cli/internal/listen"
	"github.com/bachsdev/bachs-cli/internal/resources"
)

// cmdResource runs any API operation as `bachs <group> <verb> [id] [flags]`.
//
// The command table is generated from the OpenAPI spec, so this stays in step
// with the API without anyone maintaining a list. Flags map to query params
// and request body fields, and output is JSON so it pipes into jq.
func cmdResource(group string, args []string) int {
	if len(args) == 0 {
		printGroupHelp(group)
		return 2
	}

	verb := args[0]
	cmd, ok := resources.Find(group, verb)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown operation %q for %s\n\n", verb, group)
		printGroupHelp(group)
		return 2
	}

	// Hand-rolled rather than using flag, because the flag set differs per
	// operation and every value is a string heading for a query param or a
	// JSON body. Positional args fill path params in order.
	var (
		positional []string
		flags      = map[string]string{}
		apiKey     string
	)
	for i := 0; i < len(args[1:]); i++ {
		a := args[1:][i]
		switch {
		case strings.HasPrefix(a, "--"):
			name := strings.TrimPrefix(a, "--")
			value := ""
			if eq := strings.Index(name, "="); eq >= 0 {
				name, value = name[:eq], name[eq+1:]
			} else if i+1 < len(args[1:]) && !strings.HasPrefix(args[1:][i+1], "--") {
				i++
				value = args[1:][i]
			} else {
				value = "true" // a bare --flag is a boolean
			}
			if name == "api-key" {
				apiKey = value
				continue
			}
			flags[name] = value
		default:
			positional = append(positional, a)
		}
	}

	if len(positional) < len(cmd.PathParams) {
		fmt.Fprintf(os.Stderr,
			"error: %s %s needs %s\n\n  bachs %s %s <%s>\n",
			group, verb, strings.Join(cmd.PathParams, ", "),
			group, verb, strings.Join(cmd.PathParams, "> <"))
		return 2
	}

	cfg, err := config.Load(apiKey)
	if err != nil {
		return fail(err)
	}

	path := cmd.Path
	for i, p := range cmd.PathParams {
		path = strings.ReplaceAll(path, "{"+p+"}", url.PathEscape(positional[i]))
	}

	// A flag named in the spec's query params becomes a query param; anything
	// else becomes a body field. That keeps `--limit 5` and `--name "Thing"`
	// both working without the user knowing which is which.
	query := url.Values{}
	body := map[string]any{}
	known := map[string]bool{}
	for _, q := range cmd.Query {
		known[q.Name] = true
	}
	for name, value := range flags {
		if known[name] {
			query.Set(name, value)
			continue
		}
		if cmd.HasBody {
			body[name] = coerce(value)
			continue
		}
		query.Set(name, value)
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}

	var payload any
	if len(body) > 0 {
		payload = body
	}

	var out json.RawMessage
	err = api.New(cfg).Raw(context.Background(), cmd.Method, path, payload, &out)
	if err != nil {
		return fail(err)
	}

	pretty, perr := json.MarshalIndent(out, "", "  ")
	if perr != nil {
		fmt.Println(string(out))
		return 0
	}
	fmt.Println(string(pretty))
	return 0
}

// coerce turns a flag string into the JSON type the API expects.
//
// Money must stay a string, so numeric-looking values with a decimal point are
// left alone. Sending 29.00 as a float would be both wrong and lossy.
func coerce(v string) any {
	switch v {
	case "true":
		return true
	case "false":
		return false
	}
	if strings.Contains(v, ".") {
		return v
	}
	var n int64
	if _, err := fmt.Sscanf(v, "%d", &n); err == nil && fmt.Sprint(n) == v {
		return n
	}
	return v
}

func printGroupHelp(group string) {
	cmds := resources.InGroup(group)
	if len(cmds) == 0 {
		fmt.Fprintf(os.Stderr, "unknown resource %q\n", group)
		return
	}
	fmt.Fprintf(os.Stderr, "Usage: bachs %s <operation> [flags]\n\nOperations:\n", group)
	for _, c := range cmds {
		args := ""
		for _, p := range c.PathParams {
			args += " <" + p + ">"
		}
		fmt.Fprintf(os.Stderr, "  %-14s%-24s %s\n", c.Verb, args, c.Summary)
	}
}

// printResourceGroups lists the resource groups for the top-level help.
func printResourceGroups() {
	groups := resources.Groups()
	sort.Strings(groups)
	fmt.Fprintln(os.Stderr, "\nResources:")

	var line []string
	for _, g := range groups {
		line = append(line, g)
		if len(line) == 4 {
			fmt.Fprintf(os.Stderr, "  %s\n", strings.Join(line, "  "))
			line = nil
		}
	}
	if len(line) > 0 {
		fmt.Fprintf(os.Stderr, "  %s\n", strings.Join(line, "  "))
	}
	fmt.Fprintf(os.Stderr,
		"\n%sRun \"bachs <resource>\" to see its operations.%s\n",
		listen.Dim, listen.Reset)
}

// isResourceGroup reports whether a word names a generated resource group.
func isResourceGroup(name string) bool {
	for _, g := range resources.Groups() {
		if g == name {
			return true
		}
	}
	return false
}
