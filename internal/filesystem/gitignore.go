package filesystem

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

type ignoreRule struct {
	base    string
	negated bool
	dirOnly bool
	matcher *regexp.Regexp
}

// IgnoreMatcher evaluates .gitignore rules collected from a directory tree.
type IgnoreMatcher struct {
	rules []ignoreRule
}

// AddGitignore loads the .gitignore file in dir, whose slash-separated path
// relative to the walk root is relDir ("" for the root itself). A missing file
// is not an error.
func (m *IgnoreMatcher) AddGitignore(dir, relDir string) error {
	f, err := os.Open(filepath.Join(dir, ".gitignore"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		m.AddPattern(relDir, scanner.Text())
	}
	return scanner.Err()
}

// AddPattern adds a single gitignore pattern line scoped to relDir.
func (m *IgnoreMatcher) AddPattern(relDir, line string) {
	line = strings.TrimRight(line, " \t\r")
	if line == "" || strings.HasPrefix(line, "#") {
		return
	}
	rule := ignoreRule{base: strings.Trim(relDir, "/")}
	if strings.HasPrefix(line, "!") {
		rule.negated = true
		line = line[1:]
	} else if strings.HasPrefix(line, `\`) {
		line = line[1:]
	}
	if strings.HasSuffix(line, "/") {
		rule.dirOnly = true
		line = strings.TrimRight(line, "/")
	}
	if line == "" {
		return
	}
	anchored := strings.Contains(line, "/")
	line = strings.TrimPrefix(line, "/")
	prefix := ""
	if !anchored {
		prefix = "(?:.*/)?"
	}
	re, err := regexp.Compile("^" + prefix + gitignoreGlobToRegex(line) + "$")
	if err != nil {
		return
	}
	rule.matcher = re
	m.rules = append(m.rules, rule)
}

// Ignored reports whether the slash-separated path rel, relative to the walk
// root, is ignored. The last matching rule wins, as in git.
func (m *IgnoreMatcher) Ignored(rel string, isDir bool) bool {
	if m == nil {
		return false
	}
	ignored := false
	for _, rule := range m.rules {
		if rule.dirOnly && !isDir {
			continue
		}
		target := rel
		if rule.base != "" {
			if !strings.HasPrefix(rel, rule.base+"/") {
				continue
			}
			target = strings.TrimPrefix(rel, rule.base+"/")
		}
		if rule.matcher.MatchString(target) {
			ignored = !rule.negated
		}
	}
	return ignored
}

// WalkFiles walks the regular files under root in lexical order, calling fn
// with each file's slash-separated path relative to root. The .git directory
// is always skipped, and .gitignore rules are honored when respectGitignore is
// set.
func WalkFiles(root string, respectGitignore bool, fn func(rel string, d fs.DirEntry) error) error {
	matcher := &IgnoreMatcher{}
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "." {
				rel = ""
			} else if d.Name() == ".git" || (respectGitignore && matcher.Ignored(rel, true)) {
				return filepath.SkipDir
			}
			if respectGitignore {
				_ = matcher.AddGitignore(p, rel)
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if respectGitignore && matcher.Ignored(rel, false) {
			return nil
		}
		return fn(rel, d)
	})
}

// MatchGlob reports whether the slash-separated path rel matches pattern.
// "*" and "?" stay within one path segment and "**" spans segments. A pattern
// without a slash matches the base name at any depth.
func MatchGlob(pattern, rel string) bool {
	pattern = strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(pattern)), "./")
	if pattern == "" {
		return true
	}
	if !strings.Contains(pattern, "/") {
		ok, _ := regexp.MatchString("^"+gitignoreGlobToRegex(pattern)+"$", path.Base(rel))
		return ok
	}
	ok, _ := regexp.MatchString("^"+gitignoreGlobToRegex(strings.TrimPrefix(pattern, "/"))+"$", rel)
	return ok
}

func gitignoreGlobToRegex(pattern string) string {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch {
		case strings.HasPrefix(pattern[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(pattern[i:], "/**") && i+3 == len(pattern):
			b.WriteString("(?:/.*)?")
			i += 2
		case strings.HasPrefix(pattern[i:], "**"):
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '[':
			end := strings.IndexByte(pattern[i+1:], ']')
			if end < 0 {
				b.WriteString(`\[`)
				continue
			}
			class := pattern[i+1 : i+1+end]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i += end + 1
		case c == '{':
			end := strings.IndexByte(pattern[i+1:], '}')
			if end < 0 {
				b.WriteString(`\{`)
				continue
			}
			alts := strings.Split(pattern[i+1:i+1+end], ",")
			for j, alt := range alts {
				alts[j] = gitignoreGlobToRegex(alt)
			}
			b.WriteString("(?:" + strings.Join(alts, "|") + ")")
			i += end + 1
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String()
}
