package harness

import (
	"fmt"
	"strconv"
)

// maxDiffLines caps the LCS table size. Beyond this, a file diff falls back to a
// whole-file replacement block to bound memory and time.
const maxDiffLines = 1500

type diffOp struct {
	kind byte // ' ' context, '-' removed, '+' added
	text string
}

// diffLineOps returns a line-level edit script between a and b using a longest
// common subsequence. For very large inputs it falls back to a naive
// delete-all-then-insert-all script.
func diffLineOps(a, b []string) []diffOp {
	n, m := len(a), len(b)
	if n > maxDiffLines || m > maxDiffLines {
		ops := make([]diffOp, 0, n+m)
		for _, line := range a {
			ops = append(ops, diffOp{'-', line})
		}
		for _, line := range b {
			ops = append(ops, diffOp{'+', line})
		}
		return ops
	}
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffOp{'-', a[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{'+', b[j]})
	}
	return ops
}

// emitUnifiedHunks writes standard unified-diff hunks (with @@ headers and the
// given amount of surrounding context) for the edit script between a and b.
// Each line is passed to write, which may stop accepting input once a global
// truncation limit is reached.
func emitUnifiedHunks(a, b []string, context int, write func(string)) {
	ops := diffLineOps(a, b)
	n := len(ops)
	if n == 0 {
		return
	}
	changed := make([]bool, n)
	oldBefore := make([]int, n+1)
	newBefore := make([]int, n+1)
	anyChange := false
	for k, op := range ops {
		oldBefore[k+1] = oldBefore[k]
		newBefore[k+1] = newBefore[k]
		switch op.kind {
		case ' ':
			oldBefore[k+1]++
			newBefore[k+1]++
		case '-':
			oldBefore[k+1]++
			changed[k] = true
			anyChange = true
		case '+':
			newBefore[k+1]++
			changed[k] = true
			anyChange = true
		}
	}
	if !anyChange {
		return
	}

	for k := 0; k < n; {
		if !changed[k] {
			k++
			continue
		}
		start := k - context
		if start < 0 {
			start = 0
		}
		end := k + 1
		for {
			next := -1
			for j := end; j < n && j <= end+context; j++ {
				if changed[j] {
					next = j
					break
				}
			}
			if next == -1 {
				break
			}
			end = next + 1
		}
		end += context
		if end > n {
			end = n
		}

		oldCount := oldBefore[end] - oldBefore[start]
		newCount := newBefore[end] - newBefore[start]
		oldStart := oldBefore[start]
		if oldCount > 0 {
			oldStart++
		}
		newStart := newBefore[start]
		if newCount > 0 {
			newStart++
		}
		write(fmt.Sprintf("@@ -%s +%s @@\n", hunkRange(oldStart, oldCount), hunkRange(newStart, newCount)))
		for _, op := range ops[start:end] {
			write(string(op.kind) + op.text + "\n")
		}
		k = end
	}
}

func hunkRange(start, count int) string {
	if count == 1 {
		return strconv.Itoa(start)
	}
	return strconv.Itoa(start) + "," + strconv.Itoa(count)
}
