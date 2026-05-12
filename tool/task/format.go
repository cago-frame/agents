package task

import (
	"fmt"
	"strings"
)

// formatList 渲染整张列表（含统计行），任意 task_* 工具都用这个回显给模型。
func formatList(items []Item) string {
	if len(items) == 0 {
		return "[empty task list]"
	}
	var b strings.Builder
	var pending, inProgress, completed int
	for _, it := range items {
		b.WriteString(formatItem(it))
		b.WriteByte('\n')
		switch it.Status {
		case StatusPending:
			pending++
		case StatusInProgress:
			inProgress++
		case StatusCompleted:
			completed++
		}
	}
	fmt.Fprintf(&b, "\n%d pending, %d in progress, %d completed (total %d)",
		pending, inProgress, completed, len(items))
	return b.String()
}

// formatItem 渲染单条。in_progress 优先用 activeForm。
func formatItem(it Item) string {
	prefix := "☐"
	switch it.Status {
	case StatusInProgress:
		prefix = "▶"
	case StatusCompleted:
		prefix = "✓"
	}
	text := it.Content
	if it.Status == StatusInProgress && it.ActiveForm != "" {
		text = it.ActiveForm
	}
	out := fmt.Sprintf("%s %s %s", it.ID, prefix, text)
	if it.Priority != "" {
		out += " [" + string(it.Priority) + "]"
	}
	return out
}

// formatItems 渲染若干条（不带统计行），用于 create/update 的回显（只列出受影响项）。
func formatItems(items []Item) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	for _, it := range items {
		b.WriteString(formatItem(it))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}
