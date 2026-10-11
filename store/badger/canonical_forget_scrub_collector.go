package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

type scrubPayloadCollector struct {
	refs     []memory.PayloadRef
	seen     map[string]struct{}
	overflow bool
}

func newScrubPayloadCollector(size int) *scrubPayloadCollector {
	return &scrubPayloadCollector{
		refs: make([]memory.PayloadRef, 0, size),
		seen: make(map[string]struct{}, size),
	}
}

func (c *scrubPayloadCollector) add(ref memory.PayloadRef) {
	if ref.ID == "" {
		return
	}

	if _, exists := c.seen[ref.ID]; exists {
		return
	}

	if len(c.refs) >= maxCanonicalScrubPayloads {
		c.overflow = true

		return
	}

	c.seen[ref.ID] = struct{}{}
	c.refs = append(c.refs, ref)
}
