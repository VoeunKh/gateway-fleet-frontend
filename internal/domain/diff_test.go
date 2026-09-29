package domain

import (
	"reflect"
	"testing"
)

func TestDiffLines(t *testing.T) {
	tests := []struct {
		name, a, b string
		want       []DiffLine
	}{
		{"same", "a\nb", "a\nb", []DiffLine{{DiffSame, "a"}, {DiffSame, "b"}}},
		{"change: delete before add", "a\nb\nc", "a\nx\nc",
			[]DiffLine{{DiffSame, "a"}, {DiffDel, "b"}, {DiffAdd, "x"}, {DiffSame, "c"}}},
		{"append", "a", "a\nb", []DiffLine{{DiffSame, "a"}, {DiffAdd, "b"}}},
		{"remove tail", "a\nb", "a", []DiffLine{{DiffSame, "a"}, {DiffDel, "b"}}},
		{"insert middle", "a\nc", "a\nb\nc", []DiffLine{{DiffSame, "a"}, {DiffAdd, "b"}, {DiffSame, "c"}}},
		{"all different", "a\nb", "c", []DiffLine{{DiffDel, "a"}, {DiffDel, "b"}, {DiffAdd, "c"}}},
		{"empty to text", "", "x", []DiffLine{{DiffDel, ""}, {DiffAdd, "x"}}},
	}
	for _, tc := range tests {
		if got := DiffLines(tc.a, tc.b); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}
