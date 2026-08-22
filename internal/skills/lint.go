package skills

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var skillNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)

func CheckPackage(name string, p Package) CheckSet {
	checks := CheckSet{
		LintPassed:     true,
		SecurityPassed: true,
		ReplayPassed:   true,
		CheckerVersion: "hermetrix-checks-v1",
	}
	add := func(level, code, message, path string) {
		checks.Findings = append(checks.Findings, CheckFinding{Level: level, Code: code, Message: message, Path: path})
		if level == "error" {
			checks.LintPassed = false
		}
	}
	if !skillNamePattern.MatchString(name) {
		add("error", "invalid_name", "name must be 2-63 lowercase letters, digits, or hyphens", "")
	}
	if err := p.Validate(); err != nil {
		add("error", "invalid_package", err.Error(), "")
		checks.SecurityPassed = false
	}
	markdown := p.Markdown()
	checks.TokenEstimate = estimateTokens(markdown)
	manifest := ParseManifest(markdown)
	if manifest.Name == "" {
		add("error", "missing_manifest_name", "frontmatter must declare name", "SKILL.md")
	} else if manifest.Name != name {
		add("error", "name_mismatch", "frontmatter name must match the canonical skill name", "SKILL.md")
	}
	if strings.TrimSpace(manifest.Description) == "" {
		add("error", "missing_description", "frontmatter must describe when the skill applies", "SKILL.md")
	} else if utf8.RuneCountInString(manifest.Description) > 320 {
		add("warning", "description_too_long", "description should fit progressive-disclosure metadata", "SKILL.md")
	}
	if checks.TokenEstimate > 4000 {
		add("warning", "large_skill", "skill body exceeds 4k estimated tokens; split references or narrow scope", "SKILL.md")
	}
	for _, file := range p.Files {
		lower := strings.ToLower(file.Path)
		if strings.HasSuffix(lower, ".exe") || strings.HasSuffix(lower, ".dll") || strings.HasSuffix(lower, ".dylib") || strings.HasSuffix(lower, ".so") {
			checks.SecurityPassed = false
			checks.Findings = append(checks.Findings, CheckFinding{Level: "error", Code: "binary_not_allowed", Message: "binary files require a separate quarantined import policy", Path: file.Path})
		}
	}
	if _, err := parseReplayFixtures(p); err != nil {
		add("error", "invalid_replay_fixture", err.Error(), "tests/")
	}
	checks.CapabilityHints = append([]string(nil), manifest.Tools...)
	checks.Passed = checks.LintPassed && checks.SecurityPassed && (!checks.ReplayRequired || checks.ReplayPassed)
	return checks
}

func estimateTokens(text string) int {
	ascii, nonASCII, punct := 0, 0, 0
	for _, r := range text {
		if r > 127 {
			nonASCII++
			continue
		}
		ascii++
		if strings.ContainsRune("{}[]():,;\"'`", r) {
			punct++
		}
	}
	return (ascii+3)/4 + nonASCII + (punct+5)/6
}
