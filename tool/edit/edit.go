// Package edit 暴露一个"按精确文本替换的多块编辑"工具，行为对齐 pi-mono edit.ts：
//   - oldText 必须在原文件里唯一，且不与同次调用的其他 oldText 重叠
//   - 没有 replace_all：唯一性是强制约束
//   - exact match miss 时回退 fuzzy（NFKC + 智能引号/破折号/空格归一）；
//     一旦走 fuzzy，磁盘上的内容也会被替换为 fuzzy-normalized 形式（无副作用替换）
//   - 跨平台保留 CRLF / LF / BOM
package edit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/internal/filemu"
	"github.com/cago-frame/agents/tool/internal/pathutil"
	"github.com/cago-frame/agents/tool/state"
)

var noAdditional = func() *bool { v := false; return &v }()

// Name 是工具暴露给 LLM 的名字。
const Name = "edit"

// Description 与 pi-mono 对齐。
const Description = "Edit a single file using exact text replacement. Every edits[].oldText must match a unique, non-overlapping region of the original file. If two changes affect the same block or nearby lines, merge them into one edit instead of emitting overlapping edits. Do not include large unchanged regions just to connect distant changes."

// Option 可选构造项。
type Option func(*config)

type config struct {
	cwd     string
	pool    *filemu.Pool
	tracker *state.ReadTracker
	serial  bool
}

// Cwd 指定相对路径解析的根。
func Cwd(p string) Option { return func(c *config) { c.cwd = p } }

// Pool 覆盖默认的 per-path 文件互斥池（默认共用 filemu.Default）。
func Pool(p *filemu.Pool) Option { return func(c *config) { c.pool = p } }

// Tracker 接入 *state.ReadTracker。挂上后 edit 之前必须先 read 过该文件，
// 且文件自上次 read 之后不能被外部改过；否则报相应错误（与 Claude Code 行为对齐）。
func Tracker(t *state.ReadTracker) Option { return func(c *config) { c.tracker = t } }

// Serial 让该工具与同一轮的其他工具串行执行。
func Serial() Option { return func(c *config) { c.serial = true } }

// New 构造 edit 工具。
func New(opts ...Option) tool.Tool {
	cfg := &config{}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.pool == nil {
		cfg.pool = &filemu.Default
	}
	return &tool.RawTool{
		NameStr:   Name,
		DescStr:   Description,
		SchemaVal: schema(),
		IsSerial:  cfg.serial,
		Handler: func(ctx context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
			return run(ctx, cfg, in)
		},
		PromptSnippetStr: "Edit a file by exact text replacement (multiple disjoint edits per call).",
		PromptGuidelinesArr: []string{
			"Read the file first; the edit pre-image must match byte-for-byte.",
			"Group multiple edits in one call instead of editing the same file repeatedly.",
		},
	}
}

func schema() agent.Schema {
	return agent.Schema{
		Type:     "object",
		Required: []string{"path", "edits"},
		Properties: map[string]*agent.Property{
			"path": {Type: "string", Description: "Path to the file to edit (relative or absolute)"},
			"edits": {
				Type:        "array",
				Description: "One or more targeted replacements. Each edit is matched against the original file, not incrementally. Do not include overlapping or nested edits. If two changes touch the same block or nearby lines, merge them into one edit instead.",
				MinItems:    1,
				Items: &agent.Property{
					Type: "object",
					Properties: map[string]*agent.Property{
						"oldText":     {Type: "string", Description: "Exact text for one targeted replacement. Must be unique in the original file unless replace_all is set."},
						"newText":     {Type: "string", Description: "Replacement text for this targeted edit."},
						"replace_all": {Type: "boolean", Description: "If true, replace every occurrence of oldText (uniqueness no longer required). Default false."},
					},
					Required:             []string{"oldText", "newText"},
					AdditionalProperties: noAdditional,
				},
			},
		},
	}
}

type editEntry struct {
	OldText    string
	NewText    string
	ReplaceAll bool
}

// parseEdits extracts the edits slice from the map[string]any input.
// Handles three shapes:
//  1. Standard: {"path": ..., "edits": [...]}
//  2. Legacy top-level: {"path": ..., "oldText": ..., "newText": ...}
//  3. edits as a JSON string (compatibility)
func parseEdits(in map[string]any) (path string, edits []editEntry, err error) {
	path, _ = in["path"].(string)

	rawEdits, hasEdits := in["edits"]
	if hasEdits {
		switch v := rawEdits.(type) {
		case []any:
			// Standard shape: edits came through JSON decode ([]any of map[string]any)
			for _, item := range v {
				m, ok := item.(map[string]any)
				if !ok {
					return path, nil, fmt.Errorf("edit: edits item is not an object")
				}
				e := editEntry{}
				e.OldText, _ = m["oldText"].(string)
				e.NewText, _ = m["newText"].(string)
				if ra, ok := m["replace_all"].(bool); ok {
					e.ReplaceAll = ra
				}
				edits = append(edits, e)
			}
		case []map[string]any:
			// Direct Go call: edits passed as []map[string]any
			for _, m := range v {
				e := editEntry{}
				e.OldText, _ = m["oldText"].(string)
				e.NewText, _ = m["newText"].(string)
				if ra, ok := m["replace_all"].(bool); ok {
					e.ReplaceAll = ra
				}
				edits = append(edits, e)
			}
		case string:
			// edits encoded as a JSON string (compatibility)
			var arr []struct {
				OldText    string `json:"oldText"`
				NewText    string `json:"newText"`
				ReplaceAll bool   `json:"replace_all"`
			}
			if jerr := json.Unmarshal([]byte(v), &arr); jerr == nil {
				for _, a := range arr {
					edits = append(edits, editEntry{OldText: a.OldText, NewText: a.NewText, ReplaceAll: a.ReplaceAll})
				}
			}
		}
	}

	// Legacy top-level oldText/newText
	if len(edits) == 0 {
		oldText, _ := in["oldText"].(string)
		newText, _ := in["newText"].(string)
		if oldText != "" || newText != "" {
			edits = append(edits, editEntry{OldText: oldText, NewText: newText})
		}
	}

	return path, edits, nil
}

