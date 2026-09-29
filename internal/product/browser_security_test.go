package product

import (
	"testing"

	"hermetrix-harness/internal/agentruntime"
)

func TestBrowserSecurityAuditUsesObservedDocumentOnly(t *testing.T) {
	page := "https://example.test/login?token=secret"
	view := agentruntime.BrowserInspection{Forms: []agentruntime.BrowserInspectionForm{
		{Method: "POST", Action: "http://example.test/login", HasPassword: true},
	}}
	audit := auditBrowserInspection(page, view)
	if len(audit.Findings) != 1 || audit.Findings[0].ID != "password_form_http" {
		t.Fatalf("missing document response produced speculative header findings: %+v", audit.Findings)
	}
	view.Traffic = []agentruntime.BrowserInspectionTraffic{{URL: "https://example.test/login", Type: "Document", Status: 200,
		SecurityHeaders: map[string]string{"content-security-policy-report-only": "default-src 'self'", "x-content-type-options": "nosniff"}}}
	audit = auditBrowserInspection(page, view)
	want := map[string]bool{"password_form_http": true, "csp_report_only": true, "hsts_absent": true, "frame_policy_absent": true}
	for _, finding := range audit.Findings {
		if !want[finding.ID] {
			t.Errorf("unexpected finding %+v", finding)
		}
		delete(want, finding.ID)
	}
	if len(want) != 0 {
		t.Fatalf("missing findings: %+v", want)
	}
}

func TestRecordTrafficRedactsURLAndSensitiveHeaders(t *testing.T) {
	client := &cdpClient{}
	client.recordTraffic("https://user:pass@example.test/api/items?token=secret#frag", "GET", "Fetch", 200,
		map[string]any{"Set-Cookie": "session=secret", "Content-Security-Policy": "default-src 'self'"})
	if len(client.traffic) != 1 {
		t.Fatalf("traffic=%+v", client.traffic)
	}
	item := client.traffic[0]
	if item.URL != "https://example.test/api/items" || item.SecurityHeaders["content-security-policy"] == "" || len(item.SecurityHeaders) != 1 {
		t.Fatalf("traffic redaction failed: %+v", item)
	}
}
