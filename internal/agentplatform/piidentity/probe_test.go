package piidentity

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeCaller struct {
	responses map[string]string
	calls     []string
	failOnce  bool
}

func validFake() *fakeCaller {
	return &fakeCaller{responses: map[string]string{
		"whoami":            `{"authenticated":true,"agent_key":"hermetrix-bonsai","node_key":"windows-pc-main","scopes":["read"],"credential_id":2,"auth_method":"scoped_agent"}`,
		"get_node":          `{"id":3,"node_key":"windows-pc-main","status":"active","agent_count":1}`,
		"get_agent":         `{"id":2,"agent_key":"hermetrix-bonsai","agent_type":"managed","node_key":"windows-pc-main","status":"active","node_status":"active"}`,
		"list_capabilities": `{"agent_key":"hermetrix-bonsai","capabilities":["work.write"]}`,
	}}
}

func (f *fakeCaller) Call(_ context.Context, name string, _ json.RawMessage) (json.RawMessage, error) {
	f.calls = append(f.calls, name)
	if f.failOnce {
		f.failOnce = false
		return nil, ErrTransient
	}
	if !allowed[name] {
		return nil, errors.New("forbidden tool")
	}
	return json.RawMessage(f.responses[name]), nil
}

func TestValidIdentityAndCapabilityIsNotAuthority(t *testing.T) {
	f := validFake()
	r, err := Probe(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if !r.RealPiContacted || len(r.EffectiveScopes) != 1 || r.EffectiveScopes[0] != "read" || !r.CapabilitiesInformational || r.CapabilitiesCount != 1 {
		t.Fatalf("unexpected receipt: %+v", r)
	}
	if strings.Join(f.calls, ",") != "whoami,get_node,get_agent,list_capabilities" {
		t.Fatalf("wrong read flow: %v", f.calls)
	}
}

func TestIdentityAndBindingFailuresFailClosed(t *testing.T) {
	cases := map[string]struct{ tool, response string }{
		"wrong node key":               {"whoami", `{"authenticated":true,"agent_key":"hermetrix-bonsai","node_key":"other","scopes":["read"],"credential_id":2,"auth_method":"scoped_agent"}`},
		"wrong agent key":              {"whoami", `{"authenticated":true,"agent_key":"other","node_key":"windows-pc-main","scopes":["read"],"credential_id":2,"auth_method":"scoped_agent"}`},
		"unknown credential":           {"whoami", `{"authenticated":false,"reason":"BAD_CREDENTIAL"}`},
		"revoked credential":           {"whoami", `{"authenticated":false,"reason":"CREDENTIAL_REVOKED"}`},
		"expired credential":           {"whoami", `{"authenticated":false,"reason":"CREDENTIAL_EXPIRED"}`},
		"missing read":                 {"whoami", `{"authenticated":true,"agent_key":"hermetrix-bonsai","node_key":"windows-pc-main","scopes":[],"credential_id":2,"auth_method":"scoped_agent"}`},
		"unexpected write":             {"whoami", `{"authenticated":true,"agent_key":"hermetrix-bonsai","node_key":"windows-pc-main","scopes":["read","work.write"],"credential_id":2,"auth_method":"scoped_agent"}`},
		"legacy fallback":              {"whoami", `{"authenticated":true,"agent_key":"hermetrix-bonsai","node_key":"windows-pc-main","scopes":["read"],"credential_id":2,"auth_method":"legacy_kanban_write_key"}`},
		"wrong node id":                {"get_node", `{"id":4,"node_key":"windows-pc-main","status":"active"}`},
		"disabled node":                {"get_node", `{"id":3,"node_key":"windows-pc-main","status":"disabled"}`},
		"wrong agent id":               {"get_agent", `{"id":4,"agent_key":"hermetrix-bonsai","agent_type":"managed","node_key":"windows-pc-main","status":"active","node_status":"active"}`},
		"disabled agent":               {"get_agent", `{"id":2,"agent_key":"hermetrix-bonsai","agent_type":"managed","node_key":"windows-pc-main","status":"disabled","node_status":"active"}`},
		"wrong binding":                {"get_agent", `{"id":2,"agent_key":"hermetrix-bonsai","agent_type":"managed","node_key":"other","status":"active","node_status":"active"}`},
		"capability identity mismatch": {"list_capabilities", `{"agent_key":"other","capabilities":[]}`},
		"malformed response":           {"whoami", `not-json`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := validFake()
			f.responses[tc.tool] = tc.response
			if _, err := Probe(context.Background(), f); err == nil {
				t.Fatal("accepted invalid Pi response")
			}
		})
	}
}

func TestTransientRetryStartsWithFreshWhoami(t *testing.T) {
	f := validFake()
	f.failOnce = true
	r, err := Probe(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if r.RetryCount != 1 || len(f.calls) < 2 || f.calls[0] != "whoami" || f.calls[1] != "whoami" {
		t.Fatalf("retry did not restart identity proof: %v %+v", f.calls, r)
	}
}

func TestReconnectProofDoesNotGainSecondRetry(t *testing.T) {
	f := validFake()
	f.failOnce = true
	if _, err := ProbeFresh(context.Background(), f); !errors.Is(err, ErrTransient) {
		t.Fatalf("expected one failed reconnect attempt: %v", err)
	}
	if len(f.calls) != 1 || f.calls[0] != "whoami" {
		t.Fatalf("reconnect retried unexpectedly: %v", f.calls)
	}
}

func TestEndpointPolicy(t *testing.T) {
	for _, url := range []string{"http://192.168.1.123:8902/mcp", "http://pi.local:8902/mcp", "https://user:secret@pi.example/mcp", "https://pi.example/other"} {
		if err := validateEndpoint(url); err == nil {
			t.Errorf("accepted unsafe URL %q", url)
		}
	}
	for _, url := range []string{"http://127.0.0.1:8902/mcp", "https://pi.example/mcp"} {
		if err := validateEndpoint(url); err != nil {
			t.Errorf("rejected secure URL %q: %v", url, err)
		}
	}
}
