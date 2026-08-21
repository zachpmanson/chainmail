package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// api/openapi.json is the contract a client generates types from, so it is a
// test input here rather than documentation: every handler test asserts its
// response against the schema the client will trust. A field the server emits
// and the document does not declare fails, which is the direction that matters
// — a client cannot use what it was never told about, but it WILL trust an
// absent-able field the document forgot to mark absent-able.

const openapiPath = "../../api/openapi.json"

type apiDoc struct {
	schemas map[string]any
}

func loadAPI(t *testing.T) *apiDoc {
	t.Helper()
	blob, err := os.ReadFile(openapiPath)
	if err != nil {
		t.Fatalf("reading the contract: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(blob, &doc); err != nil {
		t.Fatalf("parsing the contract: %v", err)
	}
	comps, _ := doc["components"].(map[string]any)
	schemas, _ := comps["schemas"].(map[string]any)
	if len(schemas) == 0 {
		t.Fatal("the contract declares no schemas")
	}
	return &apiDoc{schemas: schemas}
}

// assert checks a decoded response body against a named component schema.
func (d *apiDoc) assert(t *testing.T, name string, body []byte) {
	t.Helper()
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("%s: response is not JSON: %v", name, err)
	}
	s, ok := d.schemas[name]
	if !ok {
		t.Fatalf("the contract has no schema named %s", name)
	}
	for _, p := range d.violations(s.(map[string]any), v, name) {
		t.Error(p)
	}
}

// violations walks value against schema and reports every disagreement, rather
// than the first: one missing field and one undocumented one are two separate
// facts about the handler.
func (d *apiDoc) violations(schema map[string]any, v any, path string) []string {
	var out []string
	if ref, ok := schema["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		target, ok := d.schemas[name].(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: unresolvable $ref %s", path, ref)}
		}
		return d.violations(target, v, path)
	}
	if alts, ok := schema["oneOf"].([]any); ok {
		for _, alt := range alts {
			if len(d.violations(alt.(map[string]any), v, path)) == 0 {
				return nil
			}
		}
		return []string{fmt.Sprintf("%s: matches none of the documented alternatives", path)}
	}
	switch schema["type"] {
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: want an object, got %T", path, v)}
		}
		props, _ := schema["properties"].(map[string]any)
		for _, r := range strList(schema["required"]) {
			if _, present := m[r]; !present {
				out = append(out, fmt.Sprintf("%s: required field %q is missing", path, r))
			}
		}
		extra, closed := schema["additionalProperties"]
		for _, k := range sortedKeys(m) {
			sub, documented := props[k]
			if documented {
				out = append(out, d.violations(sub.(map[string]any), m[k], path+"."+k)...)
				continue
			}
			if closed {
				if allow, isBool := extra.(bool); isBool && !allow {
					out = append(out, fmt.Sprintf("%s: field %q is not in the contract", path, k))
					continue
				}
				if sch, isSchema := extra.(map[string]any); isSchema {
					out = append(out, d.violations(sch, m[k], path+"."+k)...)
					continue
				}
			}
			out = append(out, fmt.Sprintf("%s: field %q is not in the contract", path, k))
		}
	case "array":
		vs, ok := v.([]any)
		if !ok {
			return []string{fmt.Sprintf("%s: want an array, got %T", path, v)}
		}
		items, _ := schema["items"].(map[string]any)
		if items == nil {
			return nil
		}
		for i, e := range vs {
			out = append(out, d.violations(items, e, fmt.Sprintf("%s[%d]", path, i))...)
		}
	case "string":
		s, ok := v.(string)
		if !ok {
			return []string{fmt.Sprintf("%s: want a string, got %T", path, v)}
		}
		if allowed := strList(schema["enum"]); len(allowed) > 0 && !contains(allowed, s) {
			out = append(out, fmt.Sprintf("%s: %q is not one of %v", path, s, allowed))
		}
	case "integer":
		f, ok := v.(float64)
		if !ok || f != float64(int64(f)) {
			return []string{fmt.Sprintf("%s: want an integer, got %v", path, v)}
		}
	case "number":
		if _, ok := v.(float64); !ok {
			return []string{fmt.Sprintf("%s: want a number, got %T", path, v)}
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return []string{fmt.Sprintf("%s: want a boolean, got %T", path, v)}
		}
	case nil:
		// An untyped schema (an enum-only one, say) constrains nothing walkable.
		if allowed := strList(schema["enum"]); len(allowed) > 0 {
			if s, ok := v.(string); ok && !contains(allowed, s) {
				out = append(out, fmt.Sprintf("%s: %q is not one of %v", path, s, allowed))
			}
		}
	}
	return out
}

