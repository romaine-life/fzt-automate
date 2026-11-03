package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// A built-in is an at command fzt-automate runs itself, with no process
// (FZT-71). A menu entry names one as the first word of its action
// ("lights off 30", "folder:windows"); the same name is a subcommand
// (`fzt-automate lights off 30`).
type builtin struct {
	name  string
	usage string // its arguments, for the usage line
	about string
	// terminal: it talks to the person (a picker, a prompt, a sign-in), so a
	// pick opens it as a takeover running `fzt-automate <name> ...`.
	terminal bool
	// run does the command and returns the sentence it ends with. out is
	// where a terminal built-in writes as it goes.
	run func(ctx context.Context, out io.Writer, args []string) (string, error)
	// rows fills a command folder: set for a built-in that lists something.
	rows func(ctx context.Context, args []string) ([]folderRow, error)
}

// folderRow is one child of a command folder.
type folderRow struct {
	name, description, action string
}

var builtins = map[string]*builtin{}

func register(b *builtin) {
	if _, dup := builtins[b.name]; dup {
		panic("two built-ins are named " + b.name)
	}
	builtins[b.name] = b
}

// usageError is a built-in given the wrong arguments.
func usageError(b *builtin) error {
	return fmt.Errorf("usage: %s %s", b.name, b.usage)
}

// runSubcommand runs a built-in from the command line: what it writes and
// its result go to stdout, a failure to stderr with exit 1.
func runSubcommand(b *builtin, args []string) int {
	// From a prompt, or as a takeover tmux opened in the tab's folder: here.
	dir, _ := os.Getwd()
	ctx := withDir(context.Background(), dir)
	if b.rows != nil {
		rows, err := b.rows(ctx, args)
		if err != nil {
			fmt.Fprintln(os.Stderr, logFailure("", b.name, dir, fmt.Sprintf("%s: %v", b.name, err)))
			return 1
		}
		for _, r := range rows {
			fmt.Printf("%s\t%s\t%s\n", r.name, r.description, r.action)
		}
		return 0
	}
	result, err := b.run(ctx, os.Stdout, args)
	if err != nil {
		fmt.Fprintln(os.Stderr, logFailure("", b.name, dir, fmt.Sprintf("%s: %v", b.name, err)))
		return 1
	}
	if result != "" {
		fmt.Println(result)
	}
	return 0
}

// builtinList is every built-in's usage line, for --help.
func builtinList() string {
	names := make([]string, 0, len(builtins))
	for n := range builtins {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		bi := builtins[n]
		fmt.Fprintf(&b, "  %-40s %s\n", strings.TrimSpace(n+" "+bi.usage), bi.about)
	}
	return b.String()
}
