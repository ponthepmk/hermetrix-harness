package tools

// runtimeDefinitions are the tools that reach the machine rather than the
// repository. They are session-scoped: the agent service executes them, because
// answering them needs the session's project and its in-flight jobs, neither of
// which the registry holds.
func runtimeDefinitions() []Definition {
	return []Definition{
		{Name: "workspace.run", Revision: "v1", Effect: "execute",
			Description: "Run one allow-listed command in the project and read its result. Start it with action=start, which returns a job_id straight away, then call action=status with that job_id to wait for the result; status blocks for up to 30 seconds per call, so call it again if the command has not finished. action=cancel stops a command. Allowed executables: go, git, node, npm, python3, rg, ls. There is no shell, so pipes, redirects, globs and command chaining do not work; pass each argument separately.",
			Parameters: objectSchema(map[string]any{
				"action":          map[string]any{"type": "string", "enum": []any{"start", "status", "cancel"}, "description": "start a command, wait for one, or stop one"},
				"executable":      map[string]any{"type": "string", "description": "For start: one of go, git, node, npm, python3, rg, ls"},
				"arguments":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "For start: the arguments, one per element, with no shell syntax"},
				"working_dir":     map[string]any{"type": "string", "description": "For start: a project-relative directory; omit for the project root"},
				"timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 600, "description": "For start: how long the command may run; defaults to 30"},
				"job_id":          map[string]any{"type": "string", "description": "For status and cancel: the job_id that start returned"},
			}, []string{"action"})},
	}
}
