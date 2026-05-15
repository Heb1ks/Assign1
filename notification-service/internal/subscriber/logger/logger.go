package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// JSONLogger пишет структурированные JSON-строки в указанный writer.
type JSONLogger struct {
	writer io.Writer
}

// LogEntry - строгая структура лог-записи по заданию.
type LogEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	JobID   string `json:"job_id,omitempty"`
	Attempt int    `json:"attempt,omitempty"`
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

func New(w io.Writer) *JSONLogger {
	return &JSONLogger{writer: w}
}

// Info пишет info-запись с произвольными полями.
func (l *JSONLogger) Info(msg string, extra interface{}) {
	l.writeRaw("info", msg, extra)
}

// Warn пишет предупреждение.
func (l *JSONLogger) Warn(msg string) {
	l.writeRaw("warn", msg, nil)
}

// Error пишет ошибку.
func (l *JSONLogger) Error(msg string, err error) {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	entry := map[string]interface{}{
		"time":  time.Now().Format(time.RFC3339),
		"level": "error",
		"msg":   msg,
	}
	if errStr != "" {
		entry["error"] = errStr
	}
	data, _ := json.Marshal(entry)
	fmt.Fprintln(l.writer, string(data))
}

// JobLog пишет запись по заданной спецификации (job lifecycle).
func (l *JSONLogger) JobLog(entry LogEntry) {
	entry.Time = time.Now().Format(time.RFC3339)
	data, _ := json.Marshal(entry)
	fmt.Fprintln(l.writer, string(data))
}

func (l *JSONLogger) writeRaw(level, msg string, extra interface{}) {
	entry := map[string]interface{}{
		"time":  time.Now().Format(time.RFC3339),
		"level": level,
		"msg":   msg,
	}
	if extra != nil {
		entry["data"] = extra
	}
	data, _ := json.Marshal(entry)
	fmt.Fprintln(l.writer, string(data))
}
