package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/junfuchang/superflare/config/data"
)

// RunConfigCommand handles lifecycle operations before server initialization.
// Reads are safe under the shell's flock; config-login acquires its own lock.
func RunConfigCommand(args []string, input io.Reader, output io.Writer) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if args[0] == "config-login" {
		if len(args) != 1 {
			return true, fmt.Errorf("usage: superflare config-login (NUL-separated username and password on stdin)")
		}
		limit := data.LoginUserMaxBytes + data.LoginPassMaxBytes + 2
		raw, err := io.ReadAll(io.LimitReader(input, int64(limit+1)))
		if err != nil {
			return true, err
		}
		if len(raw) > limit {
			return true, fmt.Errorf("login credentials exceed the size limit")
		}
		values := strings.Split(string(raw), "\x00")
		if len(values) != 3 || values[2] != "" {
			return true, fmt.Errorf("expected NUL-separated username and password")
		}
		return true, data.UpdateLoginConfig(values[0], values[1])
	}
	if args[0] != "config-read" {
		return false, nil
	}
	if len(args) != 4 || (args[1] != "yaml" && args[1] != "env") || args[3] == "" {
		return true, fmt.Errorf("usage: superflare config-read <yaml|env> <file> <key>")
	}
	value, err := data.ReadConfigScalar(args[1], args[2], args[3])
	if err != nil {
		return true, err
	}
	_, err = io.WriteString(output, value)
	return true, err
}
