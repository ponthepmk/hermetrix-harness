//go:build windows

package product

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"
)

// configureProcessTermination approximates the Unix process-group kill on
// Windows, which has no POSIX process groups. On cancel or timeout it runs
// taskkill with /T so the child's descendant tree is included and /F so the
// kill is not negotiable. If taskkill cannot be run it falls back to killing
// the direct child only. The return value remains false because taskkill is a
// best-effort runtime action, not the OS-level lifetime guarantee provided by
// a Windows Job Object; receipts must not overclaim that guarantee.
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
	return false
}

// runCommandProcess assigns the root process to a Job Object with
// KILL_ON_JOB_CLOSE. taskkill remains the immediate cancellation path set
// above; closing the Job after the root exits is the OS-enforced backstop that
// kills descendants even if they re-parented away from the original process.
func runCommandProcess(command *exec.Cmd) (error, bool) {
	if err := command.Start(); err != nil {
		return err, false
	}
	job, jobErr := windows.CreateJobObject(nil, nil)
	if jobErr != nil {
		return command.Wait(), false
	}
	defer windows.CloseHandle(job)
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return command.Wait(), false
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(command.Process.Pid))
	if err != nil {
		return command.Wait(), false
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		return command.Wait(), false
	}
	return command.Wait(), true
}
