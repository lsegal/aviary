package mcp

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/store"
)

func commandEnv(ctx context.Context, extra map[string]string) []string {
	env := append([]string{}, os.Environ()...)
	applyEnvFile(ctx, &env)
	applyEnvMap(&env, extra)
	return env
}

func applyEnvFile(ctx context.Context, env *[]string) {
	agentID, ok := agent.SessionAgentIDFromContext(ctx)
	if !ok || strings.TrimSpace(agentID) == "" {
		return
	}
	path := filepath.Join(store.AgentDir(agentID), ".env")
	values, err := parseDotEnvFile(path)
	if err != nil {
		return
	}
	applyEnvMap(env, values)
}

func applyEnvMap(env *[]string, values map[string]string) {
	if env == nil || len(values) == 0 {
		return
	}
	for key, value := range values {
		setEnvValue(env, key, value)
	}
}

func setEnvValue(env *[]string, key, value string) {
	key = strings.TrimSpace(key)
	if env == nil || key == "" {
		return
	}
	needle := normalizeEnvKey(key) + "="
	for i, entry := range *env {
		name, _, ok := strings.Cut(entry, "=")
		if ok && normalizeEnvKey(name)+"=" == needle {
			(*env)[i] = key + "=" + value
			return
		}
	}
	*env = append(*env, key+"="+value)
}

func normalizeEnvKey(key string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(key)
	}
	return key
}

func parseDotEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseDotEnv(string(data))
}

func parseDotEnv(src string) (map[string]string, error) {
	values := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(src))
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if lineNo == 1 {
			line = strings.TrimPrefix(line, "\uFEFF")
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, rawValue, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("dotenv line %d missing '='", lineNo)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("dotenv line %d has empty key", lineNo)
		}
		value, err := parseDotEnvValue(strings.TrimSpace(rawValue))
		if err != nil {
			return nil, fmt.Errorf("dotenv line %d: %w", lineNo, err)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func parseDotEnvValue(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	switch raw[0] {
	case '\'':
		if len(raw) < 2 || raw[len(raw)-1] != '\'' {
			return "", fmt.Errorf("unterminated single-quoted value")
		}
		return raw[1 : len(raw)-1], nil
	case '"':
		if len(raw) < 2 || raw[len(raw)-1] != '"' {
			return "", fmt.Errorf("unterminated double-quoted value")
		}
		return strconv.Unquote(raw)
	default:
		if idx := inlineCommentIndex(raw); idx >= 0 {
			raw = raw[:idx]
		}
		return strings.TrimSpace(raw), nil
	}
}

func inlineCommentIndex(raw string) int {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '#' {
			continue
		}
		if i == 0 || raw[i-1] == ' ' || raw[i-1] == '\t' {
			return i
		}
	}
	return -1
}
