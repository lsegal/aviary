package config

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fieldPaths(fields []FieldMeta) []string {
	paths := make([]string, len(fields))
	for i, f := range fields {
		paths[i] = f.Path
	}
	return paths
}

func findField(t *testing.T, fields []FieldMeta, path string) FieldMeta {
	t.Helper()
	for _, f := range fields {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("field %q not found in %v", path, fieldPaths(fields))
	return FieldMeta{}
}

func TestSectionFieldsServer(t *testing.T) {
	fields := SectionFields("server")
	require.NotEmpty(t, fields)

	port := findField(t, fields, "server.port")
	assert.Equal(t, FieldMeta{
		Path:        "server.port",
		Label:       "Port",
		Placeholder: "16677",
		Description: "HTTP(S) listen port. Defaults to 16677.",
		Default:     "16677",
		Type:        "int",
		Section:     "server",
		Order:       1,
	}, port)

	assert.Equal(t, "bool", findField(t, fields, "server.external_access").Type)
	autoUpdate := findField(t, fields, "server.auto_update")
	assert.Equal(t, "false", autoUpdate.Default)

	cert := findField(t, fields, "server.tls.cert")
	assert.Equal(t, "string", cert.Type)
	assert.Equal(t, "/path/to/cert.pem", cert.Placeholder)
	assert.Empty(t, cert.Default)

	for i := 1; i < len(fields); i++ {
		prev, cur := fields[i-1], fields[i]
		assert.Equal(t, "server", cur.Section)
		assert.True(t, prev.Order < cur.Order || (prev.Order == cur.Order && prev.Path < cur.Path),
			"fields out of order: %s (%d) before %s (%d)", prev.Path, prev.Order, cur.Path, cur.Order)
	}
}

func TestSectionFieldsSchedulerOneOfIsString(t *testing.T) {
	fields := SectionFields("scheduler")
	concurrency := findField(t, fields, "scheduler.concurrency")
	assert.Equal(t, "string", concurrency.Type)
	assert.Equal(t, "auto", concurrency.Placeholder)
}

func TestSectionFieldsUnknownSection(t *testing.T) {
	assert.Empty(t, SectionFields("nope"))
	assert.Empty(t, SectionFields("skills"))
}

func TestSectionFieldsGeneralOrdersBySectionThenOrder(t *testing.T) {
	all := SectionFields("general")

	total := 0
	for _, section := range []string{"server", "browser", "scheduler", "search", "models"} {
		total += len(SectionFields(section))
	}
	require.Len(t, all, total)

	for i := 1; i < len(all); i++ {
		prev, cur := all[i-1], all[i]
		po, co := sectionOrder(prev.Section), sectionOrder(cur.Section)
		assert.True(t, po < co || (po == co && prev.Order <= cur.Order),
			"general fields out of order: %s before %s", prev.Path, cur.Path)
	}
	assert.Equal(t, "server", all[0].Section)
}

func TestSectionOrder(t *testing.T) {
	for section, want := range map[string]int{
		"server": 0, "browser": 1, "scheduler": 2, "search": 3, "models": 4, "other": 99,
	} {
		assert.Equal(t, want, sectionOrder(section), section)
	}
}

func TestEveryFieldRoundTrips(t *testing.T) {
	samples := map[string]string{"string": "sample", "int": "42", "bool": "true"}
	for _, f := range SectionFields("general") {
		t.Run(f.Path, func(t *testing.T) {
			cfg := &Config{}
			require.NoError(t, SetField(cfg, f.Path, samples[f.Type]))
			want := samples[f.Type]
			if f.Path == "scheduler.concurrency" {
				// Non-numeric concurrency is stored as the raw string.
				want = "sample"
			}
			assert.Equal(t, want, GetField(cfg, f.Path))
		})
	}
}

func TestGetField(t *testing.T) {
	yes := true
	cfg := &Config{
		Server: ServerConfig{
			Port:           8080,
			ExternalAccess: true,
			TLS:            &TLSConfig{Cert: "/c.pem"},
			AutoUpdate:     &yes,
		},
		Browser:   BrowserConfig{Binary: "chrome"},
		Scheduler: SchedulerConfig{Concurrency: 4},
	}
	cases := map[string]string{
		"server.port":            "8080",
		"server.external_access": "true",
		"server.no_tls":          "false",
		"server.tls.cert":        "/c.pem",
		"server.tls.key":         "",
		"server.auto_update":     "true",
		"browser.binary":         "chrome",
		"browser.cdp_port":       "",
		"browser.reuse_tabs":     "",
		"scheduler.concurrency":  "4",
		"server.missing":         "",
		"server.port.extra":      "",
		"agents.name":            "",
		"models.providers":       "",
	}
	for path, want := range cases {
		assert.Equal(t, want, GetField(cfg, path), path)
	}

	assert.Empty(t, GetField(&Config{}, "server.tls.cert"), "nil pointer parent")
	assert.Empty(t, GetField(&Config{}, "scheduler.concurrency"), "nil interface")
	assert.Equal(t, "auto", GetField(&Config{Scheduler: SchedulerConfig{Concurrency: "auto"}}, "scheduler.concurrency"))
}

func TestGetReflectFieldThroughInterface(t *testing.T) {
	type inner struct {
		Name string `yaml:"name"`
	}
	type outer struct {
		Any any `yaml:"any"`
	}
	assert.Equal(t, "x", getReflectField(reflect.ValueOf(outer{Any: inner{Name: "x"}}), []string{"any", "name"}, 0))
	assert.Empty(t, getReflectField(reflect.ValueOf(outer{}), []string{"any", "name"}, 0))
}

func TestReflectToString(t *testing.T) {
	n := 7
	var nilInt *int
	var nilAny any
	assert.Equal(t, "7", reflectToString(reflect.ValueOf(&n)))
	assert.Empty(t, reflectToString(reflect.ValueOf(nilInt)))
	assert.Empty(t, reflectToString(reflect.ValueOf(&nilAny).Elem()))
	assert.Empty(t, reflectToString(reflect.ValueOf(1.5)), "unsupported kinds render empty")
	assert.Empty(t, reflectToString(reflect.ValueOf(int8(0))))
	assert.Equal(t, "-3", reflectToString(reflect.ValueOf(int64(-3))))
}

func TestSetFieldScalars(t *testing.T) {
	cfg := &Config{}
	require.NoError(t, SetField(cfg, "server.port", " 9000 "))
	assert.Equal(t, 9000, cfg.Server.Port)
	require.NoError(t, SetField(cfg, "server.port", ""))
	assert.Equal(t, 0, cfg.Server.Port)

	require.NoError(t, SetField(cfg, "browser.binary", "  /bin/chrome  "))
	assert.Equal(t, "/bin/chrome", cfg.Browser.Binary)

	for _, v := range []string{"true", "on", "yes", "1"} {
		cfg.Browser.Headless = false
		require.NoError(t, SetField(cfg, "browser.headless", v))
		assert.True(t, cfg.Browser.Headless, v)
	}
	for _, v := range []string{"false", "off", "no", "0", ""} {
		cfg.Browser.Headless = true
		require.NoError(t, SetField(cfg, "browser.headless", v))
		assert.False(t, cfg.Browser.Headless, v)
	}
}

func TestSetFieldInitializesPointers(t *testing.T) {
	cfg := &Config{}
	require.NoError(t, SetField(cfg, "server.tls.key", "/k.pem"))
	require.NotNil(t, cfg.Server.TLS)
	assert.Equal(t, "/k.pem", cfg.Server.TLS.Key)

	require.NoError(t, SetField(cfg, "browser.reuse_tabs", "false"))
	require.NotNil(t, cfg.Browser.ReuseTabs)
	assert.False(t, *cfg.Browser.ReuseTabs)
	assert.Equal(t, "false", GetField(cfg, "browser.reuse_tabs"))

	require.NoError(t, SetField(cfg, "server.auto_update", "yes"))
	require.NotNil(t, cfg.Server.AutoUpdate)
	assert.True(t, *cfg.Server.AutoUpdate)

	require.EqualError(t, SetField(cfg, "server.auto_update", "maybe"), `expected boolean, got "maybe"`)
	assert.True(t, *cfg.Server.AutoUpdate, "failed set keeps the previous value")

	require.NoError(t, SetField(cfg, "server.auto_update", ""))
	assert.Nil(t, cfg.Server.AutoUpdate, "empty value clears an optional field")
}

func TestSetFieldConcurrency(t *testing.T) {
	cfg := &Config{}
	require.NoError(t, SetField(cfg, "scheduler.concurrency", "8"))
	assert.Equal(t, 8, cfg.Scheduler.Concurrency)

	require.NoError(t, SetField(cfg, "scheduler.concurrency", "auto"))
	assert.Nil(t, cfg.Scheduler.Concurrency)

	cfg.Scheduler.Concurrency = 3
	require.NoError(t, SetField(cfg, "scheduler.concurrency", ""))
	assert.Nil(t, cfg.Scheduler.Concurrency)

	require.NoError(t, SetField(cfg, "scheduler.concurrency", "many"))
	assert.Equal(t, "many", cfg.Scheduler.Concurrency)
}

func TestSetFieldErrors(t *testing.T) {
	cases := []struct {
		path, value, err string
	}{
		{"server.port", "abc", `expected integer, got "abc"`},
		{"browser.headless", "maybe", `expected boolean, got "maybe"`},
		{"server.missing", "x", `field "missing" not found in config.ServerConfig`},
		{"bogus", "x", `field "bogus" not found in config.Config`},
		{"server.port.extra", "1", `cannot traverse int for "extra"`},
		{"agents", "x", "unsupported field kind slice"},
		{"models.providers.openai", "x", `cannot traverse map for "openai"`},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			cfg := &Config{Server: ServerConfig{Port: 5}}
			err := SetField(cfg, tc.path, tc.value)
			require.EqualError(t, err, tc.err)
			assert.Equal(t, 5, cfg.Server.Port, "failed set must not change other fields")
		})
	}
}

