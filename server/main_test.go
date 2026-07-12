package main

import "testing"

func TestIgnoreMatcher(t *testing.T) {
	matcher := IgnoreMatcher{
		rules: []IgnoreRule{
			mustParseIgnoreRule(t, "node_modules/"),
			mustParseIgnoreRule(t, "*.log"),
			mustParseIgnoreRule(t, "/tmp/"),
			mustParseIgnoreRule(t, "build/cache/"),
		},
	}

	tests := []struct {
		name    string
		relPath string
		isDir   bool
		want    bool
	}{
		{name: "directory itself", relPath: "node_modules", isDir: true, want: true},
		{name: "file inside ignored directory", relPath: "node_modules/pkg/index.js", want: true},
		{name: "nested ignored directory", relPath: "app/node_modules/pkg/index.js", want: true},
		{name: "glob in root", relPath: "app.log", want: true},
		{name: "glob in nested directory", relPath: "logs/app.log", want: true},
		{name: "anchored directory", relPath: "tmp/cache.bin", want: true},
		{name: "anchored directory does not match nested", relPath: "app/tmp/cache.bin", want: false},
		{name: "unanchored path with slash", relPath: "app/build/cache/data.bin", want: true},
		{name: "not ignored", relPath: "src/main.go", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matcher.Match(tt.relPath, tt.isDir)
			if got != tt.want {
				t.Fatalf("Match(%q, %v) = %v, want %v", tt.relPath, tt.isDir, got, tt.want)
			}
		})
	}
}

func mustParseIgnoreRule(t *testing.T, line string) IgnoreRule {
	t.Helper()

	rule, ok := parseIgnoreRule(line)
	if !ok {
		t.Fatalf("failed to parse ignore rule %q", line)
	}

	return rule
}
