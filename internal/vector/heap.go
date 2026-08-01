package vector

import "sort"

// minHeap keeps the Top-K largest scores in a fixed-capacity min-heap.
type minHeap struct {
	items []Result
}

func (h *minHeap) push(r Result) {
	if len(h.items) < cap(h.items) {
		h.items = append(h.items, r)
		h.up(len(h.items) - 1)
		return
	}
	if len(h.items) == 0 {
		return
	}
	// If r is larger than the current minimum, replace the root.
	if r.Score > h.items[0].Score {
		h.items[0] = r
		h.down(0)
	}
}

func (h *minHeap) up(i int) {
	for {
		p := (i - 1) / 2
		if p == i || h.items[p].Score <= h.items[i].Score {
			break
		}
		h.items[p], h.items[i] = h.items[i], h.items[p]
		i = p
	}
}

func (h *minHeap) down(i int) {
	n := len(h.items)
	for {
		l, r, m := 2*i+1, 2*i+2, i
		if l < n && h.items[l].Score < h.items[m].Score {
			m = l
		}
		if r < n && h.items[r].Score < h.items[m].Score {
			m = r
		}
		if m == i {
			break
		}
		h.items[m], h.items[i] = h.items[i], h.items[m]
		i = m
	}
}

// sortSlice sorts Results descending by Score (best first).
func sortSlice(items []Result) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Score > items[j].Score
	})
}
