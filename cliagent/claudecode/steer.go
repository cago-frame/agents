package claudecode

import (
	"encoding/json"
	"fmt"
	"io"
)

// userFrameBytes 把 text 序列化成一帧 stream-json `user` frame(尾部带换行,
// 直接写入 claude stdin)。process.go / steer.go 共用,确保 wire 格式只有一处定义。
func userFrameBytes(text string) ([]byte, error) {
	frame := map[string]any{
		"type": string(frameTypeUser),
		"message": map[string]any{
			"role":    "user",
			"content": text,
		},
	}
	data, err := json.Marshal(frame)
	if err != nil {
		return nil, fmt.Errorf("claudecode: marshal user frame: %w", err)
	}
	return append(data, '\n'), nil
}

// writeUserFrame writes one stream-json `user` frame to the live process stdin.
func writeUserFrame(stdin io.Writer, text string) error {
	data, err := userFrameBytes(text)
	if err != nil {
		return err
	}
	if _, werr := stdin.Write(data); werr != nil {
		return fmt.Errorf("claudecode: write user frame: %w", werr)
	}
	return nil
}

// permissionResponseFrameBytes builds a control_response frame for
// --permission-prompt-tool stdio mode.
func permissionResponseFrameBytes(id, decision string) ([]byte, error) {
	frame := map[string]any{
		"type":     "control_response",
		"id":       id,
		"decision": decision,
	}
	data, err := json.Marshal(frame)
	if err != nil {
		return nil, fmt.Errorf("claudecode: marshal control_response: %w", err)
	}
	return append(data, '\n'), nil
}

// writePermissionResponse writes a control_response frame to stdin.
func writePermissionResponse(stdin io.Writer, id string, allow bool) error {
	decision := "deny"
	if allow {
		decision = "allow"
	}
	data, err := permissionResponseFrameBytes(id, decision)
	if err != nil {
		return err
	}
	if _, werr := stdin.Write(data); werr != nil {
		return fmt.Errorf("claudecode: write control_response: %w", werr)
	}
	return nil
}
