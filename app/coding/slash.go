package coding

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	agent "github.com/cago-frame/agents/agent"
)

// Template is one loaded prompt template.
type Template struct {
	Name         string
	Description  string
	ArgumentHint string
	Body         string
	Source       string // absolute file path
}

// LoadTemplates scans ~/.claude/commands/*.md + every .claude/commands/*.md along the
// cwd→repo-root chain. Project-level wins on Name conflicts; cwd-direction wins within project.
func LoadTemplates(ctx context.Context, cwd string) ([]Template, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return loadTemplatesWithHome(ctx, cwd, home)
}

func loadTemplatesWithHome(ctx context.Context, cwd, home string) ([]Template, error) {
	cwdAbs, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	root, isRepo := findGitRoot(cwdAbs)
	if !isRepo {
		root = home
	}
	byName := make(map[string]Template)
	insert := func(t Template) { byName[t.Name] = t }

	if home != "" {
		for _, t := range scanCommandDir(ctx, filepath.Join(home, ".claude", "commands")) {
			insert(t)
		}
	}
	for _, dir := range chainDirs(root, cwdAbs) {
		for _, t := range scanCommandDir(ctx, filepath.Join(dir, ".claude", "commands")) {
			insert(t)
		}
	}
	out := make([]Template, 0, len(byName))
	for _, t := range byName {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func scanCommandDir(ctx context.Context, dir string) []Template {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			logger.Ctx(ctx).Warn("scanCommandDir: readdir failed",
				zap.String("dir", dir),
				zap.Error(err),
			)
		}
		return nil
	}
	var out []Template
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(full) //nolint:gosec // path from trusted dir scan
		if err != nil {
			continue
		}
		tmpl, ok := parseTemplate(data)
		if !ok {
			continue
		}
		tmpl.Name = strings.TrimSuffix(e.Name(), ".md")
		tmpl.Source = full
		out = append(out, tmpl)
	}
	return out
}

// parseTemplate splits frontmatter from body. Recognized keys: description, argument-hint.
// Unknown keys ignored. Files without frontmatter use the whole content as Body.
func parseTemplate(data []byte) (Template, bool) {
	s := string(data)
	if !strings.HasPrefix(strings.TrimLeft(s, " \t\r\n"), "---") {
		return Template{Body: s}, true
	}
	lines := strings.Split(s, "\n")
	open, closeIdx := -1, -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "---" {
			if open == -1 {
				open = i
			} else {
				closeIdx = i
				break
			}
		}
	}
	if open == -1 || closeIdx == -1 {
		return Template{Body: s}, true
	}
	var t Template
	for _, line := range lines[open+1 : closeIdx] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:colon]))
		val := strings.Trim(strings.TrimSpace(line[colon+1:]), `"'`)
		switch key {
		case "description":
			t.Description = val
		case "argument-hint":
			t.ArgumentHint = val
		}
	}
	t.Body = strings.Join(lines[closeIdx+1:], "\n")
	t.Body = strings.TrimPrefix(t.Body, "\n")
	return t, true
}

var (
	reSliceLen = regexp.MustCompile(`\$\{@:(\d+):(\d+)\}`)
	reSlice    = regexp.MustCompile(`\$\{@:(\d+)\}`)
	rePosArg   = regexp.MustCompile(`\$(\d+)`)
)

