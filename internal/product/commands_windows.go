//go:build windows

package product

<<<<<<< HEAD
import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// configureProcessTermination approximates the Unix process-group kill on
// Windows, which has no POSIX process groups. On cancel or timeout it runs
// taskkill with /T so the child's descendant tree is included and /F so the
// kill is not negotiable. If taskkill cannot be run it falls back to killing
// the direct child only, which is why a taskkill failure flips the reported
// guarantee off.
//
// A stricter alternative is a Windows Job Object (CREATE_SUSPENDED + assign +
// resume, KILL_ON_JOB_CLOSE): the OS then guarantees no descendant outlives
// the job even if it re-parents. That needs golang.org/x/sys/windows and about
// forty lines of syscall plumbing; taskkill /T covers the allowlisted
// executables (go, npm, python3, ...) well enough for this process-hardening
// tier. Swap the body if you need the Job Object guarantee.
func configureProcessTermination(command *exec.Cmd) bool {
	taskkill := "taskkill"
	if root := os.Getenv("SystemRoot"); root != "" {
		taskkill = filepath.Join(root, "System32", "taskkill.exe")
	}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		kill := exec.Command(taskkill, "/pid", strconv.Itoa(command.Process.Pid), "/t", "/f")
		if err := kill.Run(); err != nil {
			return command.Process.Kill()
		}
		return nil
	}
	return true
}
=======
import "os/exec"

// isolateProcessGroup does nothing on Windows and says so.
//
// Windows has no process groups in the POSIX sense, so a cancelled command
// stops the process it started and not whatever that process spawned. The
// receipt records this as process_group_terminated_on_cancel=false rather than
// leaving the caller to assume a guarantee the platform does not give.
func isolateProcessGroup(*exec.Cmd) bool { return false }
>>>>>>> c7c3083f6d0b28b6e40a2c64796b5637fce5b8fd
