//go:build windows

package core

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// symlinkDir creates link as a junction pointing at the directory target,
// which needs no special privileges. The command line is written out because
// cmd.exe splits unquoted arguments at characters such as "&", and Go quotes
// only arguments with spaces. Windows paths cannot contain '"', but cmd.exe
// expands %VAR% even inside quotes, so paths with '%' are refused.
func symlinkDir(target, link string) error {
	if strings.Contains(target+link, "%") {
		return fmt.Errorf("can't link %s to %s: paths containing %% are not supported on Windows", link, target)
	}
	cmd := exec.Command("cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: fmt.Sprintf(`cmd /c mklink /J "%s" "%s"`, link, target)}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mklink /J %s %s: %w: %s", link, target, err, bytes.TrimSpace(out))
	}
	return nil
}
