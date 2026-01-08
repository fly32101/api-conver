package service

import (
	"strconv"
	"time"
)

// GenerateToolCallID generates a unique tool call ID
func GenerateToolCallID() string {
	return "call_" + strconv.FormatInt(time.Now().UnixNano(), 10)
}
