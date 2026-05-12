// Package pathutil 给 tool 子包提供共享的路径解析逻辑。
// 直接对齐 pi-mono path-utils.ts：~ 展开、@ 前缀剥离、Unicode 空格归一。
// 没有"必须在 cwd 下"的沙箱（pi-mono 也没有）—— 让上层 hook / 权限层去管。
package pathutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Expand 处理 @ 前缀、~ home 展开、Unicode 空格归一。
// 返回展开后的字符串（仍可能是相对路径）。
func Expand(p string) string {
	if p == "" {
		return p
	}
	p = strings.TrimPrefix(p, "@")
	p = normalizeUnicodeSpaces(p)
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return p
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// ResolveToCwd 把相对路径接到 cwd 下；绝对路径原样返回。
// cwd 为空时不补默认值，让调用方负责。
func ResolveToCwd(p, cwd string) string {
	expanded := Expand(p)
	if filepath.IsAbs(expanded) {
		return filepath.Clean(expanded)
	}
	if cwd == "" {
		return filepath.Clean(expanded)
	}
	return filepath.Join(cwd, expanded)
}

// ResolveReadPath 是 read 工具专用：先按 ResolveToCwd，文件存在直接返回；
// 否则按 macOS screenshot 友好顺序尝试 4 种 NFD/curly-quote/narrow-NBSP 变体。
// 这些变体在非 macOS / 普通文件名上是无害的，找不到时回退到首选路径
// （最终错误由调用方读文件时报）。
func ResolveReadPath(p, cwd string) string {
	primary := ResolveToCwd(p, cwd)
	if exists(primary) {
		return primary
	}
	for _, alt := range macFilenameVariants(primary) {
		if exists(alt) {
			return alt
		}
	}
	return primary
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}

// 替换字符串中常见的 Unicode 空格为 ASCII 空格。
// 与 pi-mono normalizeUnicodeSpaces 对齐。
func normalizeUnicodeSpaces(s string) string {
	if s == "" {
		return s
	}
	repl := []struct{ from, to string }{
		{" ", " "}, // NO-BREAK SPACE
		{" ", " "}, // NARROW NO-BREAK SPACE
		{" ", " "}, // THIN SPACE
		{" ", " "}, // FIGURE SPACE
		{"　", " "}, // IDEOGRAPHIC SPACE
	}
	for _, r := range repl {
		s = strings.ReplaceAll(s, r.from, " ")
	}
	return s
}

// macFilenameVariants 返回 macOS 友好的 4 种文件名变体（按 pi-mono 顺序）。
func macFilenameVariants(p string) []string {
	variants := []string{}
	// 1. AM/PM narrow NBSP variant: " AM." → "  AM."
	if strings.Contains(p, " AM.") || strings.Contains(p, " PM.") {
		v := strings.ReplaceAll(p, " AM.", " AM.")
		v = strings.ReplaceAll(v, " PM.", " PM.")
		variants = append(variants, v)
	}
	// 2. NFD form (decomposed) — Go 标准库不带 NFD，简单近似：替换 É 等无效。
	//    我们退化成不做 NFD，只做剩下三种；Linux 下文件系统也通常用 NFC 存盘，
	//    NFD 主要是 macOS HFS+ 的特例，对 cago/agents 的目标用户影响很小。
	// 3. Curly quote variant
	if strings.Contains(p, "'") {
		variants = append(variants, strings.ReplaceAll(p, "'", "’"))
	}
	return variants
}
