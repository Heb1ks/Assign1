package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type JSONLogger struct {
	writer io.Writer
}

type LogEntry struct {
	Time    string      `json:"time"`
	Level   string      `json:"level"`
	Message string      `json:"message"`
	Event   interface{} `json:"event,omitempty"`
	Error   string      `json:"error,omitempty"`
}

func New(w io.Writer) *JSONLogger {
	return &JSONLogger{writer: w}
}

func (l *JSONLogger) Info(msg string, event interface{}) {
	entry := LogEntry{
		Time:    time.Now().RFC3339,
		Level:   "INFO",
		Message: msg,
		Event:   event,
	}
	l.write(entry)
}

func (l *JSONLogger) Error(msg string, err error) {
	entry := LogEntry{
		Time:    time.Now().RFC3339,
		Level:   "ERROR",
		Message: msg,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	l.write(entry)
}

func (l *JSONLogger) Warn(msg string) {
	entry := LogEntry{
		Time:    time.Now().RFC3339,
		Level:   "WARN",
		Message: msg,
	}
	l.write(entry)
}

func (l *JSONLogger) write(entry LogEntry) {
	data, _ := json.Marshal(entry)
	fmt.Fprintln(l.writer, string(data))
}
