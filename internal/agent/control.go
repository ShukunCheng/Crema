package agent

import (
	"encoding/json"
	"errors"
	"fmt"
)

// A held-open conversation takes more than prompts on its stdin. The CLI's
// stream-json input also carries control requests — measured from its own
// bundle (2.1.259): interrupt, set_model, set_permission_mode, stop_task,
// among others — each answered by a control_response line naming the request.
//
// These are what make a live process cheap to keep: cancelling a turn no
// longer means killing the process and rebuilding the conversation's cache
// on the next message, and neither does changing the model or the permission
// mode. And stop_task is a thing crema can honestly offer for background
// work, which it could not before — a "stop" with nothing behind it would
// have been a lie in the shape of a key.

// Controller is the optional face of a Conversation that takes control
// requests. Each call returns the request id the CLI will answer with, so the
// caller can match the reply — a KindControl event — to what it asked.
type Controller interface {
	// Interrupt stops the turn in flight. The CLI ends it with a result line
	// as usual, so the turn's lifecycle is unchanged; only the process lives.
	Interrupt() (requestID string, err error)
	// SetModel switches the model for the turns that follow. DefaultModel
	// asks for the CLI's own default.
	SetModel(model string) (requestID string, err error)
	// SetPermission switches the permission mode for the turns that follow.
	SetPermission(p PermissionMode) (requestID string, err error)
	// StopTask stops one background task by the CLI's own id for it.
	StopTask(taskID string) (requestID string, err error)
}

// ControlReply is the CLI's answer to a control request. KindControl only.
type ControlReply struct {
	RequestID string
	OK        bool
	Err       string          // when !OK: the CLI's own words
	Response  json.RawMessage // when OK: whatever the request returns, if anything
}

// controlRequest is the wire shape: an envelope naming the request, and the
// request itself with its subtype and fields.
func controlRequest(id, subtype string, fields map[string]any) ([]byte, error) {
	req := map[string]any{"subtype": subtype}
	for k, v := range fields {
		req[k] = v
	}
	return json.Marshal(map[string]any{
		"type":       "control_request",
		"request_id": id,
		"request":    req,
	})
}

// control writes one request down the conversation's stdin.
func (cv *claudeConv) control(subtype string, fields map[string]any) (string, error) {
	cv.mu.Lock()
	defer cv.mu.Unlock()
	if cv.closed {
		return "", errors.New("conversation is closed")
	}
	cv.reqN++
	id := fmt.Sprintf("crema-%d", cv.reqN)
	b, err := controlRequest(id, subtype, fields)
	if err != nil {
		return "", err
	}
	if _, err := cv.stdin.Write(append(b, '\n')); err != nil {
		return "", err
	}
	return id, nil
}

func (cv *claudeConv) Interrupt() (string, error) {
	return cv.control("interrupt", nil)
}

func (cv *claudeConv) SetModel(model string) (string, error) {
	var m any // null is how the CLI is asked for its own default
	if model != DefaultModel {
		m = model
	}
	return cv.control("set_model", map[string]any{"model": m})
}

func (cv *claudeConv) SetPermission(p PermissionMode) (string, error) {
	mode := claudeMode(p)
	if mode == "" {
		mode = "default"
	}
	return cv.control("set_permission_mode", map[string]any{"mode": mode})
}

func (cv *claudeConv) StopTask(taskID string) (string, error) {
	return cv.control("stop_task", map[string]any{"task_id": taskID})
}
