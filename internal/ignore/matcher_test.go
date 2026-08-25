package ignore

import "testing"

func TestMatcher(t *testing.T) {
	m, err := Compile([]string{
		"# defaults",
		"node_modules/",
		"/cache/",
		"**/.cache/**",
		"*.log",
		"!important.log",
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path string
		dir  bool
		want bool
	}{
		{"node_modules", true, true},
		{"src/node_modules/pkg/a.js", false, true},
		{"cache/a", false, true},
		{"nested/cache/a", false, false},
		{"a/.cache/x/y", false, true},
		{"a/debug.log", false, true},
		{"important.log", false, false},
		{"a/important.log", false, false},
	}
	for _, tc := range cases {
		if got := m.Match(tc.path, tc.dir).Excluded; got != tc.want {
			t.Errorf("Match(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
	if m.CanPrune("node_modules") {
		t.Fatal("matcher with negations must retain traversal")
	}
}

func TestLastMatchWins(t *testing.T) {
	m, _ := Compile([]string{"*.tmp", "!keep.tmp", "keep.*"})
	result := m.Match("keep.tmp", false)
	if !result.Excluded || result.MatchingRule != "keep.*" {
		t.Fatalf("unexpected result: %+v", result)
	}
}
