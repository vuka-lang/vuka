// Package store is the example's data, in memory.
package store

import (
	"slices"
	"sync"
)

type Employer struct {
	ID   int
	Name string
	City string
}

type Permit struct {
	ID       int
	Employer int
	Kind     string
	Approved bool
}

// EmployerForm is the form of the new and edit pages.
type EmployerForm struct {
	Name string `form:"name" validate:"required"`
	City string `form:"city" validate:"required"`
}

type Store struct {
	mu        sync.Mutex
	next      int
	employers map[int]Employer
	permits   map[int]Permit
	files     map[string]string
}

func New() *Store {
	return &Store{
		next: 3,
		employers: map[int]Employer{
			1: {ID: 1, Name: "Acme Logistics", City: "Dar es Salaam"},
			2: {ID: 2, Name: "Kilima Foods", City: "Arusha"},
		},
		permits: map[int]Permit{
			1: {ID: 1, Employer: 1, Kind: "Work permit"},
			2: {ID: 2, Employer: 1, Kind: "Residence permit", Approved: true},
			3: {ID: 3, Employer: 2, Kind: "Work permit"},
		},
		files: map[string]string{
			"handbook/leave.txt": "Every employee has 28 days of paid leave a year.",
			"contracts/1.txt":    "Acme Logistics: services agreement, 2026.",
			"contracts/2.txt":    "Kilima Foods: services agreement, 2026.",
		},
	}
}

func (s *Store) Employers() []Employer {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Employer, 0, len(s.employers))
	for _, e := range s.employers {
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b Employer) int { return a.ID - b.ID })
	return out
}

func (s *Store) Employer(id int) (Employer, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.employers[id]
	return e, ok
}

func (s *Store) AddEmployer(f EmployerForm) Employer {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := Employer{ID: s.next, Name: f.Name, City: f.City}
	s.employers[e.ID] = e
	s.next++
	return e
}

func (s *Store) UpdateEmployer(id int, f EmployerForm) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.employers[id]; !ok {
		return false
	}
	s.employers[id] = Employer{ID: id, Name: f.Name, City: f.City}
	return true
}

func (s *Store) DeleteEmployer(id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.employers[id]; !ok {
		return false
	}
	delete(s.employers, id)
	for pid, p := range s.permits {
		if p.Employer == id {
			delete(s.permits, pid)
		}
	}
	return true
}

func (s *Store) Permits(employer int) []Permit {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Permit
	for _, p := range s.permits {
		if p.Employer == employer {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b Permit) int { return a.ID - b.ID })
	return out
}

// Permit is the employer's permit id: another employer's is not found.
func (s *Store) Permit(employer, id int) (Permit, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.permits[id]
	return p, ok && p.Employer == employer
}

func (s *Store) Approve(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.permits[id]; ok {
		p.Approved = true
		s.permits[id] = p
	}
}

func (s *Store) File(path string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[path]
	return f, ok
}
