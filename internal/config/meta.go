package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// FieldMeta describes a single configurable scalar field extracted from the
// JSON schema. Fields are identified by a dot-path that matches the yaml struct
// tags on Config (e.g. "server.port", "browser.headless").
type FieldMeta struct {
	Path        string // dot-path, e.g. "server.tls.cert"
	Label       string // x-label from schema — short display name
	Placeholder string // x-placeholder — input hint / example value
	Description string // description — shown as help text
	Default     string // schema default rendered as a string for form hydration
	Type        string // "string" | "int" | "bool"
	Section     string // top-level config key: "server", "browser", etc.
	Order       int    // x-order within section; lower = first
}

// SectionFields returns the ordered FieldMeta slice for a config section.
// Use one of the top-level keys ("server", "browser", "scheduler", "search",
// "models") to get section-specific fields, or "general" to get all fields
// from every section in a stable section+order sequence.
func SectionFields(section string) []FieldMeta {
	ensureMeta()
	if section == "general" {
		var all []FieldMeta
		for _, fields := range cachedSectionFields {
			all = append(all, fields...)
		}
		sort.Slice(all, func(i, j int) bool {
			oi := sectionOrder(all[i].Section)
			oj := sectionOrder(all[j].Section)
			if oi != oj {
				return oi < oj
			}
			return all[i].Order < all[j].Order
		})
		return all
	}
	return cachedSectionFields[section]
}

func sectionOrder(section string) int {
	switch section {
	case "server":
		return 0
	case "browser":
		return 1
	case "scheduler":
		return 2
	case "search":
		return 3
	case "models":
		return 4
	}
	return 99
}

// GetField reads a dot-path value from cfg and returns its string
// representation. Returns "" for unset or zero fields.
func GetField(cfg *Config, path string) string {
	v := reflect.ValueOf(cfg).Elem()
	return getReflectField(v, strings.Split(path, "."), 0)
}

// SetField writes a dot-path field in cfg from the string value.
// Pointer fields are auto-initialized when non-empty values are set.
// Returns an error when the type conversion fails.
func SetField(cfg *Config, path, value string) error {
	v := reflect.ValueOf(cfg).Elem()
	return setReflectField(v, strings.Split(path, "."), 0, strings.TrimSpace(value))
}

// ── reflection helpers ────────────────────────────────────────────────────────

func getReflectField(v reflect.Value, parts []string, idx int) string {
	if idx >= len(parts) {
		return reflectToString(v)
	}
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return ""
		}
		return getReflectField(v.Elem(), parts, idx)
	case reflect.Struct:
		t := v.Type()
		for i := range t.NumField() {
			tag := strings.Split(t.Field(i).Tag.Get("yaml"), ",")[0]
			if tag == parts[idx] {
				return getReflectField(v.Field(i), parts, idx+1)
			}
		}
	case reflect.Interface:
		if v.IsNil() {
			return ""
		}
		return getReflectField(v.Elem(), parts, idx)
	}
	return ""
}

func reflectToString(v reflect.Value) string {
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.Int() == 0 {
			return ""
		}
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Ptr:
		if v.IsNil() {
			return ""
		}
		return reflectToString(v.Elem())
	case reflect.Interface:
		if v.IsNil() {
			return ""
		}
		return fmt.Sprintf("%v", v.Interface())
	}
	return ""
}

func setReflectField(v reflect.Value, parts []string, idx int, value string) error {
	if idx >= len(parts) {
		return setReflectValue(v, value)
	}
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		return setReflectField(v.Elem(), parts, idx, value)
	case reflect.Struct:
		t := v.Type()
		for i := range t.NumField() {
			tag := strings.Split(t.Field(i).Tag.Get("yaml"), ",")[0]
			if tag == parts[idx] {
				return setReflectField(v.Field(i), parts, idx+1, value)
			}
		}
		return fmt.Errorf("field %q not found in %s", parts[idx], v.Type())
	}
	return fmt.Errorf("cannot traverse %s for %q", v.Kind(), parts[idx])
}

