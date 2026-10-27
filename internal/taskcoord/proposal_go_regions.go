package taskcoord

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

func proposalGoSourceRegions(path, source string, terms []string) ([]proposalSourceRegion, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, source, 0)
	if err != nil {
		return nil, errors.New("go source could not be parsed")
	}
	regions := []proposalSourceRegion{}
	if file.Package.IsValid() {
		pkgEnd := fset.Position(file.Name.End()).Offset
		regions = append(regions, proposalSourceRegion{Start: fset.Position(file.Package).Offset, End: pkgEnd, Score: 0})
	}

	var nonimports []proposalSourceRegion
	for _, decl := range file.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			start := fset.Position(decl.Pos()).Offset
			end := fset.Position(decl.End()).Offset
			regions = append(regions, proposalSourceRegion{Start: start, End: end, Score: 0})
			continue
		}
		start := fset.Position(decl.Pos()).Offset
		end := fset.Position(decl.End()).Offset
		score := proposalRegionScore(source[start:end], terms)
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name != nil {
			if containsTerm(terms, strings.ToLower(fd.Name.Name)) {
				score += 1000
			}
		}
		if gd, ok := decl.(*ast.GenDecl); ok {
			for _, spec := range gd.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name != nil {
					if containsTerm(terms, strings.ToLower(ts.Name.Name)) {
						score += 1000
					}
				}
				if vs, ok := spec.(*ast.ValueSpec); ok {
					for _, name := range vs.Names {
						if containsTerm(terms, strings.ToLower(name.Name)) {
							score += 1000
						}
					}
				}
			}
		}
		region := proposalSourceRegion{Start: start, End: end, Score: score}
		nonimports = append(nonimports, region)
	}
	if len(nonimports) == 0 {
		if len(regions) == 0 {
			return nil, errors.New("no source region anchor found")
		}
		if len(regions) > 32 {
			return nil, errors.New("too many source regions")
		}
		regions[0].Score = 1
		return mergeProposalRegions(regions), nil
	}
	maxScore := 0
	for _, r := range nonimports {
		if r.Score > maxScore {
			maxScore = r.Score
		}
	}
	if maxScore == 0 && len(nonimports) == 1 {
		nonimports[0].Score = 1
		maxScore = 1
	} else if maxScore == 0 {
		return nil, errors.New("no source region anchor found")
	}
	var selected []proposalSourceRegion
	if maxScore >= 1000 {
		for _, r := range nonimports {
			if r.Score >= 1000 {
				selected = append(selected, r)
			}
		}
	} else if maxScore >= 50 {
		for _, r := range nonimports {
			if r.Score >= 50 {
				selected = append(selected, r)
			}
		}
	} else {
		for _, r := range nonimports {
			if r.Score == maxScore {
				selected = append(selected, r)
			}
		}
	}
	if len(selected) == 0 {
		return nil, errors.New("no source region anchor found")
	}
	regions = append(regions, selected...)
	if len(regions) > 32 {
		return nil, errors.New("too many source regions; narrow the step")
	}
	return mergeProposalRegions(regions), nil
}

func containsTerm(terms []string, term string) bool {
	for _, t := range terms {
		if strings.EqualFold(t, term) {
			return true
		}
	}
	return false
}
