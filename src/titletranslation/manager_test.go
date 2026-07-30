package titletranslation

import (
	"os"
	"path/filepath"
	"testing"
)

func fakeHenji(t *testing.T, output string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "henji")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nschema=\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = --json-schema ]; then shift; schema=$1; fi\n  shift\ndone\ntest -f \"$schema\" || exit 2\ngrep -q '\"result\"' \"$schema\" || exit 3\ncat >/dev/null\nprintf '%s' \"$HENJI_OUTPUT\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HENJI_OUTPUT", output)
	return path
}

func TestTranslateStrictOutput(t *testing.T) {
	for _, tc := range []struct {
		name, output          string
		wantResult, wantTitle string
		wantErr               bool
	}{
		{"translated", `{"result":"translated","title":"翻訳"}`, "translated", "翻訳", false},
		{"skipped", `{"result":"skipped"}`, "skipped", "", false},
		{"skipped null title", `{"result":"skipped","title":null}`, "", "", true},
		{"unknown field", `{"result":"skipped","extra":1}`, "", "", true},
		{"duplicate field", `{"result":"skipped","result":"translated","title":"翻訳"}`, "", "", true},
		{"extra document", `{"result":"skipped"}{}`, "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(nil, Config{Path: fakeHenji(t, tc.output), API: "api", Model: "model"})
			result, title, err := m.translate("An English title")
			if tc.wantErr {
				if err == nil {
					t.Fatal("translate succeeded")
				}
				return
			}
			if err != nil || result != tc.wantResult || title != tc.wantTitle {
				t.Fatalf("translate = %q, %q, %v", result, title, err)
			}
		})
	}
}

func TestTranslateRejectsOversizedInput(t *testing.T) {
	m := New(nil, Config{Path: "/does/not/matter", API: "api", Model: "model"})
	if _, _, err := m.translate(string(make([]byte, maxInputBytes+1))); err == nil {
		t.Fatal("oversized title succeeded")
	}
}

func TestHasLatin(t *testing.T) {
	for _, tc := range []struct {
		title string
		want  bool
	}{{"日本語", false}, {"中文", false}, {"123!", false}, {"OpenAIの新機能", true}, {"English", true}} {
		if got := hasLatin(tc.title); got != tc.want {
			t.Errorf("hasLatin(%q) = %v, want %v", tc.title, got, tc.want)
		}
	}
}
