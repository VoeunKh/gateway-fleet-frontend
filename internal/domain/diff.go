package domain

import "strings"

// DiffOp is the kind of a diff line.
type DiffOp byte

// Diff operations.
const (
	DiffSame DiffOp = '='
	DiffAdd  DiffOp = '+'
	DiffDel  DiffOp = '-'
)

// DiffLine is one line of a line diff.
type DiffLine struct {
	Op   DiffOp
	Text string
}

// DiffLines computes an LCS line diff from a to b with the sample's tie-break
// (deletions before additions).
//
// sample: diffLines(a, b) — if (L[i+1][j] >= L[i][j+1]) out.push(['-',A[i++]]); else out.push(['+',B[j++]]);
func DiffLines(a, b string) []DiffLine {
	A, B := strings.Split(a, "\n"), strings.Split(b, "\n")
	n, m := len(A), len(B)
	L := make([][]int, n+1)
	for i := range L {
		L[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if A[i] == B[j] {
				L[i][j] = L[i+1][j+1] + 1
			} else {
				L[i][j] = max(L[i+1][j], L[i][j+1])
			}
		}
	}
	out := make([]DiffLine, 0, max(n, m))
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case A[i] == B[j]:
			out = append(out, DiffLine{DiffSame, A[i]})
			i++
			j++
		case L[i+1][j] >= L[i][j+1]:
			out = append(out, DiffLine{DiffDel, A[i]})
			i++
		default:
			out = append(out, DiffLine{DiffAdd, B[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, DiffLine{DiffDel, A[i]})
	}
	for ; j < m; j++ {
		out = append(out, DiffLine{DiffAdd, B[j]})
	}
	return out
}
