package piidentity

// These shapes are the P2 read responses at Pi commit d68bdd3. The Platform
// numeric IDs are evidence only; they never become local Harness IDs.
type Identity struct {
	Authenticated bool     `json:"authenticated"`
	AgentKey      string   `json:"agent_key"`
	NodeKey       string   `json:"node_key"`
	Scopes        []string `json:"scopes"`
	CredentialID  int64    `json:"credential_id"`
	AuthMethod    string   `json:"auth_method"`
	Reason        string   `json:"reason"`
}

type Node struct {
	ID         int64  `json:"id"`
	NodeKey    string `json:"node_key"`
	Status     string `json:"status"`
	AgentCount int64  `json:"agent_count"`
}

type Agent struct {
	ID         int64  `json:"id"`
	AgentKey   string `json:"agent_key"`
	AgentType  string `json:"agent_type"`
	NodeKey    string `json:"node_key"`
	Status     string `json:"status"`
	NodeStatus string `json:"node_status"`
}

type Capabilities struct {
	AgentKey     string   `json:"agent_key"`
	Capabilities []string `json:"capabilities"`
}

type Receipt struct {
	PiCommit                  string   `json:"pi_commit"`
	PiSchema                  int      `json:"pi_schema"`
	RealPiContacted           bool     `json:"real_pi_contacted"`
	Node                      Node     `json:"node"`
	Agent                     Agent    `json:"agent"`
	EffectiveScopes           []string `json:"effective_scopes"`
	CredentialID              int64    `json:"credential_id"`
	CapabilitiesCount         int      `json:"capabilities_count"`
	CapabilitiesInformational bool     `json:"capabilities_informational"`
	RetryCount                int      `json:"retry_count"`
	TransportMode             string   `json:"transport_mode"`
	EndpointClass             string   `json:"endpoint_class"`
	FreshWhoamiCount          int      `json:"fresh_whoami_count"`
	CandidateExecuted         bool     `json:"candidate_executed"`
	TaskRunsDelta             int      `json:"task_runs_delta"`
	AttemptsDelta             int      `json:"attempts_delta"`
	EffectsDelta              int      `json:"effects_delta"`
	RunUpdatesSent            int      `json:"run_updates_sent"`
	ForbiddenInvocations      int      `json:"forbidden_invocation_counters"`
	WorkspaceIdentical        bool     `json:"workspace_before_after_digest_identical"`
	CredentialExposed         bool     `json:"credential_exposed"`
	DistributedWriteAuthority bool     `json:"distributed_write_authority"`
}
