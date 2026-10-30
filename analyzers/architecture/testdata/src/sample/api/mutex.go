package api

import "sync"

// Ledger guards its balances with a lock. Locking and unlocking change only
// the lock, so a handle whose methods lock to read stays a read-only handle.
type Ledger struct {
	mu       sync.RWMutex
	balances map[string]int
}

// Account reads one balance under the ledger's read lock.
type Account struct {
	ledger *Ledger
	name   string
}

func (a Account) Balance() int {
	a.ledger.mu.RLock()
	defer a.ledger.mu.RUnlock()
	return a.ledger.balances[a.name]
}

// Closer clears every balance under the ledger's write lock; the clear is a
// write, not the lock.
type Closer struct{ ledger *Ledger }

func (c Closer) Close() {
	c.ledger.mu.Lock()
	defer c.ledger.mu.Unlock()
	clear(c.ledger.balances)
}

type Books struct{ ledger *Ledger }

func (b *Books) Account(name string) Account { return Account{b.ledger, name} }

func (b *Books) Closer() Closer { return Closer{b.ledger} } // want "ARC003: Closer result 1"