func run(ctx context.Context, cfg *config, in map[string]any) (*agent.ToolResultBlock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	path, edits, err := parseEdits(in)
	if err != nil {
		return tool.ErrorResult(err.Error()), nil
	}

	if path == "" {
		return tool.ErrorResult("edit: path must not be empty"), nil
	}
	if len(edits) == 0 {
		return tool.ErrorResult("Edit tool input is invalid. edits must contain at least one replacement."), nil
	}

	abs := pathutil.ResolveToCwd(path, cfg.cwd)
	var result string
	var toolErr string
	poolErr := cfg.pool.With(abs, func() error {
		if cfg.tracker != nil {
			if _, terr := cfg.tracker.Check(abs); terr != nil {
				switch {
				case errors.Is(terr, state.ErrNotRead):
					toolErr = fmt.Sprintf("%s has not been read in this session. Use read first before editing.", path)
					return nil
				case errors.Is(terr, state.ErrStale):
					toolErr = fmt.Sprintf("%s has been modified since the last read. Read it again before editing.", path)
					return nil
				default:
					return fmt.Errorf("tracker check: %w", terr)
				}
			}
		}
		raw, rerr := os.ReadFile(abs) //nolint:gosec // tool is intentionally unsandboxed
		if rerr != nil {
			code := rerr.Error()
			if pe := new(os.PathError); errors.As(rerr, &pe) && pe.Err != nil {
				code = pe.Err.Error()
			}
			toolErr = fmt.Sprintf("Could not edit file: %s. Error code: %s.", path, code)
			return nil
		}
		original := string(raw)
		bom := ""
		const bomMark = "\ufeff"
		if strings.HasPrefix(original, bomMark) {
			bom = bomMark
			original = strings.TrimPrefix(original, bomMark)
		}
		ending := detectLineEnding(original)
		normalized := normalizeToLF(original)

		newContent, applyErr := applyEdits(normalized, edits, path)
		if applyErr != nil {
			toolErr = applyErr.Error()
		}
		if toolErr != "" {
			return nil
		}

		final := bom + restoreLineEndings(newContent, ending)
		if werr := os.WriteFile(abs, []byte(final), 0o600); werr != nil { //nolint:gosec // tool intentionally unsandboxed
			return fmt.Errorf("edit: write: %w", werr)
		}
		if cfg.tracker != nil {
			cfg.tracker.Forget(abs)
		}
		result = fmt.Sprintf("Successfully replaced %d block(s) in %s.", len(edits), path)
		return nil
	})
	if poolErr != nil {
		return nil, fmt.Errorf("edit: %w", poolErr)
	}
	if toolErr != "" {
		return tool.ErrorResult(toolErr), nil
	}
	return tool.TextResult(result), nil
}

func detectLineEnding(s string) string {
	crlf := strings.Index(s, "\r\n")
	if crlf < 0 {
		return "\n"
	}
	lf := indexOfStandaloneLF(s)
	if lf >= 0 && lf < crlf {
		return "\n"
	}
	return "\r\n"
}

// indexOfStandaloneLF 找到第一个不在 "\r\n" 序列中的 '\n'。
func indexOfStandaloneLF(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] != '\n' {
			continue
		}
		if i == 0 || s[i-1] != '\r' {
			return i
		}
	}
	return -1
}

