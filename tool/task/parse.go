package task

import "fmt"

// parseStatus 把字符串解析成 Status，空串报错（status 是必填）。
func parseStatus(s string) (Status, error) {
	switch Status(s) {
	case StatusPending, StatusInProgress, StatusCompleted:
		return Status(s), nil
	}
	return "", fmt.Errorf("invalid status %q (want pending|in_progress|completed)", s)
}

// parsePriority 把字符串解析成 Priority；空串返回空（priority 可选）。
func parsePriority(s string) (Priority, error) {
	if s == "" {
		return "", nil
	}
	switch Priority(s) {
	case PriorityHigh, PriorityMedium, PriorityLow:
		return Priority(s), nil
	}
	return "", fmt.Errorf("invalid priority %q (want high|medium|low)", s)
}
