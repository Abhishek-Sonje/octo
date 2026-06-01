package tunnel

import (
	"fmt"
	"sync"
	"testing"
)

func TestRegistryBasic(t *testing.T) {
	reg := NewRegistry()

	if count := reg.Count(); count != 0 {
		t.Errorf("expected count 0, got %d", count)
	}

	sessionID := "test-session"
	s := reg.Register(sessionID, nil)
	if s == nil {
		t.Fatal("expected session to be registered, got nil")
	}

	if count := reg.Count(); count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}

	s2, ok := reg.Get(sessionID)
	if !ok || s2 != s {
		t.Errorf("Get failed to retrieve correct session")
	}

	reg.Remove(sessionID)
	if count := reg.Count(); count != 0 {
		t.Errorf("expected count 0 after removal, got %d", count)
	}

	_, ok = reg.Get(sessionID)
	if ok {
		t.Errorf("expected session to be removed")
	}
}

func TestRegistryConcurrent(t *testing.T) {
	reg := NewRegistry()
	numRoutines := 100
	var wg sync.WaitGroup

	// Concurrent registers
	for i := 0; i < numRoutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sessionID := fmt.Sprintf("session-%d", id)
			reg.Register(sessionID, nil)
		}(i)
	}
	wg.Wait()

	if count := reg.Count(); count != numRoutines {
		t.Errorf("expected count %d, got %d", numRoutines, count)
	}

	// Concurrent reads and removes
	for i := 0; i < numRoutines; i++ {
		wg.Add(2)
		// Reader
		go func(id int) {
			defer wg.Done()
			sessionID := fmt.Sprintf("session-%d", id)
			reg.Get(sessionID)
		}(i)
		// Writer (remover)
		go func(id int) {
			defer wg.Done()
			sessionID := fmt.Sprintf("session-%d", id)
			reg.Remove(sessionID)
		}(i)
	}
	wg.Wait()

	if count := reg.Count(); count != 0 {
		t.Errorf("expected registry to be empty after concurrent removals, got %d", count)
	}
}

func TestRegistryClose(t *testing.T) {
	reg := NewRegistry()
	reg.Register("s1", nil)
	reg.Register("s2", nil)

	if count := reg.Count(); count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}

	reg.Close()

	if count := reg.Count(); count != 0 {
		t.Errorf("expected registry to be empty after Close, got %d", count)
	}
}
