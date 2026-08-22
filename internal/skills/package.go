package skills

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

const (
	maxPackageBytes = 8 << 20
	maxFileBytes    = 2 << 20
)

type File struct {
	Path    string `json:"path"`
	Content []byte `json:"content"`
}

type Package struct {
	Format int    `json:"format"`
	Files  []File `json:"files"`
}

type Manifest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Tools       []string `json:"tools"`
}

func NewPackage(markdown string, supporting []File) (Package, error) {
	files := make([]File, 0, len(supporting)+1)
	files = append(files, File{Path: "SKILL.md", Content: []byte(markdown)})
	files = append(files, supporting...)
	p := Package{Format: 1, Files: files}
	if err := p.Validate(); err != nil {
		return Package{}, err
	}
	return p.Canonical(), nil
}

func ParsePackage(data []byte) (Package, error) {
	var p Package
	if err := json.Unmarshal(data, &p); err != nil {
		return Package{}, fmt.Errorf("decode skill package: %w", err)
	}
	if err := p.Validate(); err != nil {
		return Package{}, err
	}
	return p.Canonical(), nil
}

func (p Package) Encode() ([]byte, error) {
	canonical := p.Canonical()
	if err := canonical.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(canonical)
}

func (p Package) Canonical() Package {
	out := Package{Format: p.Format, Files: append([]File(nil), p.Files...)}
	sort.Slice(out.Files, func(i, j int) bool { return out.Files[i].Path < out.Files[j].Path })
	return out
}

func (p Package) Validate() error {
	if p.Format != 1 {
		return fmt.Errorf("unsupported package format %d", p.Format)
	}
	seen := make(map[string]bool, len(p.Files))
	total := 0
	for _, file := range p.Files {
		clean := path.Clean(strings.TrimSpace(file.Path))
		if clean == "." || clean == "" || strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "\\") {
			return fmt.Errorf("unsafe package path %q", file.Path)
		}
		if clean != file.Path {
			return fmt.Errorf("package path must be canonical: %q", file.Path)
		}
		if seen[file.Path] {
			return fmt.Errorf("duplicate package path %q", file.Path)
		}
		seen[file.Path] = true
		if len(file.Content) > maxFileBytes {
			return fmt.Errorf("file %q exceeds %d bytes", file.Path, maxFileBytes)
		}
		total += len(file.Content)
	}
	if !seen["SKILL.md"] {
		return fmt.Errorf("skill package is missing SKILL.md")
	}
	if total > maxPackageBytes {
		return fmt.Errorf("skill package exceeds %d bytes", maxPackageBytes)
	}
	return nil
}

func (p Package) Markdown() string {
	for _, file := range p.Files {
		if file.Path == "SKILL.md" {
			return string(file.Content)
		}
	}
	return ""
}

func (p Package) SupportingPaths() []string {
	var out []string
	for _, file := range p.Files {
		if file.Path != "SKILL.md" {
			out = append(out, file.Path)
		}
	}
	return out
}

func ParseManifest(markdown string) Manifest {
	m := Manifest{}
	lines := strings.Split(markdown, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return m
	}
	for _, raw := range lines[1:] {
		line := strings.TrimSpace(raw)
		if line == "---" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "\"'")
		switch strings.TrimSpace(key) {
		case "name":
			m.Name = value
		case "description":
			m.Description = value
		case "tags":
			m.Tags = splitList(value)
		case "tools":
			m.Tools = splitList(value)
		}
	}
	return m
}

func splitList(value string) []string {
	value = strings.TrimSpace(strings.Trim(value, "[]"))
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.Trim(strings.TrimSpace(part), "\"'"); item != "" {
			out = append(out, item)
		}
	}
	return out
}
