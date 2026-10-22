package component

import (
	"errors"
	"sync"

	"actionlint.kjanat.dev/internal/wasmbudget"
	"github.com/partite-ai/wacogo"
	"github.com/tetratelabs/wazero/experimental"
)

var errMemoryBudget = errors.New("aggregate plugin memory limit exceeded")

// memoryBudget accounts for live linear memories and reserved table maxima,
// including repeated and nested core instances.
// CoreFeaturesV2 excludes shared memory; these buffers may move when they grow.
type memoryBudget struct {
	mu          sync.Mutex
	limit, used uint64
}

func (b *memoryBudget) Allocate(_ uint64, maximum uint64) experimental.LinearMemory {
	return &budgetMemory{budget: b, maximum: maximum}
}

func (b *memoryBudget) allocateTables(tables []wacogo.CoreTable) (func(), error) {
	reserved, err := wasmbudget.Tables(tables, b.limit)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	if reserved > b.limit-b.used {
		b.mu.Unlock()
		return nil, errMemoryBudget
	}
	b.used += reserved
	b.mu.Unlock()
	return sync.OnceFunc(func() {
		b.mu.Lock()
		b.used -= reserved
		b.mu.Unlock()
	}), nil
}

type budgetMemory struct {
	budget      *memoryBudget
	maximum     uint64
	buffer      []byte
	initialized bool
	freed       bool
}

func (m *budgetMemory) Reallocate(size uint64) []byte {
	b := m.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if m.freed {
		return nil
	}
	capacity := uint64(cap(m.buffer))
	if size > m.maximum || size > uint64(int(^uint(0)>>1)) || size > capacity && size-capacity > b.limit-b.used {
		// wazero bounds-checks the initial result without an error return.
		// call catches only this admission failure and closes the runtime.
		if !m.initialized {
			panic(errMemoryBudget)
		}
		return nil // memory.grow must report failure to the guest.
	}
	if size > capacity {
		buffer := make([]byte, int(size))
		copy(buffer, m.buffer)
		m.buffer = buffer
		b.used += size - capacity
	} else {
		m.buffer = m.buffer[:int(size)]
	}
	m.initialized = true
	return m.buffer
}

func (m *budgetMemory) Free() {
	b := m.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if !m.freed {
		b.used -= uint64(cap(m.buffer))
		m.buffer = nil
		m.freed = true
	}
}
