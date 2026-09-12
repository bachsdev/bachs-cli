// Package resources exposes every public API operation as a CLI subcommand.
//
// The table is generated from the OpenAPI spec rather than hand-written, so
// coverage tracks the API. A hand-written table drifts the moment someone adds
// an endpoint, and the drift stays invisible until a user asks why a
// documented operation is missing.
package resources

import "sort"

// Command is one API operation as the CLI exposes it.
type Command struct {
	Group      string // "products"
	Verb       string // "list"
	Method     string // "GET"
	Path       string // "/v1/products/{product_id}"
	Summary    string
	PathParams []string
	Query      []QueryFlag
	HasBody    bool
}

type QueryFlag struct {
	Name        string
	Type        string
	Description string
}

// Groups returns the resource groups, sorted.
func Groups() []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range Commands {
		if !seen[c.Group] {
			seen[c.Group] = true
			out = append(out, c.Group)
		}
	}
	sort.Strings(out)
	return out
}

// InGroup returns the commands in one group, sorted by verb.
func InGroup(group string) []Command {
	var out []Command
	for _, c := range Commands {
		if c.Group == group {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Verb < out[j].Verb })
	return out
}

// Find resolves a group and verb to a command.
func Find(group, verb string) (Command, bool) {
	for _, c := range Commands {
		if c.Group == group && c.Verb == verb {
			return c, true
		}
	}
	return Command{}, false
}
