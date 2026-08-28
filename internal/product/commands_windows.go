//go:build windows

package product

import "os/exec"

// isolateProcessGroup does nothing on Windows and says so.
//
// Windows has no process groups in the POSIX sense, so a cancelled command
// stops the process it started and not whatever that process spawned. The
// receipt records this as process_group_terminated_on_cancel=false rather than
// leaving the caller to assume a guarantee the platform does not give.
func isolateProcessGroup(*exec.Cmd) bool { return false }
