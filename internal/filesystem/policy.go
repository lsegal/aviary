package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"
)

type compiledRule struct {
	raw     string
	negated bool
	pattern string
	matcher *regexp.Regexp
}

// Policy evaluates ordered allow/deny filesystem rules.
type Policy struct {
	rules []compiledRule
}

// NewPolicy compiles the provided ordered rules.
func NewPolicy(patterns []string) (*Policy, error) {
	rules := make([]compiledRule, 0, len(patterns))
	for _, pattern := range patterns {
		rule, err := compileRule(pattern)
		if err != nil {
			return nil, err
		}
		if rule.raw == "" {
			continue
		}
		rules = append(rules, rule)
	}
	return &Policy{rules: rules}, nil
}

// Allows reports whether the resolved path is allowed by the ordered rule set.
func (p *Policy) Allows(resolvedPath string) bool {
	if p == nil {
		return false
	}
	path := normalizePath(resolvedPath)
	allowed := false
	for _, rule := range p.rules {
		if rule.matcher.MatchString(path) {
			allowed = !rule.negated
		}
	}
	return allowed
}

// ResolvePath resolves a user-supplied path against the workspace, expands
// special prefixes, and resolves any existing symlink ancestors.
func ResolvePath(raw string) (string, error) {
	expanded, err := expandPath(raw, false)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("resolving absolute path: %w", err)
	}
	return resolveExistingAncestor(filepath.Clean(abs))
}

// PolicyFromAgent creates a path policy from an agent config.
func PolicyFromAgent(cfg *config.AgentConfig) (*Policy, error) {
	if cfg == nil || cfg.Permissions == nil || cfg.Permissions.Filesystem == nil {
		return NewPolicy(nil)
	}
	return NewPolicy(cfg.Permissions.Filesystem.AllowedPaths)
}

func compileRule(raw string) (compiledRule, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return compiledRule{}, nil
	}
	negated := strings.HasPrefix(trimmed, "!")
	if negated {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "!"))
	}
	expanded, err := expandPath(trimmed, true)
	if err != nil {
		return compiledRule{}, fmt.Errorf("invalid allowlist rule %q: %w", raw, err)
	}
	pattern := normalizePath(filepath.Clean(expanded))
	re, err := regexp.Compile(globToRegex(pattern))
	if err != nil {
		return compiledRule{}, fmt.Errorf("compiling allowlist rule %q: %w", raw, err)
	}
	return compiledRule{
		raw:     raw,
		negated: negated,
		pattern: pattern,
		matcher: re,
	}, nil
}

func expandPath(raw string, allowGlob bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("path is required")
	}
	if vol := filepath.VolumeName(raw); vol != "" && !filepath.IsAbs(raw) {
		return "", fmt.Errorf("drive-relative paths are not allowed: %s", raw)
	}

	var expanded string
	switch {
	case strings.HasPrefix(raw, "~"):
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home dir: %w", err)
		}
		expanded = joinBasePreservingGlob(home, strings.TrimPrefix(raw, "~"))
	case strings.HasPrefix(raw, "@"):
		expanded = joinBasePreservingGlob(config.BaseDir(), strings.TrimPrefix(raw, "@"))
	case filepath.IsAbs(raw):
		expanded = raw
	default:
		expanded = joinBasePreservingGlob(store.WorkspaceDir(), raw)
	}
	if !allowGlob && strings.ContainsAny(expanded, "*?[") {
		return "", fmt.Errorf("glob characters are not allowed in file paths")
	}
	return expanded, nil
}

func joinBasePreservingGlob(base, rest string) string {
	trimmed := strings.TrimLeft(rest, `/\`)
	if trimmed == "" {
		return base
	}
	return filepath.Join(base, filepath.FromSlash(strings.ReplaceAll(trimmed, `\`, `/`)))
}

func resolveExistingAncestor(cleanPath string) (string, error) {
	missing := make([]string, 0, 4)
	current := cleanPath
	for {
		if _, err := os.Lstat(current); err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", fmt.Errorf("resolving symlinks for %s: %w", current, err)
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no existing ancestor for path %s", cleanPath)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func normalizePath(p string) string {
	norm := filepath.ToSlash(filepath.Clean(p))
	if runtime.GOOS == "windows" {
		norm = strings.ToLower(norm)
	}
	return norm
}

func globToRegex(pattern string) string {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		switch ch {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '[':
			j := i + 1
			for j < len(pattern) && pattern[j] != ']' {
				j++
			}
			if j < len(pattern) {
				b.WriteString(pattern[i : j+1])
				i = j
			} else {
				b.WriteString(`\[`)
			}
		default:
			if strings.ContainsRune(`.+()|{}^$\\`, rune(ch)) {
				b.WriteByte('\\')
			}
			b.WriteByte(ch)
		}
	}
	b.WriteString("$")
	return b.String()
}