func normalizeToLF(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

func restoreLineEndings(s, ending string) string {
	if ending == "\r\n" {
		return strings.ReplaceAll(s, "\n", "\r\n")
	}
	return s
}

// applyEdits 把所有 edits 在 base 内做精确替换；失败回退 fuzzy。
// 返回新内容；若发生任何错误返回 pi-mono 风格错误信息。
func applyEdits(base string, edits []editEntry, path string) (string, error) {
	multi := len(edits) > 1
	for i, e := range edits {
		if e.OldText == "" {
			if multi {
				return "", fmt.Errorf("edits[%d].oldText must not be empty in %s.", i, path) //nolint:staticcheck // exact pi-mono error text shown to LLM
			}
			return "", fmt.Errorf("oldText must not be empty in %s.", path) //nolint:staticcheck // exact pi-mono error text shown to LLM
		}
	}

	// 先 probe 一遍：所有 edit 都用 LF-normalized 的 oldText/newText
	normEdits := make([]editEntry, len(edits))
	for i, e := range edits {
		normEdits[i] = editEntry{
			OldText:    normalizeToLF(e.OldText),
			NewText:    normalizeToLF(e.NewText),
			ReplaceAll: e.ReplaceAll,
		}
	}
	needFuzzy := false
	for _, e := range normEdits {
		if !strings.Contains(base, e.OldText) {
			needFuzzy = true
			break
		}
	}

	working := base
	if needFuzzy {
		working = normalizeForFuzzy(base)
	}
	workingEdits := normEdits
	if needFuzzy {
		workingEdits = make([]editEntry, len(normEdits))
		for i, e := range normEdits {
			workingEdits[i] = editEntry{
				OldText:    normalizeForFuzzy(e.OldText),
				NewText:    e.NewText,
				ReplaceAll: e.ReplaceAll,
			}
		}
	}

	type matched struct {
		index   int
		oldText string
		newText string
		matchAt int
	}
	matches := make([]matched, 0, len(workingEdits))
	for i, e := range workingEdits {
		idx := strings.Index(working, e.OldText)
		if idx < 0 {
			if multi {
				return "", fmt.Errorf("Could not find edits[%d] in %s. The oldText must match exactly including all whitespace and newlines.", i, path) //nolint:staticcheck // exact pi-mono error text shown to LLM
			}
			return "", fmt.Errorf("Could not find the exact text in %s. The old text must match exactly including all whitespace and newlines.", path) //nolint:staticcheck // exact pi-mono error text shown to LLM
		}
		if e.ReplaceAll {
			// 全部出现都登记为独立 match
			off := 0
			for {
				idx := strings.Index(working[off:], e.OldText)
				if idx < 0 {
					break
				}
				matches = append(matches, matched{
					index:   i,
					oldText: e.OldText,
					newText: e.NewText,
					matchAt: off + idx,
				})
				off += idx + len(e.OldText)
			}
			continue
		}
		occ := strings.Count(working, e.OldText)
		if occ > 1 {
			if multi {
				return "", fmt.Errorf("Found %d occurrences of edits[%d] in %s. Each oldText must be unique (or set replace_all=true). Please provide more context to make it unique.", occ, i, path) //nolint:staticcheck // exact pi-mono error text shown to LLM
			}
			return "", fmt.Errorf("Found %d occurrences of the text in %s. The text must be unique (or set replace_all=true). Please provide more context to make it unique.", occ, path) //nolint:staticcheck // exact pi-mono error text shown to LLM
		}
		matches = append(matches, matched{
			index:   i,
			oldText: e.OldText,
			newText: e.NewText,
			matchAt: idx,
		})
	}

	sort.Slice(matches, func(i, j int) bool { return matches[i].matchAt < matches[j].matchAt })
	for i := 1; i < len(matches); i++ {
		prev := matches[i-1]
		cur := matches[i]
		if prev.matchAt+len(prev.oldText) > cur.matchAt {
			// replace_all 自身的多个匹配天然不相邻、不重叠 —— 这里被命中的是
			// 不同 edit 之间的重叠，仍按原 pi-mono 错误格式报。
			return "", fmt.Errorf("edits[%d] and edits[%d] overlap in %s. Merge them into one edit or target disjoint regions.", prev.index, cur.index, path) //nolint:staticcheck // exact pi-mono error text shown to LLM
		}
	}

	// 反向应用避免位移
	out := working
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		out = out[:m.matchAt] + m.newText + out[m.matchAt+len(m.oldText):]
	}

	if out == working {
		if multi {
			return "", fmt.Errorf("No changes made to %s. The replacements produced identical content.", path) //nolint:staticcheck // exact pi-mono error text shown to LLM
		}
		return "", fmt.Errorf("No changes made to %s. The replacement produced identical content. This might indicate an issue with special characters or the text not existing as expected.", path) //nolint:staticcheck // exact pi-mono error text shown to LLM
	}
	return out, nil
}

// normalizeForFuzzy 与 pi-mono normalizeForFuzzyMatch 对齐：
// NFKC -> 行尾去空格 -> smart quotes / dashes / 空格归一
func normalizeForFuzzy(s string) string {
	s = norm.NFKC.String(s)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	s = strings.Join(lines, "\n")
	for _, q := range []string{"‘", "’", "‚", "‛"} {
		s = strings.ReplaceAll(s, q, "'")
	}
	for _, q := range []string{"“", "”", "„", "‟"} {
		s = strings.ReplaceAll(s, q, "\"")
	}
	for _, d := range []string{"‐", "‑", "‒", "–", "—", "―", "−"} {
		s = strings.ReplaceAll(s, d, "-")
	}
	for _, sp := range []string{" ", " ", " ", " ", "　"} {
		s = strings.ReplaceAll(s, sp, " ")
	}
	return s
}
