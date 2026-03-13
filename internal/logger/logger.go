package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
)

var (
	logger    *log.Logger
	jsonMode  bool
	jsonMutex sync.RWMutex
)

type JSONLogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

func init() {
	NewLogger("raag")
}

func NewLogger(prefix string) {
	logger = log.NewWithOptions(os.Stderr, log.Options{
		ReportTimestamp: true,
		TimeFormat:      "15:04:05",
		Prefix:          prefix,
	})

	styles := log.DefaultStyles()
	styles.Levels[log.DebugLevel] = lipgloss.NewStyle().
		SetString("DEBUG").
		Foreground(lipgloss.Color("247"))
	styles.Levels[log.InfoLevel] = lipgloss.NewStyle().
		SetString("INFO").
		Foreground(lipgloss.Color("75"))
	styles.Levels[log.WarnLevel] = lipgloss.NewStyle().
		SetString("WARN").
		Foreground(lipgloss.Color("226"))
	styles.Levels[log.ErrorLevel] = lipgloss.NewStyle().
		SetString("ERROR").
		Foreground(lipgloss.Color("196"))
	styles.Levels[log.FatalLevel] = lipgloss.NewStyle().
		SetString("FATAL").
		Foreground(lipgloss.Color("199"))

	logger.SetStyles(styles)
	logger.SetLevel(log.InfoLevel)
}

func SetLevel(level string) {
	if logger == nil {
		NewLogger("raag")
	}

	switch strings.ToLower(level) {
	case "debug":
		logger.SetLevel(log.DebugLevel)
	case "warn", "warning":
		logger.SetLevel(log.WarnLevel)
	case "error":
		logger.SetLevel(log.ErrorLevel)
	case "fatal":
		logger.SetLevel(log.FatalLevel)
	default:
		logger.SetLevel(log.InfoLevel)
	}
}

func SetJSONMode(enabled bool) {
	jsonMutex.Lock()
	defer jsonMutex.Unlock()
	jsonMode = enabled
}

func IsJSONMode() bool {
	jsonMutex.RLock()
	defer jsonMutex.RUnlock()
	return jsonMode
}

func With(args ...any) *log.Logger {
	return logger.With(args...)
}

func Debugf(format string, args ...any) {
	logJSON(log.DebugLevel, "DEBUG", format, args...)
}

func Infof(format string, args ...any) {
	logJSON(log.InfoLevel, "INFO", format, args...)
}

func Warnf(format string, args ...any) {
	logJSON(log.WarnLevel, "WARN", format, args...)
}

func Errorf(format string, args ...any) {
	logJSON(log.ErrorLevel, "ERROR", format, args...)
}

func logJSON(level log.Level, levelStr, format string, args ...any) {
	jsonMutex.RLock()
	isJSON := jsonMode
	jsonMutex.RUnlock()

	if isJSON {
		msg := format
		if len(args) > 0 {
			msg = fmt.Sprintf(format, args...)
		}

		entry := JSONLogEntry{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Level:     levelStr,
			Message:   msg,
		}
		jsonBytes, _ := json.Marshal(entry)
		os.Stderr.Write(append(jsonBytes, '\n'))
		return
	}

	switch level {
	case log.DebugLevel:
		logger.Debugf(format, args...)
	case log.InfoLevel:
		logger.Infof(format, args...)
	case log.WarnLevel:
		logger.Warnf(format, args...)
	case log.ErrorLevel:
		logger.Errorf(format, args...)
	}
}
