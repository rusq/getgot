package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

const logFileName = "getgot.log"

// defaultLogPath returns the default log file path in the user's cache
// directory, creating getgot's directory in it if needed.
func defaultLogPath() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("log directory: %w", err)
	}
	dir := filepath.Join(cache, "getgot")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("log directory: %w", err)
	}
	return filepath.Join(dir, logFileName), nil
}

// logPath returns the absolute log file path for the -log value name, or the
// default path if name is empty.  The path is made absolute here, so that all
// gotsr stages resolve it the same way.
func logPath(name string) (string, error) {
	if name == "" {
		return defaultLogPath()
	}
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", fmt.Errorf("log file: %w", err)
	}
	return abs, nil
}

// openLog opens the log file for appending, creating it with mode 0600.
func openLog(name string) (*os.File, error) {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("log file: %w", err)
	}
	return f, nil
}

// setLogOutput sends the default slog logger, and through it the standard
// log package, to w.
func setLogOutput(w io.Writer) {
	slog.SetDefault(slog.New(slog.NewTextHandler(w, nil)))
}
