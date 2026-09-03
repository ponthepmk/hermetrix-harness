package mcp

import "os/exec"

// configureStdioTermination has no process-group equivalent here yet: Windows
// needs a job object to take a launcher's children down with it, and claiming
// the subtree dies without one would be a comment that lies. The direct child
// is still killed on stop.
func configureStdioTermination(command *exec.Cmd) {}
