package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type schemaTestNested struct {
	Inner string `json:"inner,omitempty" schema:"enum=x|y"`
}

type schemaTestArgs struct {
	Mode    string            `json:"mode,omitempty" schema:"enum=fast| slow |fast|"`
	A       *string           `json:"a,omitempty"`
	B       *string           `json:"b,omitempty"`
	C       *string           `json:"c,omitempty" schema:"AtMostOne=a|b|c; atleastone=a|b|c"`
	Nested  *schemaTestNested `json:"nested,omitempty"`
	Skipped string            `json:"-" schema:"enum=never"`
}

func connectSchemaTestServer(t *testing.T, server *sdkmcp.Server) *sdkmcp.ClientSession {
	t.Helper()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "client"}, nil)
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	_, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	session, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func listedInputSchema(t *testing.T, session *sdkmcp.ClientSession, name string) *jsonschema.Schema {
	t.Helper()
	listed, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	for _, tool := range listed.Tools {
		if tool.Name != name {
			continue
		}
		raw, err := json.Marshal(tool.InputSchema)
		require.NoError(t, err)
		var schema jsonschema.Schema
		require.NoError(t, json.Unmarshal(raw, &schema))
		return &schema
	}
	t.Fatalf("tool %q not listed", name)
	return nil
}

func TestAddToolInfersSchemaAndEnforcesDirectives(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test"}, nil)
	var got schemaTestArgs
	AddTool(server, &sdkmcp.Tool{Name: "schema_probe"}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args schemaTestArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		got = args
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "mode=" + args.Mode}}}, struct{}{}, nil
	})
	session := connectSchemaTestServer(t, server)

	schema := listedInputSchema(t, session, "schema_probe")
	require.Contains(t, schema.Properties, "mode")
	assert.Equal(t, []any{"fast", "slow"}, schema.Properties["mode"].Enum)
	assert.NotContains(t, schema.Properties, "Skipped")
	// Directives apply only to the top-level struct, not nested fields.
	require.Contains(t, schema.Properties, "nested")
	assert.Nil(t, schema.Properties["nested"].Properties["inner"].Enum)

	var notPairs [][]string
	var anyOf [][]string
	for _, sub := range schema.AllOf {
		if sub.Not != nil {
			notPairs = append(notPairs, sub.Not.Required)
		}
		for _, alt := range sub.AnyOf {
			anyOf = append(anyOf, alt.Required)
		}
	}
	assert.ElementsMatch(t, [][]string{{"a", "b"}, {"a", "c"}, {"b", "c"}}, notPairs)
	assert.Equal(t, [][]string{{"a"}, {"b"}, {"c"}}, anyOf)

	ctx := context.Background()
	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "schema_probe", Arguments: map[string]any{"mode": "slow", "a": "1"}})
	require.NoError(t, err)
	require.False(t, res.IsError, "%v", res.Content)
	require.Len(t, res.Content, 1)
	assert.Equal(t, "mode=slow", res.Content[0].(*sdkmcp.TextContent).Text)
	assert.Equal(t, "slow", got.Mode)
	require.NotNil(t, got.A)
	assert.Equal(t, "1", *got.A)

	for name, args := range map[string]map[string]any{
		"enum violation": {"mode": "medium", "a": "1"},
		"two exclusive":  {"a": "1", "b": "2"},
		"none required":  {"mode": "fast"},
	} {
		t.Run(name, func(t *testing.T) {
			got = schemaTestArgs{}
			res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "schema_probe", Arguments: args})
			if err == nil {
				assert.True(t, res.IsError, "expected schema validation to reject %v", args)
			}
			assert.Equal(t, schemaTestArgs{}, got, "handler must not run")
		})
	}
}

func TestAddToolKeepsExplicitInputSchema(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test"}, nil)
	explicit := &jsonschema.Schema{
		Type:       "object",
		Properties: map[string]*jsonschema.Schema{"mode": {Type: "string", Description: "explicit"}},
	}
	tool := &sdkmcp.Tool{Name: "explicit_probe", InputSchema: explicit}
	AddTool(server, tool, func(context.Context, *sdkmcp.CallToolRequest, schemaTestArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return &sdkmcp.CallToolResult{}, struct{}{}, nil
	})
	assert.Same(t, explicit, tool.InputSchema)
	session := connectSchemaTestServer(t, server)
	schema := listedInputSchema(t, session, "explicit_probe")
	assert.Equal(t, "explicit", schema.Properties["mode"].Description)
	assert.Nil(t, schema.Properties["mode"].Enum)
	assert.Empty(t, schema.AllOf)
}

