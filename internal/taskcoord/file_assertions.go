package taskcoord

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

func assertionCommand(argv []string) bool {
	return len(argv) > 0 && (argv[0] == "assert-file-sha256" || argv[0] == "assert-file-contains")
}

// Compile a bounded assertion into a fixed host-owned program. Model text is
// encoded as data, never interpolated as JavaScript. A mismatch throws and exits 1.
func compileFileAssertion(argv []string) ([]string, error) {
	if len(argv) != 3 || !assertionCommand(argv) {
		return nil, fmt.Errorf("file assertion requires relative path and expected value")
	}
	p := argv[1]
	if p == "" || p == "." || path.IsAbs(p) || path.Clean(p) != p || p == ".." || strings.HasPrefix(p, "../") || strings.ContainsAny(p, "\\:\x00\r\n") {
		return nil, fmt.Errorf("file assertion path must stay inside the project root")
	}
	if argv[0] == "assert-file-sha256" {
		decoded, err := hex.DecodeString(argv[2])
		if err != nil || len(decoded) != 32 || strings.ToLower(argv[2]) != argv[2] {
			return nil, fmt.Errorf("file assertion requires a lowercase SHA256 digest")
		}
	} else if len(argv[2]) == 0 || len(argv[2]) > 4096 {
		return nil, fmt.Errorf("contains assertion requires a nonempty literal up to 4096 bytes")
	}
	file, _ := json.Marshal(p)
	expected, _ := json.Marshal(argv[2])
	program := "const fs=require('node:fs'),assert=require('node:assert/strict');const data=fs.readFileSync(" + string(file) + ");"
	if argv[0] == "assert-file-sha256" {
		program += "assert.equal(require('node:crypto').createHash('sha256').update(data).digest('hex')," + string(expected) + ",'file bytes changed');"
	} else {
		program += "assert.ok(data.toString('utf8').includes(" + string(expected) + "),'required literal missing');"
	}
	return []string{"node", "-e", program + "console.log('File assertion passed');"}, nil
}
