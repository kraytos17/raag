package logger

import (
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
)

var logger *log.Logger

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

func With(args ...any) *log.Logger {
	return logger.With(args...)
}

func Debugf(format string, args ...any) {
	logger.Debugf(format, args...)
}

func Infof(format string, args ...any) {
	logger.Infof(format, args...)
}

func Warnf(format string, args ...any) {
	logger.Warnf(format, args...)
}

func Errorf(format string, args ...any) {
	logger.Errorf(format, args...)
}

// Err logs an error message with the given error if not nil
func Err(msg string, err error) {
	if err != nil {
		logger.Errorf("%s error=%v", msg, err)
	}
}

// WarnErr logs a warning message with the given error if not nil
func WarnErr(msg string, err error) {
	if err != nil {
		logger.Warnf("%s error=%v", msg, err)
	}
}
