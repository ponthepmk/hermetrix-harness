package mcp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// isLocalComfyMCP accepts only the installed first-party MCP beside comfy-cli
// in one Comfy virtual environment. The user still has to save this exact
// connection; arbitrary local executables remain outside the MCP allowlist.
func isLocalComfyMCP(executable string) bool {
	if !filepath.IsAbs(executable) || !strings.EqualFold(filepath.Base(executable), "comfy-mcp.exe") {
		return false
	}
	dir := filepath.Dir(filepath.Clean(executable))
	if !strings.EqualFold(filepath.Base(dir), "Scripts") || !strings.EqualFold(filepath.Base(filepath.Dir(dir)), ".venv") {
		return false
	}
	for _, path := range []string{executable, filepath.Join(dir, "comfy.exe"), filepath.Join(dir, "python.exe")} {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return false
		}
	}
	return true
}

func configureStdioTermination(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
}

func startStdioProcess(command *exec.Cmd) (func(), bool, error) {
	if err := command.Start(); err != nil {
		return nil, false, err
	}
	fail := func(cause error) (func(), bool, error) {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = command.Wait()
		return nil, false, cause
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fail(err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		return fail(err)
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(command.Process.Pid))
	if err != nil {
		windows.CloseHandle(job)
		return fail(err)
	}
	if err = windows.AssignProcessToJobObject(job, process); err != nil {
		windows.CloseHandle(process)
		windows.CloseHandle(job)
		return fail(err)
	}
	windows.CloseHandle(process)
	thread, err := stdioSuspendedThread(uint32(command.Process.Pid))
	if err != nil {
		windows.CloseHandle(job)
		return fail(err)
	}
	if _, err = windows.ResumeThread(thread); err != nil {
		windows.CloseHandle(thread)
		windows.CloseHandle(job)
		return fail(err)
	}
	windows.CloseHandle(thread)
	var once sync.Once
	return func() { once.Do(func() { _ = windows.CloseHandle(job) }) }, true, nil
}

func stdioSuspendedThread(processID uint32) (windows.Handle, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	if err = windows.Thread32First(snapshot, &entry); err != nil {
		return 0, err
	}
	for {
		if entry.OwnerProcessID == processID {
			return windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		}
		if err = windows.Thread32Next(snapshot, &entry); err != nil {
			return 0, fmt.Errorf("find suspended MCP thread for process %d: %w", processID, err)
		}
	}
}
