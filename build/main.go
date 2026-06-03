// Build wrapper for fzt-automate.
//
// Plain `go build .` produces a broken binary because main.go's render.Version
// check exits if the version wasn't injected via ldflags. This wrapper computes
// the right ldflags (binary version from `git describe`, fzt engine version
// from go.mod) and runs `go build`.
//
// Usage:
//
//	go run ./build              -- build to ./fzt-automate(.exe)
//	go run ./build install      -- also copy to ~/bin (or %USERPROFILE%/bin)
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func main() {
	binVersion := gitDescribe(".")
	engineVersion := goModVersion("github.com/romaine-life/fzt")

	binName := "fzt-automate"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}

	ldflags := fmt.Sprintf(
		"-X github.com/romaine-life/fzt/render.Version=%s -X github.com/romaine-life/fzt-frontend.EngineVersion=%s",
		binVersion, engineVersion,
	)

	cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", binName, ".")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build failed:", err)
		os.Exit(1)
	}

	fmt.Printf("Built %s (version=%s, engine=%s)\n", binName, binVersion, engineVersion)

	if len(os.Args) > 1 && os.Args[1] == "install" {
		dest := installDest(binName)
		if err := install(binName, dest); err != nil {
			fmt.Fprintln(os.Stderr, "install failed:", err)
			os.Exit(1)
		}
		fmt.Printf("Installed to %s\n", dest)
	}
}

// install copies src to dst, working around Windows' inability to overwrite
// a running .exe. Renames the existing file aside (which Windows allows even
// when locked), copies the new one in, then deletes the renamed-aside file
// (which may itself be locked — left for next install to clean up).
func install(src, dst string) error {
	oldPath := dst + ".old"
	_ = os.Remove(oldPath)
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, oldPath); err != nil {
			return fmt.Errorf("rename existing aside: %w", err)
		}
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	_ = os.Remove(oldPath)
	return nil
}

func gitDescribe(dir string) string {
	out, err := exec.Command("git", "-C", dir, "describe", "--tags", "--always", "--dirty").Output()
	if err != nil {
		return "dev"
	}
	return strings.TrimSpace(string(out))
}

func goModVersion(modulePath string) string {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return "dev"
	}
	prefix := modulePath + " "
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}
	return "dev"
}

func installDest(binName string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("USERPROFILE"), "bin", binName)
	}
	return filepath.Join(os.Getenv("HOME"), "bin", binName)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}
