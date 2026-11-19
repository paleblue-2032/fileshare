package store

import (
	"strings"
	"testing"
)

func TestCleanRel(t *testing.T) {
	valid := map[string]string{
		"":             "",
		"/":            "",
		".":            "",
		"a":            "a",
		"/a/b/":        "a/b",
		"a//b/./c":     "a/b/c",
		"日本語/ファイル.txt": "日本語/ファイル.txt",
	}
	for input, want := range valid {
		got, err := CleanRel(input)
		if err != nil {
			t.Fatalf("CleanRel(%q) error: %v", input, err)
		}
		if got != want {
			t.Fatalf("CleanRel(%q) = %q, want %q", input, got, want)
		}
	}

	invalid := []string{
		"..",
		"../etc/passwd",
		"a/../../b",
		"/a/../b",
		"a/\x00b",
		"a/\x01b",
		"a\\b",
		strings.Repeat("x", 256),
	}
	for _, input := range invalid {
		if _, err := CleanRel(input); err == nil {
			t.Fatalf("CleanRel(%q) should fail", input)
		}
	}
}

func TestValidName(t *testing.T) {
	valid := []string{"a", "a.txt", "日本語", "name with spaces", ".hidden"}
	for _, name := range valid {
		if err := ValidName(name); err != nil {
			t.Fatalf("ValidName(%q) error: %v", name, err)
		}
	}
	invalid := []string{"", ".", "..", "a/b", "a\\b", "a\x00b", string([]byte{0xff})}
	for _, name := range invalid {
		if err := ValidName(name); err == nil {
			t.Fatalf("ValidName(%q) should fail", name)
		}
	}
}

func TestCleanUploadName(t *testing.T) {
	got, err := CleanUploadName(`C:\Users\me\photo.jpg`)
	if err != nil || got != "photo.jpg" {
		t.Fatalf("CleanUploadName = %q, %v", got, err)
	}
	if _, err := CleanUploadName("../evil"); err != nil {
		t.Fatal(err)
	}
	got, _ = CleanUploadName("../evil")
	if got != "evil" {
		t.Fatalf("got %q, want evil", got)
	}
}
