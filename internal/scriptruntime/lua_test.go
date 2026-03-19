package scriptruntime

import (
	"context"
	"testing"
)

func TestRunLua_JSONEncode(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		script string
		want   string
	}{
		{
			name:   "encode string",
			script: `print(json.encode("hello"))`,
			want:   `"hello"`,
		},
		{
			name:   "encode number",
			script: `print(json.encode(42))`,
			want:   `42`,
		},
		{
			name:   "encode boolean true",
			script: `print(json.encode(true))`,
			want:   `true`,
		},
		{
			name:   "encode boolean false",
			script: `print(json.encode(false))`,
			want:   `false`,
		},
		{
			name:   "encode nil",
			script: `print(json.encode(nil))`,
			want:   `null`,
		},
		{
			name:   "encode array table",
			script: `print(json.encode({1, 2, 3}))`,
			want:   `[1,2,3]`,
		},
		{
			name:   "encode object table",
			script: `print(json.encode({name="alice", age=30}))`,
			// map iteration order is non-deterministic, so we test via decode round-trip instead
			want:   ``, // skipped below
		},
		{
			name:   "encode nested structure",
			script: `print(json.encode({tags={"a","b"}, count=2}))`,
			want:   ``, // skipped below — map order non-deterministic
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.want == "" {
				t.Skip("non-deterministic output, covered by round-trip tests")
			}
			got, err := RunLua(ctx, tt.script, Options{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunLua_JSONDecode(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		script string
		want   string
	}{
		{
			name:   "decode string",
			script: `print(json.decode('"hello"'))`,
			want:   `hello`,
		},
		{
			name:   "decode number",
			script: `print(json.decode('42'))`,
			want:   `42`,
		},
		{
			name:   "decode float",
			script: `print(json.decode('3.14'))`,
			want:   `3.14`,
		},
		{
			name:   "decode boolean true",
			script: `print(json.decode('true'))`,
			want:   `true`,
		},
		{
			name:   "decode boolean false",
			script: `print(json.decode('false'))`,
			want:   `false`,
		},
		{
			name:   "decode null",
			script: `print(json.decode('null'))`,
			want:   `nil`,
		},
		{
			name:   "decode array and access element",
			script: `local t = json.decode('[10,20,30]'); print(t[2])`,
			want:   `20`,
		},
		{
			name:   "decode array length",
			script: `local t = json.decode('[1,2,3]'); print(#t)`,
			want:   `3`,
		},
		{
			name:   "decode object field access",
			script: `local t = json.decode('{"name":"bob"}'); print(t.name)`,
			want:   `bob`,
		},
		{
			name:   "decode nested object",
			script: `local t = json.decode('{"user":{"age":25}}'); print(t.user.age)`,
			want:   `25`,
		},
		{
			name:   "decode invalid json errors",
			script: `json.decode('not json')`,
			want:   ``, // expect error, not output
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "decode invalid json errors" {
				_, err := RunLua(ctx, tt.script, Options{})
				if err == nil {
					t.Fatal("expected error for invalid JSON, got nil")
				}
				return
			}
			got, err := RunLua(ctx, tt.script, Options{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunLua_JSONRoundTrip(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		script string
		want   string
	}{
		{
			name: "object round-trip preserves fields",
			script: `
				local original = {x = 1, y = 2}
				local encoded = json.encode(original)
				local decoded = json.decode(encoded)
				print(decoded.x, decoded.y)
			`,
			want: "1\t2",
		},
		{
			name: "array round-trip preserves order",
			script: `
				local original = {"a", "b", "c"}
				local encoded = json.encode(original)
				local decoded = json.decode(encoded)
				print(decoded[1], decoded[2], decoded[3])
			`,
			want: "a\tb\tc",
		},
		{
			name: "nested round-trip",
			script: `
				local original = {items = {10, 20}, meta = {count = 2}}
				local encoded = json.encode(original)
				local decoded = json.decode(encoded)
				print(decoded.items[1], decoded.meta.count)
			`,
			want: "10\t2",
		},
		{
			name: "bool values survive round-trip",
			script: `
				local original = {active = true, deleted = false}
				local encoded = json.encode(original)
				local decoded = json.decode(encoded)
				print(decoded.active, decoded.deleted)
			`,
			want: "true\tfalse",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RunLua(ctx, tt.script, Options{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
