package mcp

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestParseDotEnv(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want map[string]string
	}{
		{"empty", "", map[string]string{}},
		{"blank and comment lines", "\n   \n# comment\n  # indented comment\nA=1\n", map[string]string{"A": "1"}},
		{"byte order mark on first line", "\uFEFFA=1", map[string]string{"A": "1"}},
		{"export prefix", "export KEY=value", map[string]string{"KEY": "value"}},
		{"export prefix with extra spaces", "export    KEY = value ", map[string]string{"KEY": "value"}},
		{"empty value", "KEY=", map[string]string{"KEY": ""}},
		{"value containing equals", "URL=a=b=c", map[string]string{"URL": "a=b=c"}},
		{"single quotes are literal", `KEY='a\nb "c" # d'`, map[string]string{"KEY": `a\nb "c" # d`}},
		{"double quotes process escapes", `KEY="a\nb\t\"c\" # d"`, map[string]string{"KEY": "a\nb\t\"c\" # d"}},
		{"empty quoted values", "A=''\nB=\"\"", map[string]string{"A": "", "B": ""}},
		{"inline comment after space", "KEY=value # comment", map[string]string{"KEY": "value"}},
		{"inline comment after tab", "KEY=value\t# comment", map[string]string{"KEY": "value"}},
		{"hash without preceding space is kept", "KEY=abc#def", map[string]string{"KEY": "abc#def"}},
		{"hash kept then later comment stripped", "KEY=a#b #c", map[string]string{"KEY": "a#b"}},
		{"value that is only a comment", "KEY=#comment", map[string]string{"KEY": ""}},
		{"later duplicate wins", "KEY=1\nKEY=2", map[string]string{"KEY": "2"}},
		{"CRLF line endings", "A=1\r\nB=2\r\n", map[string]string{"A": "1", "B": "2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDotEnv(tt.src)
			if err != nil {
				t.Fatalf("parseDotEnv(%q) error = %v", tt.src, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseDotEnv(%q) = %#v, want %#v", tt.src, got, tt.want)
			}
		})
	}
}

func TestParseDotEnvErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"missing equals", "A=1\nNOEQUALS", "dotenv line 2 missing '='"},
		{"empty key", "\n  =value", "dotenv line 2 has empty key"},
		{"unterminated single quote", "KEY='abc", "dotenv line 1: unterminated single-quoted value"},
		{"lone single quote", "KEY='", "dotenv line 1: unterminated single-quoted value"},
		{"unterminated double quote", `KEY="abc`, "dotenv line 1: unterminated double-quoted value"},
		{"lone double quote", `KEY="`, "dotenv line 1: unterminated double-quoted value"},
		{"invalid escape in double quotes", `KEY="\q"`, "dotenv line 1: invalid syntax"},
		{"quoted value followed by comment", `KEY="abc" # note`, "dotenv line 1: unterminated double-quoted value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDotEnv(tt.src)
			if err == nil {
				t.Fatalf("parseDotEnv(%q) = %#v, want error %q", tt.src, got, tt.want)
			}
			if err.Error() != tt.want {
				t.Fatalf("parseDotEnv(%q) error = %q, want %q", tt.src, err.Error(), tt.want)
			}
			if got != nil {
				t.Fatalf("parseDotEnv(%q) returned values %#v alongside error", tt.src, got)
			}
		})
	}
}

func TestParseDotEnvScannerError(t *testing.T) {
	// bufio.Scanner rejects lines longer than its 64 KiB default buffer.
	src := "KEY=" + strings.Repeat("x", 70*1024)
	got, err := parseDotEnv(src)
	if err == nil || !strings.Contains(err.Error(), "token too long") {
		t.Fatalf("parseDotEnv(long line) = %v, %v; want token too long error", got, err)
	}
}

func TestInlineCommentIndex(t *testing.T) {
	tests := []struct {
		raw  string
		want int
	}{
		{"", -1},
		{"value", -1},
		{"#all comment", 0},
		{"a #b", 2},
		{"a\t#b", 2},
		{"a#b", -1},
		{"a#b c #d", 6},
	}
	for _, tt := range tests {
		if got := inlineCommentIndex(tt.raw); got != tt.want {
			t.Errorf("inlineCommentIndex(%q) = %d, want %d", tt.raw, got, tt.want)
		}
	}
}

func TestSetEnvValue(t *testing.T) {
	t.Run("replaces existing key in place", func(t *testing.T) {
		env := []string{"A=1", "KEY=old", "B=2"}
		setEnvValue(&env, "KEY", "new")
		want := []string{"A=1", "KEY=new", "B=2"}
		if !reflect.DeepEqual(env, want) {
			t.Fatalf("env = %#v, want %#v", env, want)
		}
	})

	t.Run("appends new key", func(t *testing.T) {
		env := []string{"A=1"}
		setEnvValue(&env, "KEY", "v=1")
		want := []string{"A=1", "KEY=v=1"}
		if !reflect.DeepEqual(env, want) {
			t.Fatalf("env = %#v, want %#v", env, want)
		}
	})

	t.Run("trims key and ignores entries without equals", func(t *testing.T) {
		env := []string{"KEY", "OTHER=1"}
		setEnvValue(&env, "  KEY  ", "x")
		want := []string{"KEY", "OTHER=1", "KEY=x"}
		if !reflect.DeepEqual(env, want) {
			t.Fatalf("env = %#v, want %#v", env, want)
		}
	})

	t.Run("does not match key prefixes", func(t *testing.T) {
		env := []string{"KEYS=1"}
		setEnvValue(&env, "KEY", "2")
		want := []string{"KEYS=1", "KEY=2"}
		if !reflect.DeepEqual(env, want) {
			t.Fatalf("env = %#v, want %#v", env, want)
		}
	})

	t.Run("blank key is ignored", func(t *testing.T) {
		env := []string{"A=1"}
		setEnvValue(&env, "   ", "x")
		if want := []string{"A=1"}; !reflect.DeepEqual(env, want) {
			t.Fatalf("env = %#v, want %#v", env, want)
		}
	})

	t.Run("nil env is ignored", func(_ *testing.T) {
		setEnvValue(nil, "KEY", "x") // must not panic
	})

	t.Run("key case follows platform rules", func(t *testing.T) {
		env := []string{"Path=old"}
		setEnvValue(&env, "PATH", "new")
		want := []string{"Path=old", "PATH=new"}
		if runtime.GOOS == "windows" {
			want = []string{"PATH=new"}
		}
		if !reflect.DeepEqual(env, want) {
			t.Fatalf("env = %#v, want %#v", env, want)
		}
	})
}

func TestNormalizeEnvKey(t *testing.T) {
	want := "Path"
	if runtime.GOOS == "windows" {
		want = "PATH"
	}
	if got := normalizeEnvKey("Path"); got != want {
		t.Fatalf("normalizeEnvKey(%q) = %q, want %q", "Path", got, want)
	}
}

func TestApplyEnvMapNilAndEmpty(t *testing.T) {
	applyEnvMap(nil, map[string]string{"A": "1"}) // must not panic
	env := []string{"A=1"}
	applyEnvMap(&env, nil)
	if want := []string{"A=1"}; !reflect.DeepEqual(env, want) {
		t.Fatalf("env = %#v, want %#v", env, want)
	}
}
