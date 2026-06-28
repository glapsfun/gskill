package selection

import "sort"

// Closest returns up to limit candidate names within a small edit distance of
// target, ordered by (distance, name) for determinism. It is used to suggest
// near-misses when a skill selector does not resolve (FR-017).
func Closest(target string, candidates []string, limit int) []string {
	type scored struct {
		name string
		dist int
	}
	threshold := len(target)/3 + 1 // scale tolerance to name length
	if threshold < 2 {
		threshold = 2
	}

	var hits []scored
	for _, c := range candidates {
		d := levenshtein(target, c)
		if d <= threshold {
			hits = append(hits, scored{c, d})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].dist != hits[j].dist {
			return hits[i].dist < hits[j].dist
		}
		return hits[i].name < hits[j].name
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.name
	}
	return out
}

// levenshtein returns the edit distance between a and b.
func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