// TestTheInlinedTimelineSchemaHasNotDrifted is why the spec response can be
// asserted against the contract at all: schema/timeline.schema.json is already
// published and the renderer validates against it, so the copy in the contract
// has to be that file and not a paraphrase of it. Two things legitimately
// differ — the document-level $schema/$id, meaningless inside a component, and
// the ref target, which must point at where the component lives — and this
// reverses both.
func TestTheInlinedTimelineSchemaHasNotDrifted(t *testing.T) {
	d := loadAPI(t)
	blob, err := os.ReadFile("../../schema/timeline.schema.json")
	if err != nil {
		t.Fatalf("reading the published schema: %v", err)
	}
	var published map[string]any
	if err := json.Unmarshal(blob, &published); err != nil {
		t.Fatal(err)
	}
	delete(published, "$schema")
	delete(published, "$id")
	defs, _ := published["definitions"].(map[string]any)
	entry := defs["Entry"]
	delete(published, "definitions")
	msgs := published["properties"].(map[string]any)["messages"].(map[string]any)
	msgs["items"] = map[string]any{"$ref": "#/components/schemas/TimelineEntry"}

	if !reflect.DeepEqual(published, d.schemas["TimelineSpec"]) {
		t.Error("components.schemas.TimelineSpec is no longer schema/timeline.schema.json — " +
			"regenerate it rather than editing one side")
	}
	if !reflect.DeepEqual(entry, d.schemas["TimelineEntry"]) {
		t.Error("components.schemas.TimelineEntry is no longer the published Entry definition")
	}
}

// The rule the contract states in prose, checked: an Entry cannot quietly gain
// a field, so an endpoint cannot smuggle one onto a page.
func TestTheTimelineEntryStaysClosed(t *testing.T) {
	d := loadAPI(t)
	entry, ok := d.schemas["TimelineEntry"].(map[string]any)
	if !ok {
		t.Fatal("the contract has no TimelineEntry")
	}
	if allow, isBool := entry["additionalProperties"].(bool); !isBool || allow {
		t.Error("TimelineEntry must be additionalProperties: false")
	}
}

// Every documented path must be routed, and nothing may answer that is not
// documented. The catch-all makes the second half checkable: an undocumented
// path answers 404 rather than 200.
func TestEveryDocumentedPathIsServed(t *testing.T) {
	blob, err := os.ReadFile(openapiPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(blob, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Paths) != 6 {
		t.Errorf("the contract declares %d paths; the handler table lists 6", len(doc.Paths))
	}
	srv := testServer(t)
	for path, ops := range doc.Paths {
		for verb := range ops {
			// Path parameters are filled with the fixture's own ids, so a routed
			// path answers 200 and an unrouted one 404 for the right reason.
			concrete := strings.NewReplacer(
				"{extId}", extAda1, "{rootExtId}", extAda1).Replace(path)
			var res *response
			if verb == "post" {
				res = srv.do(t, "POST", concrete, specBody(extAda1))
			} else {
				res = srv.do(t, "GET", concrete+"?q=cutover", nil)
			}
			if res.status == 404 {
				t.Errorf("%s %s is documented but not routed", verb, path)
			}
		}
	}
}

func strList(v any) []string {
	vs, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(vs))
	for _, e := range vs {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
