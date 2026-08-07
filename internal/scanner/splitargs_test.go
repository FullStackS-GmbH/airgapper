package scanner

import (
	"reflect"
	"testing"
)

func TestSplitArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want []string
	}{
		{"trivy image nginx:1.0", []string{"trivy", "image", "nginx:1.0"}},
		{`trivy image "my path/x"`, []string{"trivy", "image", "my path/x"}},
		{"trivy   image\tnginx", []string{"trivy", "image", "nginx"}},
		{`echo 'a b' c`, []string{"echo", "a b", "c"}},
		{`echo a\ b`, []string{"echo", "a b"}},
		{"", nil},
		{`echo ""`, []string{"echo", ""}},
	}
	for _, tc := range tests {
		if got := splitArgs(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitArgs(%q) = %#v, want %#v", tc.in, got, tc.want)
		}
	}
}