func setReflectValue(v reflect.Value, value string) error {
	switch v.Kind() {
	case reflect.String:
		v.SetString(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if value == "" {
			v.SetInt(0)
			return nil
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("expected integer, got %q", value)
		}
		v.SetInt(n)
	case reflect.Bool:
		switch value {
		case "true", "on", "yes", "1":
			v.SetBool(true)
		case "false", "off", "no", "0", "":
			v.SetBool(false)
		default:
			return fmt.Errorf("expected boolean, got %q", value)
		}
	case reflect.Interface:
		// scheduler.concurrency: any = "auto" | integer
		// scheduler.precompute_tasks: boolean
		if value == "" || value == "auto" {
			v.Set(reflect.Zero(v.Type()))
		} else if n, err := strconv.Atoi(value); err == nil {
			v.Set(reflect.ValueOf(n))
		} else {
			v.Set(reflect.ValueOf(value))
		}
	default:
		return fmt.Errorf("unsupported field kind %s", v.Kind())
	}
	return nil
}

// ── schema parsing ────────────────────────────────────────────────────────────

var cachedSectionFields map[string][]FieldMeta
var metaInit bool

func ensureMeta() {
	if metaInit {
		return
	}
	metaInit = true
	cachedSectionFields = parseSchemaFields()
}

// schemaNode is a minimal JSON schema object for metadata extraction.
type schemaNode struct {
	Type         any                    `json:"type"`
	Default      any                    `json:"default"`
	Properties   map[string]*schemaNode `json:"properties"`
	Description  string                 `json:"description"`
	OneOf        []*schemaNode          `json:"oneOf"`
	XLabel       string                 `json:"x-label"`
	XPlaceholder string                 `json:"x-placeholder"`
	XOrder       int                    `json:"x-order"`
}

func parseSchemaFields() map[string][]FieldMeta {
	var root schemaNode
	if err := json.Unmarshal(schemaJSON, &root); err != nil {
		return nil
	}
	sections := []string{"server", "browser", "scheduler", "search", "models"}
	result := make(map[string][]FieldMeta, len(sections))
	for _, section := range sections {
		node := root.Properties[section]
		if node == nil {
			continue
		}
		fields := extractFields(node, section, section)
		if len(fields) > 0 {
			result[section] = fields
		}
	}
	return result
}

func extractFields(node *schemaNode, path, section string) []FieldMeta {
	if node == nil {
		return nil
	}
	// Leaf: a scalar with an x-label becomes a form field.
	if node.XLabel != "" {
		ft := schemaType(node)
		if ft != "" {
			return []FieldMeta{{
				Path:        path,
				Label:       node.XLabel,
				Placeholder: node.XPlaceholder,
				Description: node.Description,
				Default:     schemaDefault(node.Default),
				Type:        ft,
				Section:     section,
				Order:       node.XOrder,
			}}
		}
	}
	// Non-leaf: recurse into child properties.
	var fields []FieldMeta
	for key, child := range node.Properties {
		fields = append(fields, extractFields(child, path+"."+key, section)...)
	}
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].Order != fields[j].Order {
			return fields[i].Order < fields[j].Order
		}
		return fields[i].Path < fields[j].Path
	})
	return fields
}

func schemaType(node *schemaNode) string {
	switch t := node.Type.(type) {
	case string:
		switch t {
		case "string":
			return "string"
		case "integer":
			return "int"
		case "boolean":
			return "bool"
		}
	}
	// oneOf (e.g. scheduler.concurrency: string | integer) → treat as string.
	if len(node.OneOf) > 0 {
		return "string"
	}
	return ""
}

func schemaDefault(v any) string {
	switch d := v.(type) {
	case nil:
		return ""
	case string:
		return d
	case bool:
		return strconv.FormatBool(d)
	case float64:
		return strconv.FormatInt(int64(d), 10)
	default:
		return fmt.Sprintf("%v", d)
	}
}
