package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const failureLogBytes = 1024 * 1024

var failureLogMu sync.Mutex

// A process owns its two rolling files; simultaneous menus never rotate
// or truncate each other's history. Old processes' files expire after 30 days.
func logFailure(dir, name, cwd, message string) string {
	failureLogMu.Lock()
	defer failureLogMu.Unlock()
	if dir == "" {
		dir = configDir()
	}
	dir = filepath.Join(dir, "failures")
	err := appendFailure(dir, name, cwd, message)
	if err != nil {
		return message + "\nCouldn't save failure log: " + err.Error()
	}
	return message
}

func appendFailure(dir, name, cwd, message string) error {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	// Bound metadata too: an action line can be much larger than its label.
	if len(name) > 16*1024 {
		name = name[:16*1024] + " [truncated]"
	}
	if len(cwd) > 16*1024 {
		cwd = cwd[:16*1024] + " [truncated]"
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, fmt.Sprintf("at-%d.log", os.Getpid()))
	if _, err := os.Stat(path); os.IsNotExist(err) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), "at-") ||
				!(strings.HasSuffix(e.Name(), ".log") || strings.HasSuffix(e.Name(), ".log.1")) {
				continue
			}
			if info, err := e.Info(); err == nil && info.Mode().IsRegular() && time.Since(info.ModTime()) > 30*24*time.Hour {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	if len(message) > 64*1024 {
		message = message[:64*1024] + " [truncated]"
	}
	entry := struct {
		Time, Name, Directory, Error string
	}{time.Now().UTC().Format(time.RFC3339Nano), name, cwd, message}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if info, err := os.Stat(path); err == nil && info.Size()+int64(len(data)) > failureLogBytes {
		if err := os.Remove(path + ".1"); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Rename(path, path+".1"); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}
