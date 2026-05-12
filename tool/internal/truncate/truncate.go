// Package truncate 是 tool 子包共用的截断工具。直接对齐 pi-mono
// (packages/coding-agent/src/core/tools/truncate.ts) 的行为：行/字节双限制、
// 头/尾两种方向、最长单行截断、人类可读 size 输出。
package truncate

import (
	"fmt"
	"strings"
)

const (
	// DefaultMaxLines 是 read / bash / grep / find / ls 等工具的默认行数上限。
	DefaultMaxLines = 2000
	// DefaultMaxBytes 是默认字节上限（50KiB），所有工具一致。
	DefaultMaxBytes = 50 * 1024
	// GrepMaxLineLength 是 grep 单行字符上限，超出走 Line 截断。
	GrepMaxLineLength = 500
)

// Options 控制 Head / Tail 截断阈值。零值落到默认值。
type Options struct {
	MaxLines int
	MaxBytes int
}

func (o Options) resolve() (int, int) {
	ml := o.MaxLines
	if ml <= 0 {
		ml = DefaultMaxLines
	}
	mb := o.MaxBytes
	if mb <= 0 {
		mb = DefaultMaxBytes
	}
	return ml, mb
}

// Result 汇报一次截断的结果。LLM 可见的只有 Content；其他字段供调用方拼提示。
type Result struct {
	Content               string
	Truncated             bool
	TruncatedBy           string // "lines" / "bytes" / ""
	TotalLines            int
	TotalBytes            int
	OutputLines           int
	OutputBytes           int
	LastLinePartial       bool
	FirstLineExceedsLimit bool
	MaxLines              int
	MaxBytes              int
}

// Head 从内容头部保留尽可能多的整行，行/字节任一超限即停。
// 永远不返回半行；首行字节就超限则返回空内容并置 FirstLineExceedsLimit。
func Head(content string, opts Options) Result {
	maxLines, maxBytes := opts.resolve()
	totalBytes := len(content)
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	res := Result{
		TotalLines: totalLines,
		TotalBytes: totalBytes,
		MaxLines:   maxLines,
		MaxBytes:   maxBytes,
	}

	if totalLines <= maxLines && totalBytes <= maxBytes {
		res.Content = content
		res.OutputLines = totalLines
		res.OutputBytes = totalBytes
		return res
	}

	if len(lines[0]) > maxBytes {
		res.Truncated = true
		res.TruncatedBy = "bytes"
		res.FirstLineExceedsLimit = true
		return res
	}

	out := make([]string, 0, maxLines)
	bytesCount := 0
	truncatedBy := ""
	for i, line := range lines {
		cost := len(line)
		if i > 0 {
			cost++
		}
		if bytesCount+cost > maxBytes {
			truncatedBy = "bytes"
			break
		}
		out = append(out, line)
		bytesCount += cost
		if len(out) >= maxLines {
			if i+1 < totalLines {
				truncatedBy = "lines"
			}
			break
		}
	}

	res.Content = strings.Join(out, "\n")
	res.OutputLines = len(out)
	res.OutputBytes = bytesCount
	if truncatedBy != "" {
		res.Truncated = true
		res.TruncatedBy = truncatedBy
	}
	return res
}

// Tail 反向收集整行，尽可能保留内容尾部。最后一行（即原始末行）单独超限时
// 走字节级裁剪（UTF-8 安全：跳过续字节）保留尾部 maxBytes 字节。
func Tail(content string, opts Options) Result {
	maxLines, maxBytes := opts.resolve()
	totalBytes := len(content)
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	res := Result{
		TotalLines: totalLines,
		TotalBytes: totalBytes,
		MaxLines:   maxLines,
		MaxBytes:   maxBytes,
	}

	if totalLines <= maxLines && totalBytes <= maxBytes {
		res.Content = content
		res.OutputLines = totalLines
		res.OutputBytes = totalBytes
		return res
	}

	out := make([]string, 0, maxLines)
	bytesCount := 0
	truncatedBy := ""
	lastLinePartial := false

	for i := totalLines - 1; i >= 0; i-- {
		line := lines[i]
		cost := len(line)
		if len(out) > 0 {
			cost++
		}
		if bytesCount+cost > maxBytes {
			if len(out) == 0 {
				// 末行单独超限：从尾部裁剪保留 maxBytes 字节，跳过 UTF-8 续字节。
				start := max(len(line)-maxBytes, 0)
				for start < len(line) && line[start]&0xc0 == 0x80 {
					start++
				}
				partial := line[start:]
				out = append([]string{partial}, out...)
				bytesCount = len(partial)
				lastLinePartial = true
			}
			truncatedBy = "bytes"
			break
		}
		out = append([]string{line}, out...)
		bytesCount += cost
		if len(out) >= maxLines {
			if i > 0 {
				truncatedBy = "lines"
			}
			break
		}
	}

	res.Content = strings.Join(out, "\n")
	res.OutputLines = len(out)
	res.OutputBytes = bytesCount
	res.LastLinePartial = lastLinePartial
	if truncatedBy != "" {
		res.Truncated = true
		res.TruncatedBy = truncatedBy
	}
	return res
}

// Line 把单行截断到 maxChars 个字符（rune），超出追加 "... [truncated]"。
// maxChars <= 0 时落到 GrepMaxLineLength。
func Line(line string, maxChars int) (string, bool) {
	if maxChars <= 0 {
		maxChars = GrepMaxLineLength
	}
	runes := []rune(line)
	if len(runes) <= maxChars {
		return line, false
	}
	return string(runes[:maxChars]) + "... [truncated]", true
}

// FormatSize 把字节数格式化成 "123B" / "12.3KB" / "1.2MB"。
// 与 pi-mono formatSize(bytes) 对齐，给提示文案用。
func FormatSize(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
}
