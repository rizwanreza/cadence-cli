package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// NormalizeArgs rewrites Go-style single-dash long flags (-goal, -json,
// -dry-run=true) to the double-dash form cobra/pflag expect (--goal ...).
//
// Earlier releases used the standard library flag package, so scripts and
// agents in the wild pass single-dash long flags. pflag would read "-goal" as
// the shorthand cluster -g -o -a -l, so we translate before parsing. To keep
// values safe, an argument is rewritten only when:
//   - its name (before any "=") is a long flag registered somewhere in the
//     command tree (so "-1" or "-foo" as a value is left alone),
//   - it is not the value of the preceding flag that takes a value
//     (so `-notes -json` keeps "-json" as the notes text), and
//   - it appears before a bare "--" terminator.
func NormalizeArgs(root *cobra.Command, args []string) []string {
	takesValue := flagTable(root)
	out := make([]string, 0, len(args))
	expectValue := false
	for i, arg := range args {
		if arg == "--" {
			out = append(out, args[i:]...)
			return out
		}
		if expectValue {
			out = append(out, arg)
			expectValue = false
			continue
		}
		name, hasValue := flagName(arg)
		if name == "" {
			out = append(out, arg)
			continue
		}
		valued, known := takesValue[name]
		if !known {
			out = append(out, arg)
			continue
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && len(name) > 1 {
			arg = "-" + arg
		}
		out = append(out, arg)
		expectValue = valued && !hasValue
	}
	return out
}

// flagName extracts the flag name from "-name", "--name" or "-name=value".
func flagName(arg string) (string, bool) {
	if !strings.HasPrefix(arg, "-") || arg == "-" {
		return "", false
	}
	trimmed := strings.TrimLeft(arg, "-")
	if len(arg)-len(trimmed) > 2 || trimmed == "" {
		return "", false
	}
	name, _, hasValue := strings.Cut(trimmed, "=")
	if name == "" || !isLetter(name[0]) {
		return "", false
	}
	return name, hasValue
}

// flagTable maps every long flag name in the tree to whether it takes a
// value. A name that is boolean anywhere is treated as boolean, which is the
// safe choice: it never swallows the next argument.
func flagTable(root *cobra.Command) map[string]bool {
	table := map[string]bool{"help": false, "version": false}
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		visit := func(f *pflag.Flag) {
			valued := f.NoOptDefVal == ""
			if prev, ok := table[f.Name]; ok {
				table[f.Name] = prev && valued
			} else {
				table[f.Name] = valued
			}
		}
		c.LocalFlags().VisitAll(visit)
		c.PersistentFlags().VisitAll(visit)
		for _, child := range c.Commands() {
			walk(child)
		}
	}
	walk(root)
	return table
}

func isLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
