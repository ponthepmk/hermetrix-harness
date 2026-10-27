package taskcoord

import (
	"regexp"
	"strconv"
	"strings"
)

var nodeTestCount = regexp.MustCompile(`(?m)^# tests ([0-9]+)\s*$`)
var pythonTestCount = regexp.MustCompile(`(?m)^Ran ([0-9]+) tests? in `)
var goPassedPackage = regexp.MustCompile(`(?m)^ok\s+[^\r\n]+`)

// A successful process is not evidence that a test suite executed. Recognize
// only known summaries; custom commands still require their own assertions.
func emptyTestDiscovery(executable string, args []string, output string) bool {
	if executable == "go" && len(args) > 0 && args[0] == "test" {
		packages := goPassedPackage.FindAllString(output, -1)
		for _, summary := range packages {
			if !strings.Contains(summary, "[no tests to run]") {
				return false
			}
		}
		return strings.Contains(output, "[no test files]") || len(packages) > 0
	}
	isNode := executable == "node" && strings.Contains(" "+strings.Join(args, " ")+" ", " --test ")
	isPython := (executable == "python" || executable == "python3") && len(args) > 1 && args[0] == "-m" && args[1] == "unittest"
	var pattern *regexp.Regexp
	if isNode {
		pattern = nodeTestCount
	} else if isPython {
		pattern = pythonTestCount
	} else {
		return false
	}
	matches := pattern.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return false
	}
	for _, match := range matches {
		n, err := strconv.Atoi(match[1])
		if err != nil || n > 0 {
			return false
		}
	}
	return true
}
