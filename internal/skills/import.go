package skills

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"
)

// ParseSkillArchive reads a .skill ZIP as inert data. It accepts either a
// root SKILL.md or the common single enclosing directory.
func ParseSkillArchive(data []byte) (string, Package, error) {
	if len(data) > maxPackageBytes {
		return "", Package{}, fmt.Errorf("skill archive exceeds %d bytes", maxPackageBytes)
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", Package{}, fmt.Errorf("open skill archive: %w", err)
	}
	if len(r.File) > 256 {
		return "", Package{}, fmt.Errorf("skill archive has too many files")
	}
	prefix := ""
	hasRoot := false
	for _, f := range r.File {
		if f.Name == "SKILL.md" {
			hasRoot = true
			break
		}
	}
	if !hasRoot {
		for _, f := range r.File {
			if strings.HasSuffix(f.Name, "/SKILL.md") && strings.Count(f.Name, "/") == 1 {
				if prefix != "" {
					return "", Package{}, fmt.Errorf("skill archive has multiple roots")
				}
				prefix = strings.TrimSuffix(f.Name, "SKILL.md")
			}
		}
	}
	if !hasRoot && prefix == "" {
		return "", Package{}, fmt.Errorf("skill archive is missing SKILL.md")
	}
	pkg := Package{Format: 1}
	total := 0
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if !f.Mode().IsRegular() {
			return "", Package{}, fmt.Errorf("unsupported skill archive entry %q", f.Name)
		}
		if strings.Contains(f.Name, "\\") || strings.HasPrefix(f.Name, "/") || strings.HasPrefix(f.Name, "../") || f.Name == ".." || strings.Contains(f.Name, "/../") || path.Clean(f.Name) != f.Name {
			return "", Package{}, fmt.Errorf("unsafe skill archive path %q", f.Name)
		}
		if prefix != "" && !strings.HasPrefix(f.Name, prefix) {
			return "", Package{}, fmt.Errorf("skill archive contains another root %q", f.Name)
		}
		name := strings.TrimPrefix(f.Name, prefix)
		if f.UncompressedSize64 > maxFileBytes || total+int(f.UncompressedSize64) > maxPackageBytes {
			return "", Package{}, fmt.Errorf("skill archive content exceeds size limit")
		}
		file, err := f.Open()
		if err != nil {
			return "", Package{}, err
		}
		content, readErr := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return "", Package{}, readErr
		}
		if closeErr != nil {
			return "", Package{}, closeErr
		}
		total += len(content)
		pkg.Files = append(pkg.Files, File{Path: name, Content: content})
	}
	if err := pkg.Validate(); err != nil {
		return "", Package{}, err
	}
	manifest := ParseManifest(pkg.Markdown())
	if manifest.Name == "" {
		return "", Package{}, fmt.Errorf("SKILL.md must declare a name")
	}
	return manifest.Name, pkg.Canonical(), nil
}
