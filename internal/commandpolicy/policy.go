// Package commandpolicy evaluates allow/deny command rules for agents.
package commandpolicy

import (
	"fmt"
	"regexp"
	"runtime"
	"strings"
)

type compiledRule struct {
	negated bool
	matcher *regexp.Regexp
}

// Policy evaluates ordered allow/deny command rules.
type Policy struct {
	rules []compiledRule
}

// New compiles the provided ordered rules.
func New(patterns []string) (*Policy, error) {
	rules := make([]compiledRule, 0, len(patterns))
	for _, pattern := range patterns {
		rule, ok, err := compileRule(pattern)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		rules = append(rules, rule)
	}
	return &Policy{rules: rules}, nil
}

// Allows reports whether command is allowed by the ordered rule set.
func (p *Policy) Allows(command string) bool {
	if p == nil {
		return false
	}
	cmd := normalize(command)
	allowed := false
	for _, rule := range p.rules {
		if rule.matcher.MatchString(cmd) {
			allowed = !rule.negated
		}
	}
	return allowed
}

func compileRule(raw string) (compiledRule, bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return compiledRule{}, false, nil
	}
	negated := strings.HasPrefix(trimmed, "!")
	if negated {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "!"))
	}
	if trimmed == "" {
		return compiledRule{}, false, fmt.Errorf("invalid command allowlist rule %q", raw)
	}
	re, err := regexp.Compile(globToRegex(normalize(trimmed)))
	if err != nil {
		return compiledRule{}, false, fmt.Errorf("compiling command allowlist rule %q: %w", raw, err)
	}
	return compiledRule{negated: negated, matcher: re}, true, nil
}

func normalize(command string) string {
	command = strings.TrimSpace(command)
	if runtime.GOOS == "windows" {
		command = strings.ToLower(command)
	}
	return command
}

func globToRegex(pattern string) string {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		switch ch {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
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