func TestInferredInputSchemaDereferencesPointerArgs(t *testing.T) {
	schema, ok := inferredInputSchema[**schemaTestArgs]().(*jsonschema.Schema)
	require.True(t, ok)
	require.Contains(t, schema.Properties, "mode")
	assert.Equal(t, []any{"fast", "slow"}, schema.Properties["mode"].Enum)
	assert.Len(t, schema.AllOf, 4)
}

func TestInferredInputSchemaReturnsNilForUnsupportedType(t *testing.T) {
	assert.Nil(t, inferredInputSchema[chan int]())
	assert.Nil(t, inferredInputSchema[func()]())
}

func TestApplySchemaTagsIgnoresNonStructAndNilRoot(t *testing.T) {
	root := &jsonschema.Schema{Type: "string"}
	applySchemaTags(reflect.TypeFor[string](), root)
	assert.Equal(t, &jsonschema.Schema{Type: "string"}, root)
	assert.NotPanics(t, func() { applySchemaTags(reflect.TypeFor[schemaTestArgs](), nil) })
}

func TestApplySchemaTagsIgnoresMalformedAndUnknownDirectives(t *testing.T) {
	type args struct {
		Untagged  string `json:"untagged,omitempty"`
		Blank     string `json:"blank,omitempty" schema:"   "`
		NoEquals  string `json:"no_equals,omitempty" schema:";enum; ;"`
		Unknown   string `json:"unknown,omitempty" schema:"pattern=^x$;min=3"`
		EmptyAny  string `json:"empty_any,omitempty" schema:"atleastone= | ;atmostone=solo"`
		NoJSONTag string `schema:"enum=p|q"`
	}
	schema, ok := inferredInputSchema[args]().(*jsonschema.Schema)
	require.True(t, ok)
	for _, name := range []string{"untagged", "blank", "no_equals", "unknown", "empty_any"} {
		require.Contains(t, schema.Properties, name)
		assert.Nil(t, schema.Properties[name].Enum, name)
		assert.Empty(t, schema.Properties[name].Pattern, name)
	}
	// A field without a json tag is named after the Go field.
	require.Contains(t, schema.Properties, "NoJSONTag")
	assert.Equal(t, []any{"p", "q"}, schema.Properties["NoJSONTag"].Enum)
	// atleastone with no names and atmostone with a single name add nothing.
	assert.Empty(t, schema.AllOf)
}

func TestApplySchemaTagsUsesJSONNameWithoutTagOptions(t *testing.T) {
	type args struct {
		Mode  string `json:"mode" schema:"enum=a|b"`
		Empty string `json:",omitempty" schema:"enum=c|d"`
	}
	schema, ok := inferredInputSchema[args]().(*jsonschema.Schema)
	require.True(t, ok)
	require.Contains(t, schema.Properties, "mode")
	assert.Equal(t, []any{"a", "b"}, schema.Properties["mode"].Enum)
	assert.NotContains(t, schema.Properties, "Mode")
	// An empty json name still falls back to the Go field name.
	require.Contains(t, schema.Properties, "Empty")
	assert.Equal(t, []any{"c", "d"}, schema.Properties["Empty"].Enum)
}

func TestApplySchemaDirectiveEnumNeedsExistingProperty(t *testing.T) {
	root := &jsonschema.Schema{}
	applySchemaDirective(root, "mode", "enum", "a|b")
	assert.Nil(t, root.Properties)

	root.Properties = map[string]*jsonschema.Schema{"other": {}}
	applySchemaDirective(root, "mode", "enum", "a|b")
	applySchemaDirective(root, "", "enum", "a|b")
	assert.Equal(t, map[string]*jsonschema.Schema{"other": {}}, root.Properties)

	applySchemaDirective(root, "other", "enum", "a|b|a")
	assert.Equal(t, []any{"a", "b"}, root.Properties["other"].Enum)
}

func TestParseSchemaTag(t *testing.T) {
	assert.Equal(t, map[string]string{
		"enum":      "a|b",
		"atmostone": "x|y",
	}, parseSchemaTag(" Enum = a|b ;; bare ; ATMOSTONE=x|y ;"))
	assert.Empty(t, parseSchemaTag(";;"))
	assert.Equal(t, map[string]string{"enum": "b"}, parseSchemaTag("enum=a;enum=b"))
}
