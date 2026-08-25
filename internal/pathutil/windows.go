package pathutil

import (
	"errors"
	"path/filepath"
	"strings"
)

var ErrOutsideRoot = errors.New("path is outside watched root")

func NormalizeRoot(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if len(abs) > 3 {
		abs = strings.TrimRight(abs, `\/`)
	}
	return strings.ToLower(filepath.ToSlash(abs)), nil
}

func NormalizeRelative(p string) (string, error) {
	if p == "" || p == "." {
		return "", nil
	}
	p = strings.ReplaceAll(p, `\`, "/")
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return "", ErrOutsideRoot
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", ErrOutsideRoot
	}
	return strings.ToLower(clean), nil
}

func JoinWithinRoot(root, relative string) (string, error) {
	normalized, err := NormalizeRelative(relative)
	if err != nil || normalized == "" && relative != "" && relative != "." {
		return "", ErrOutsideRoot
	}
	target := filepath.Join(root, filepath.FromSlash(relative))
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, `..\`) || filepath.IsAbs(rel) {
		return "", ErrOutsideRoot
	}
	return targetAbs, nil
}