// Expand performs single-pass interpolation: $1, $2, ... / $@, $ARGUMENTS / ${@:N} / ${@:N:L}.
// Argument values are NOT recursively re-substituted.
func (t *Template) Expand(args []string) (string, error) {
	body := t.Body
	all := strings.Join(args, " ")

	// 1. ${@:N:L}
	body = reSliceLen.ReplaceAllStringFunc(body, func(m string) string {
		sm := reSliceLen.FindStringSubmatch(m)
		n, _ := strconv.Atoi(sm[1])
		l, _ := strconv.Atoi(sm[2])
		return joinSlice(args, n, l)
	})
	// 2. ${@:N}
	body = reSlice.ReplaceAllStringFunc(body, func(m string) string {
		sm := reSlice.FindStringSubmatch(m)
		n, _ := strconv.Atoi(sm[1])
		return joinSlice(args, n, len(args))
	})
	// 3. $@ and $ARGUMENTS
	body = strings.ReplaceAll(body, "$ARGUMENTS", all)
	body = strings.ReplaceAll(body, "$@", all)
	// 4. Positional $1, $2, ...
	body = rePosArg.ReplaceAllStringFunc(body, func(m string) string {
		idx, _ := strconv.Atoi(m[1:])
		if idx >= 1 && idx <= len(args) {
			return args[idx-1]
		}
		return ""
	})
	return body, nil
}

func joinSlice(args []string, n, l int) string {
	if n < 1 || n > len(args) {
		return ""
	}
	end := n - 1 + l
	if end > len(args) {
		end = len(args)
	}
	return strings.Join(args[n-1:end], " ")
}

// SlashRegistry merges templates + library-builtin commands behind a single Resolve API.
type SlashRegistry struct {
	templates map[string]*Template
	builtins  map[string]Builtin
}

// Builtin is one library-internal slash command.
type Builtin struct {
	Name        string
	Description string
	Run         func(ctx context.Context, conv *agent.Conversation, args []string) (BuiltinOutput, error)
}

// BuiltinOutput is the result of a builtin run.
type BuiltinOutput struct {
	UserText string // non-empty: caller should send this as the next user prompt
	Notice   string // non-empty: caller should display to user (UI string, not a prompt)
}

// ResolveResult bundles what Resolve returned.
type ResolveResult struct {
	Literal      string
	IsSlash      bool
	IsBuiltin    bool
	Run          func(ctx context.Context, conv *agent.Conversation) (BuiltinOutput, error)
	TemplateName string
	BuiltinName  string
}

// ErrUnknownSlashCommand is returned by Resolve when a /name is not registered.
var ErrUnknownSlashCommand = errors.New("coding: unknown slash command")

// Resolve interprets one user-input line.
func (r *SlashRegistry) Resolve(line string) (ResolveResult, error) {
	name, args, isSlash := parseSlashLine(line)
	if !isSlash {
		return ResolveResult{Literal: line, IsSlash: false}, nil
	}
	if t, ok := r.templates[name]; ok {
		expanded, err := t.Expand(args)
		if err != nil {
			return ResolveResult{}, err
		}
		return ResolveResult{
			Literal:      expanded,
			IsSlash:      true,
			TemplateName: name,
		}, nil
	}
	if b, ok := r.builtins[name]; ok {
		captured := args
		return ResolveResult{
			IsSlash:     true,
			IsBuiltin:   true,
			BuiltinName: name,
			Run: func(ctx context.Context, conv *agent.Conversation) (BuiltinOutput, error) {
				return b.Run(ctx, conv, captured)
			},
		}, nil
	}
	return ResolveResult{}, fmt.Errorf("%w: %s", ErrUnknownSlashCommand, name)
}

// parseSlashLine splits "/name a b \"c d\"" into ("name", ["a","b","c d"], true).
// Non-slash lines: returns ("", nil, false).
func parseSlashLine(line string) (string, []string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "/") {
		return "", nil, false
	}
	rest := strings.TrimSpace(line[1:])
	tokens := tokenizeArgs(rest)
	if len(tokens) == 0 {
		return "", nil, false
	}
	return tokens[0], tokens[1:], true
}

func tokenizeArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '"':
			inQuote = !inQuote
		case (ch == ' ' || ch == '\t') && !inQuote:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(ch)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func newSlashRegistry() *SlashRegistry {
	return &SlashRegistry{
		templates: map[string]*Template{},
		builtins:  map[string]Builtin{},
	}
}