func TestParseSchemaFields(t *testing.T) {
	assert.Nil(t, parseSchemaFields([]byte("{not json")))

	data := []byte(`{
		"properties": {
			"server": {
				"properties": {
					"b": {"type": "boolean", "x-label": "B", "x-order": 2, "default": true},
					"a": {"type": "integer", "x-label": "A", "x-order": 2, "default": 3},
					"s": {"type": "string", "x-label": "S", "x-order": 1, "default": "hi"},
					"arr": {"type": "array", "x-label": "Ignored"},
					"multi": {"type": ["string", "null"], "x-label": "Ignored too"},
					"nolabel": {"type": "string"},
					"nested": {"properties": {"o": {"oneOf": [{"type": "string"}], "x-label": "O", "x-order": 0, "default": 1.5}}}
				}
			},
			"browser": {"properties": {"nolabel": {"type": "string"}}},
			"other": {"properties": {"x": {"type": "string", "x-label": "X"}}}
		}
	}`)
	got := parseSchemaFields(data)

	require.Contains(t, got, "server")
	assert.NotContains(t, got, "browser", "sections without labelled fields are dropped")
	assert.NotContains(t, got, "other", "only known sections are parsed")
	assert.NotContains(t, got, "scheduler", "missing sections are skipped")

	server := got["server"]
	assert.Equal(t, []string{"server.nested.o", "server.s", "server.a", "server.b"}, fieldPaths(server))
	assert.Equal(t, "1", server[0].Default)
	assert.Equal(t, "string", server[0].Type)
	assert.Equal(t, "hi", server[1].Default)
	assert.Equal(t, "3", server[2].Default)
	assert.Equal(t, "int", server[2].Type)
	assert.Equal(t, "true", server[3].Default)
	assert.Equal(t, "bool", server[3].Type)
}

func TestExtractFieldsNil(t *testing.T) {
	assert.Nil(t, extractFields(nil, "server", "server"))
}

func TestSchemaDefault(t *testing.T) {
	assert.Empty(t, schemaDefault(nil))
	assert.Equal(t, "x", schemaDefault("x"))
	assert.Equal(t, "false", schemaDefault(false))
	assert.Equal(t, "12", schemaDefault(float64(12)))
	assert.Equal(t, "[a]", schemaDefault([]any{"a"}))
}
