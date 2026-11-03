package main

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"
)

// The test binary stands in for the programs an entry names: with
// FZT_TEST_HELPER set it runs one of these instead of the tests.
//
//	print <line>...      each line on stdout
//	exit <code>          exit with code
//	sleep <seconds>      wait, then exit 0
func TestMain(m *testing.M) {
	if os.Getenv("FZT_TEST_HELPER") != "" {
		os.Exit(helper(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func helper(args []string) int {
	if len(args) == 0 {
		return 2
	}
	switch args[0] {
	case "failure-log":
		fmt.Println(logFailure("", "test program", "test folder", args[1]))
		return 0
	case "print":
		for _, l := range args[1:] {
			fmt.Println(l)
		}
		return 0
	case "exit":
		n, _ := strconv.Atoi(args[1])
		return n
	case "sleep":
		n, _ := strconv.Atoi(args[1])
		time.Sleep(time.Duration(n) * time.Second)
		return 0
	}
	return 2
}

// helperLine is an action line that runs the test binary as a helper program.
func helperLine(t *testing.T, args ...string) string {
	t.Helper()
	t.Setenv("FZT_TEST_HELPER", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return joinArgs(append([]string{exe}, args...)...)
}
