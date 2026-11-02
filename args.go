package main

import (
	"fmt"
	"os"
	"strings"
)

// splitArgs reads a menu entry's action as an argument list, with no shell:
// words split on spaces, and a quoted stretch ('…' or "…") is one word with
// the quotes removed. Inside quotes, the quote character twice is one of it,
// as in PowerShell. Nothing else is special: a backslash is a path separator.
func splitArgs(line string) ([]string, error) {
	var args []string
	var word strings.Builder
	inWord := false
	var quote rune
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case quote != 0:
			if r == quote {
				if i+1 < len(runes) && runes[i+1] == quote {
					word.WriteRune(r)
					i++
					continue
				}
				quote = 0
				continue
			}
			word.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
			inWord = true
		case r == ' ' || r == '\t':
			if inWord {
				args = append(args, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("a %c quote is never closed", quote)
	}
	if inWord {
		args = append(args, word.String())
	}
	return args, nil
}

// quoteArg makes text one word for splitArgs.
func quoteArg(text string) string {
	if text != "" && !strings.ContainsAny(text, " \t'\"") {
		return text
	}
	return "'" + strings.ReplaceAll(text, "'", "''") + "'"
}

// joinArgs is the action line that splitArgs reads back as args.
func joinArgs(args ...string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = quoteArg(a)
	}
	return strings.Join(quoted, " ")
}

// expandArg resolves what a word names: ${NAME} is an environment variable,
// tool:<name> a published tool's program and tool:<name>/<file> a file in its
// current release, tool-config:<name> the tool's shared settings folder.
func expandArg(arg string) (string, error) {
	var missing []string
	arg = expandBraces(arg, func(name string) string {
		v, ok := os.LookupEnv(name)
		if !ok {
			missing = append(missing, name)
		}
		return v
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("%s is not set", strings.Join(missing, ", "))
	}
	switch {
	case strings.HasPrefix(arg, "tool-config:"):
		return toolConfigDir(strings.TrimPrefix(arg, "tool-config:"))
	case strings.HasPrefix(arg, "tool:"):
		name, file, _ := strings.Cut(strings.TrimPrefix(arg, "tool:"), "/")
		return toolPath(name, file)
	}
	return arg, nil
}

// expandBraces replaces each ${NAME}; a lone $ stays as it is.
func expandBraces(s string, lookup func(string) string) string {
	var b strings.Builder
	for {
		start := strings.Index(s, "${")
		if start < 0 {
			b.WriteString(s)
			return b.String()
		}
		end := strings.IndexByte(s[start:], '}')
		if end < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:start])
		b.WriteString(lookup(s[start+2 : start+end]))
		s = s[start+end+1:]
	}
}

func expandArgs(args []string) ([]string, error) {
	out := make([]string, len(args))
	for i, a := range args {
		v, err := expandArg(a)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}
