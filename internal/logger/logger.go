package logger

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	// Logger is the global logger instance
	Logger zerolog.Logger
)

func init() {
	// Initialize with a default logger (info level, console output)
	// Can be reconfigured later with Init()
	Logger = zerolog.New(os.Stdout).
		With().
		Timestamp().
		Caller().
		Logger()
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	log.Logger = Logger
}

// LogLevel represents the logging level
type LogLevel string

const (
	DebugLevel LogLevel = "debug"
	InfoLevel  LogLevel = "info"
	WarnLevel  LogLevel = "warn"
	ErrorLevel LogLevel = "error"
)

// Init initializes the global logger with the specified level and output
func Init(level string, pretty bool) {
	// Parse log level
	var zlLevel zerolog.Level
	switch strings.ToLower(level) {
	case "debug":
		zlLevel = zerolog.DebugLevel
	case "info":
		zlLevel = zerolog.InfoLevel
	case "warn", "warning":
		zlLevel = zerolog.WarnLevel
	case "error":
		zlLevel = zerolog.ErrorLevel
	default:
		zlLevel = zerolog.InfoLevel
	}

	// Set global log level
	zerolog.SetGlobalLevel(zlLevel)

	// Configure output
	var output io.Writer = os.Stdout
	if pretty {
		output = zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
			NoColor:    false,
		}
	}

	// Create logger
	Logger = zerolog.New(output).
		With().
		Timestamp().
		Caller().
		Logger()

	// Set as global logger
	log.Logger = Logger
}

// Get returns the global logger instance
func Get() *zerolog.Logger {
	return &Logger
}

// WithComponent returns a logger with a component field set
func WithComponent(component string) *zerolog.Logger {
	l := Logger.With().Str("component", component).Logger()
	return &l
}

// WithField adds a custom field to the logger
func WithField(key string, value interface{}) *zerolog.Logger {
	l := Logger.With().Interface(key, value).Logger()
	return &l
}

// Debug logs a debug message
func Debug(msg string) {
	Logger.Debug().Msg(msg)
}

// Info logs an info message
func Info(msg string) {
	Logger.Info().Msg(msg)
}

// Warn logs a warning message
func Warn(msg string) {
	Logger.Warn().Msg(msg)
}

// Error logs an error message
func Error(msg string) {
	Logger.Error().Msg(msg)
}

// Fatal logs a fatal message and exits
func Fatal(msg string) {
	Logger.Fatal().Msg(msg)
}
