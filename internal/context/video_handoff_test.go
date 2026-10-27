package context

import (
	stdcontext "context"
	"strings"
	"testing"
)

func TestVideoHandoffRetainsSmallReportWithoutRelaxingEnvelope(t *testing.T) {
	profile := Compact32K()
	for _, repeats := range []int{500, 1400} {
		compiler := testCompiler(t)
		content := strings.Repeat("actual video evidence ", repeats)
		within := compiler.estimator.Count(content) <= 4096
		compiled, err := compiler.Compile(stdcontext.Background(), Request{Profile: profile, Fragments: []Fragment{
			{ID: "goal", Kind: KindUserGoal, Scope: "session", Trust: "user", Pinned: true, Content: "summarize public video"},
			{ID: "report", Kind: KindToolResult, Scope: "session", Trust: "tool", Pinned: true, Content: content, Metadata: map[string]string{"video_report_handoff": "owner-cli-video-v1"}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if within && len(compiled.Report.Spilled) != 0 {
			t.Fatal("small handoff report was unnecessarily spilled")
		}
		if !within && len(compiled.Report.Spilled) != 1 {
			t.Fatal("large report bypassed the bounded inline threshold")
		}
		if compiled.Report.UnaccountedTokens != 0 || compiled.Report.PredictedInput > profile.Total-profile.OutputReserve-profile.UncertaintyReserve {
			t.Fatal("handoff relaxed or broke the context ledger")
		}
		for _, fragment := range compiled.Fragments {
			if fragment.ID == "report" && fragment.Trust != "tool" {
				t.Fatal("untrusted report gained authority")
			}
		}
	}
}
