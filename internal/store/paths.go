package store

import (
	"path"
	"strings"
	"unicode/utf8"
)

const (
	maxNameBytes = 255
	maxPathBytes = 4096
)

// CleanRel normalizes a slash-separated path relative to the share root.
// Leading and duplicate slashes are ignored, but any ".." segment is rejected.
func CleanRel(p string) (string, error) {
	if len(p) > maxPathBytes {
		return "", ErrInvalidPath
	}
	if !utf8.ValidString(p) {
		return "", ErrInvalidPath
	}
	p = strings.TrimPrefix(p, "/")
	if p == "" || p == "." {
		return "", nil
	}
	segments := strings.Split(p, "/")
	cleaned := make([]string, 0, len(segments))
	for _, seg := range segments {
		switch seg {
		case "", ".":
			continue
		case "..":
			return "", ErrInvalidPath
		}
		if err := ValidName(seg); err != nil {
			return "", err
		}
		cleaned = append(cleaned, seg)
	}
	return strings.Join(cleaned, "/"), nil
}

// ValidName checks a single path component. Backslashes are rejected so the
// same tree behaves predictably from Windows WebDAV clients.
func ValidName(name string) error {
	if name == "" || name == "." || name == ".." {
		return ErrInvalidPath
	}
	if len(name) > maxNameBytes || !utf8.ValidString(name) {
		return ErrInvalidPath
	}
	for _, r := range name {
		if r == '/' || r == '\\' || r == 0 {
			return ErrInvalidPath
		}
		if r < 0x20 || r == 0x7f {
			return ErrInvalidPath
		}
	}
	return nil
}

// CleanUploadName extracts a safe base name from a client-supplied filename.
func CleanUploadName(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	if err := ValidName(name); err != nil {
		return "", err
	}
	return name, nil
}

func filesPath(rel string) string {
	if rel == "" {
		return "files"
	}
	return "files/" + rel
}

func cleanParent(rel string) string {
	dir := path.Dir(rel)
	if dir == "." || dir == "/" {
		return ""
	}
	return dir
}

func joinRel(base, name string) string {
	if base == "" {
		return name
	}
	return base + "/" + name
}
