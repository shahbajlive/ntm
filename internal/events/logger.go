package events

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// DefaultLogPath is the default location for the events log.
	DefaultLogPath = "~/.config/ntm/analytics/events.jsonl"

	// DefaultRetentionDays is the number of days to retain log entries.
	DefaultRetentionDays = 30

	// RotationCheckInterval is how often to check for rotation (in events).
	RotationCheckInterval = 100
)

// Logger writes events to a JSONL file with automatic rotation.
type Logger struct {
	path           string
	retentionDays  int
	enabled        bool
	mu             sync.Mutex
	file           *os.File
	eventCount     int
	lastRotation   time.Time
}

// LoggerOptions configures the event logger.
type LoggerOptions struct {
	Path          string
	RetentionDays int
	Enabled       bool
}

// DefaultOptions returns the default logger options.
func DefaultOptions() LoggerOptions {
	return LoggerOptions{
		Path:          expandPath(DefaultLogPath),
		RetentionDays: DefaultRetentionDays,
		Enabled:       true,
	}
}

// NewLogger creates a new event logger.
func NewLogger(opts LoggerOptions) (*Logger, error) {
	if opts.Path == "" {
		opts.Path = expandPath(DefaultLogPath)
	}
	if opts.RetentionDays == 0 {
		opts.RetentionDays = DefaultRetentionDays
	}

	l := &Logger{
		path:          opts.Path,
		retentionDays: opts.RetentionDays,
		enabled:       opts.Enabled,
		lastRotation:  time.Now(),
	}

	if !l.enabled {
		return l, nil
	}

	// Ensure directory exists
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating log directory: %w", err)
	}

	// Open file for appending
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening log file: %w", err)
	}
	l.file = f

	return l, nil
}

// Log writes an event to the log file.
func (l *Logger) Log(event *Event) error {
	if !l.enabled || l.file == nil {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Serialize event to JSON
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshaling event: %w", err)
	}

	// Write to file with newline
	if _, err := l.file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing event: %w", err)
	}

	l.eventCount++

	// Check for rotation periodically
	if l.eventCount%RotationCheckInterval == 0 {
		go l.maybeRotate()
	}

	return nil
}

// LogEvent is a convenience method to create and log an event in one call.
func (l *Logger) LogEvent(eventType EventType, session string, data interface{}) error {
	event := NewEvent(eventType, session, ToMap(data))
	return l.Log(event)
}

// Close closes the log file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		err := l.file.Close()
		l.file = nil
		return err
	}
	return nil
}

// maybeRotate checks if rotation is needed and performs it.
func (l *Logger) maybeRotate() {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Only rotate once per day at most (check under lock to avoid TOCTOU)
	if time.Since(l.lastRotation) < 24*time.Hour {
		return
	}

	l.lastRotation = time.Now()

	// Read existing file and filter old entries
	if err := l.rotateOldEntries(); err != nil {
		// Log rotation errors but don't fail
		fmt.Fprintf(os.Stderr, "event log rotation error: %v\n", err)
	}
}

// rotateOldEntries removes entries older than retention period.
func (l *Logger) rotateOldEntries() error {
	// Close current file
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}

	// Read all entries
	data, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	// Parse and filter entries
	cutoff := time.Now().AddDate(0, 0, -l.retentionDays)
	var kept []byte

	for _, line := range splitLines(data) {
		if len(line) == 0 {
			continue
		}

		var event Event
		if err := json.Unmarshal(line, &event); err != nil {
			// Keep malformed entries
			kept = append(kept, line...)
			kept = append(kept, '\n')
			continue
		}

		if event.Timestamp.After(cutoff) {
			kept = append(kept, line...)
			kept = append(kept, '\n')
		}
	}

	// Write filtered entries back
	if err := os.WriteFile(l.path, kept, 0644); err != nil {
		return err
	}

	// Reopen file for appending
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	l.file = f

	return nil
}

// splitLines splits data into lines without allocating new strings.
func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

// expandPath expands ~ in a path to the home directory.
func expandPath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}

// Global logger instance
var (
	globalLogger     *Logger
	globalLoggerOnce sync.Once
)

// DefaultLogger returns the global default logger instance.
func DefaultLogger() *Logger {
	globalLoggerOnce.Do(func() {
		var err error
		globalLogger, err = NewLogger(DefaultOptions())
		if err != nil {
			// If we can't create the logger, create a disabled one
			globalLogger = &Logger{enabled: false}
		}
	})
	return globalLogger
}

// Emit logs an event using the default logger.
func Emit(eventType EventType, session string, data interface{}) {
	DefaultLogger().LogEvent(eventType, session, data)
}

// EmitSessionCreate logs a session creation event.
func EmitSessionCreate(session string, claudeCount, codexCount, geminiCount int, workDir, recipe string) {
	Emit(EventSessionCreate, session, SessionCreateData{
		ClaudeCount: claudeCount,
		CodexCount:  codexCount,
		GeminiCount: geminiCount,
		WorkDir:     workDir,
		Recipe:      recipe,
	})
}

// EmitPromptSend logs a prompt send event.
func EmitPromptSend(session string, targetCount, promptLength int, template, targetTypes string, hasContext bool) {
	// Estimate tokens based on prompt length (using ~3.5 chars/token heuristic)
	estimatedTokens := promptLength * 10 / 35

	Emit(EventPromptSend, session, PromptSendData{
		TargetCount:     targetCount,
		PromptLength:    promptLength,
		Template:        template,
		TargetTypes:     targetTypes,
		HasContext:      hasContext,
		EstimatedTokens: estimatedTokens,
	})
}

// EmitError logs an error event.
func EmitError(session, errorType, message string) {
	Emit(EventError, session, ErrorData{
		ErrorType: errorType,
		Message:   message,
	})
}
