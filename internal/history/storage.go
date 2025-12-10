package history

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const (
	historyFileName = "history.jsonl"
	defaultMaxEntries = 10000
)

var (
	// ErrNoHistory is returned when history file doesn't exist
	ErrNoHistory = errors.New("no history file found")

	// historyMu protects concurrent access to history file
	historyMu sync.Mutex
)

// StoragePath returns the path to the history file.
// Uses XDG_DATA_HOME if set, otherwise ~/.local/share/ntm/history.jsonl
func StoragePath() string {
	dataDir := os.Getenv("XDG_DATA_HOME")
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return historyFileName // fallback to current dir
		}
		dataDir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataDir, "ntm", historyFileName)
}

// Append adds an entry to the history file.
// Thread-safe and atomic (writes full line or nothing).
func Append(entry *HistoryEntry) error {
	historyMu.Lock()
	defer historyMu.Unlock()

	path := StoragePath()

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	// Write line with newline atomically
	_, err = f.Write(append(data, '\n'))
	return err
}

// ReadAll reads all history entries from the file.
// Returns empty slice if file doesn't exist.
func ReadAll() ([]HistoryEntry, error) {
	historyMu.Lock()
	defer historyMu.Unlock()

	return readAllLocked()
}

// readAllLocked reads all entries (caller must hold lock)
func readAllLocked() ([]HistoryEntry, error) {
	path := StoragePath()

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []HistoryEntry{}, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []HistoryEntry
	scanner := bufio.NewScanner(f)
	// Set max line size for large prompts
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		var entry HistoryEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			// Skip malformed lines
			continue
		}
		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return entries, err
	}

	return entries, nil
}

// ReadRecent reads the last n entries efficiently.
// Reads from end of file when possible.
func ReadRecent(n int) ([]HistoryEntry, error) {
	historyMu.Lock()
	defer historyMu.Unlock()

	// For simplicity, read all and return last n
	// Could be optimized with tail-reading for large files
	entries, err := readAllLocked()
	if err != nil {
		return nil, err
	}

	if len(entries) <= n {
		return entries, nil
	}

	return entries[len(entries)-n:], nil
}

// ReadForSession reads entries for a specific session.
func ReadForSession(session string) ([]HistoryEntry, error) {
	entries, err := ReadAll()
	if err != nil {
		return nil, err
	}

	var result []HistoryEntry
	for _, e := range entries {
		if e.Session == session {
			result = append(result, e)
		}
	}
	return result, nil
}

// Count returns the number of entries in history.
func Count() (int, error) {
	historyMu.Lock()
	defer historyMu.Unlock()

	path := StoragePath()

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		count++
	}

	return count, scanner.Err()
}

// Clear removes all history.
func Clear() error {
	historyMu.Lock()
	defer historyMu.Unlock()

	path := StoragePath()
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Prune keeps only the last n entries, removing older ones.
func Prune(keep int) (int, error) {
	historyMu.Lock()
	defer historyMu.Unlock()

	entries, err := readAllLocked()
	if err != nil {
		return 0, err
	}

	if len(entries) <= keep {
		return 0, nil // nothing to prune
	}

	// Keep only recent entries
	toKeep := entries[len(entries)-keep:]
	removed := len(entries) - keep

	// Rewrite file
	path := StoragePath()
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	writer := bufio.NewWriter(f)
	for _, entry := range toKeep {
		data, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		writer.Write(data)
		writer.WriteByte('\n')
	}

	return removed, writer.Flush()
}

// Search finds entries matching a query string in the prompt.
func Search(query string) ([]HistoryEntry, error) {
	entries, err := ReadAll()
	if err != nil {
		return nil, err
	}

	var result []HistoryEntry
	for _, e := range entries {
		if containsIgnoreCase(e.Prompt, query) {
			result = append(result, e)
		}
	}
	return result, nil
}

// containsIgnoreCase checks if s contains substr (case-insensitive)
func containsIgnoreCase(s, substr string) bool {
	// Simple implementation - could be optimized
	sLower := toLower(s)
	substrLower := toLower(substr)

	for i := 0; i <= len(sLower)-len(substrLower); i++ {
		if sLower[i:i+len(substrLower)] == substrLower {
			return true
		}
	}
	return false
}

// toLower converts ASCII letters to lowercase
func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// Exists checks if history file exists and has content.
func Exists() bool {
	path := StoragePath()
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Size() > 0
}

// ExportTo writes history to a specific file.
func ExportTo(path string) error {
	historyMu.Lock()
	defer historyMu.Unlock()

	entries, err := readAllLocked()
	if err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	writer := bufio.NewWriter(f)
	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		writer.Write(data)
		writer.WriteByte('\n')
	}

	return writer.Flush()
}

// ImportFrom reads history from a specific file and appends to current history.
func ImportFrom(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	imported := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		var entry HistoryEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if err := Append(&entry); err != nil {
			return imported, err
		}
		imported++
	}

	return imported, scanner.Err()
}

// Stats returns summary statistics about history.
type Stats struct {
	TotalEntries    int `json:"total_entries"`
	SuccessCount    int `json:"success_count"`
	FailureCount    int `json:"failure_count"`
	UniqueSessions  int `json:"unique_sessions"`
	FileSizeBytes   int64 `json:"file_size_bytes"`
}

// GetStats returns history statistics.
func GetStats() (*Stats, error) {
	entries, err := ReadAll()
	if err != nil {
		return nil, err
	}

	stats := &Stats{
		TotalEntries: len(entries),
	}

	sessions := make(map[string]bool)
	for _, e := range entries {
		if e.Success {
			stats.SuccessCount++
		} else {
			stats.FailureCount++
		}
		sessions[e.Session] = true
	}
	stats.UniqueSessions = len(sessions)

	// Get file size
	path := StoragePath()
	if info, err := os.Stat(path); err == nil {
		stats.FileSizeBytes = info.Size()
	}

	return stats, nil
}

// ensure we use io package
var _ io.Reader = (*os.File)(nil)
