package task

import "strconv"

// formatID 把 int 编成 "t<n>"。
func formatID(n int) string { return "t" + strconv.Itoa(n) }

// parseID 反向解析 "t<n>" 形式的 id。失败返回 ok=false（包括宿主自带的非数字 id，alloc 时跳过即可）。
func parseID(s string) (int, bool) {
	if len(s) < 2 || s[0] != 't' {
		return 0, false
	}
	n, err := strconv.Atoi(s[1:])
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}
