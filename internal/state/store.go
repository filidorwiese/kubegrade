// Package state keeps first-seen timestamps for conditions the API does not
// date itself. Lives in process memory only; resets on restart.
package state

import (
	"strings"
	"sync"
	"time"
)

type Store struct {
	mu    sync.Mutex
	start time.Time
	first map[string]time.Time
}

func New() *Store {
	return &Store{start: time.Now(), first: map[string]time.Time{}}
}

func (s *Store) Start() time.Time { return s.start }

// FirstSeen returns when key was first observed, recording now if new.
func (s *Store) FirstSeen(key string, now time.Time) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.first[key]; ok {
		return t
	}
	s.first[key] = now
	return now
}

// PrunePrefix drops every key under prefix that is not in keep, so a
// condition that cleared and comes back starts its clock again.
func (s *Store) PrunePrefix(prefix string, keep map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.first {
		if strings.HasPrefix(k, prefix) && !keep[k] {
			delete(s.first, k)
		}
	}
}
