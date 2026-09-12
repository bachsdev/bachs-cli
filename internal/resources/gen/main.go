//go:build ignore

// Generates the resource command table from the OpenAPI spec.
//
//	go run ./internal/resources/gen -spec openapi.json -out internal/resources/table.go
//
// Generated rather than hand-written so coverage tracks the API. A hand-written
// table drifts the moment someone adds an endpoint, and the drift is invisible
// until a user asks why a documented operation is missing from the CLI.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

type spec struct {
	Paths map[string]map[string]operation `json:"paths"`
}

type operation struct {
	OperationID string      `json:"operationId"`
	Summary     string      `json:"summary"`
	Tags        []string    `json:"tags"`
	Parameters  []parameter `json:"parameters"`
	RequestBody *struct {
		Content map[string]struct {
			Schema struct {
				Ref        string              `json:"$ref"`
				Properties map[string]property `json:"properties"`
				Required   []string            `json:"required"`
			} `json:"schema"`
		} `json:"content"`
	} `json:"requestBody"`
}

type parameter struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
	Schema      struct {
		Type string `json:"type"`
	} `json:"schema"`
}

type property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

var pathParam = regexp.MustCompile(`\{(\w+)\}`)

// Command is one CLI subcommand derived from one API operation.
type Command struct {
	Group      string // "products"
	Verb       string // "list"
	Method     string
	Path       string
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

// verbFor maps an operationId to the subcommand a user would guess.
//
// createProduct -> create, listProducts -> list, archiveProduct -> archive.
// Falling back to the whole id keeps unusual operations reachable rather than
// dropping them, which is the failure mode that makes a generated CLI feel
// incomplete.
func verbFor(operationID, group string) string {
	s := operationID
	for _, suffix := range []string{
		singular(group), group, strings.Title(singular(group)), strings.Title(group),
	} {
		s = strings.TrimSuffix(s, suffix)
		s = strings.TrimSuffix(s, strings.Title(suffix))
	}
	if s == "" {
		return kebab(operationID)
	}
	return kebab(s)
}

func kebab(s string) string {
	var out []rune
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				out = append(out, '-')
			}
			r = r + 32
		}
		out = append(out, r)
	}
	return string(out)
}

func singular(s string) string {
	switch {
	case strings.HasSuffix(s, "ies"):
		return strings.TrimSuffix(s, "ies") + "y"
	case strings.HasSuffix(s, "s"):
		return strings.TrimSuffix(s, "s")
	}
	return s
}

func groupFor(tags []string) string {
	if len(tags) == 0 {
		return "misc"
	}
	// Tags are title-cased with spaces ("Checkout Sessions"). Lowercase and
	// join on a single hyphen. Running kebab() over an already-hyphenated
	// string would emit "checkout--sessions", because the capital and the
	// space each contribute one.
	parts := strings.Fields(tags[0])
	for i, p := range parts {
		parts[i] = strings.ToLower(p)
	}
	return strings.Join(parts, "-")
}

func main() {
	specPath := flag.String("spec", "", "OpenAPI JSON")
	outPath := flag.String("out", "", "Go file to write")
	flag.Parse()

	raw, err := os.ReadFile(*specPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	var s spec
	if err := json.Unmarshal(raw, &s); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	var cmds []Command
	for path, methods := range s.Paths {
		for method, op := range methods {
			switch method {
			case "get", "post", "patch", "delete", "put":
			default:
				continue
			}
			// Internal routes are not part of the public surface.
			if strings.HasPrefix(path, "/v1/i/") {
				continue
			}
			group := groupFor(op.Tags)
			c := Command{
				Group:   group,
				Verb:    verbFor(op.OperationID, group),
				Method:  strings.ToUpper(method),
				Path:    path,
				Summary: op.Summary,
				HasBody: op.RequestBody != nil,
			}
			for _, m := range pathParam.FindAllStringSubmatch(path, -1) {
				c.PathParams = append(c.PathParams, m[1])
			}
			for _, p := range op.Parameters {
				if p.In != "query" {
					continue
				}
				t := p.Schema.Type
				if t == "" {
					t = "string"
				}
				c.Query = append(c.Query, QueryFlag{
					Name: p.Name, Type: t, Description: p.Description,
				})
			}
			cmds = append(cmds, c)
		}
	}

	sort.Slice(cmds, func(i, j int) bool {
		if cmds[i].Group != cmds[j].Group {
			return cmds[i].Group < cmds[j].Group
		}
		return cmds[i].Verb < cmds[j].Verb
	})

	var b strings.Builder
	b.WriteString(`// Code generated from the Bachs OpenAPI spec. DO NOT EDIT.
//
// Regenerate with:
//	go run ./internal/resources/gen -spec openapi.json -out internal/resources/table.go

package resources

// Commands is every public API operation, grouped as the CLI exposes it.
var Commands = []Command{
`)
	for _, c := range cmds {
		fmt.Fprintf(&b, "\t{Group: %q, Verb: %q, Method: %q, Path: %q, Summary: %q, HasBody: %t",
			c.Group, c.Verb, c.Method, c.Path, c.Summary, c.HasBody)
		if len(c.PathParams) > 0 {
			fmt.Fprintf(&b, ", PathParams: []string{")
			for i, p := range c.PathParams {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, "%q", p)
			}
			b.WriteString("}")
		}
		if len(c.Query) > 0 {
			b.WriteString(", Query: []QueryFlag{")
			for i, q := range c.Query {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, "{Name: %q, Type: %q, Description: %q}",
					q.Name, q.Type, trim(q.Description))
			}
			b.WriteString("}")
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\n")

	if err := os.WriteFile(*outPath, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %d commands to %s\n", len(cmds), *outPath)
}

func trim(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 90 {
		s = s[:90] + "…"
	}
	return strings.TrimSpace(s)
}
