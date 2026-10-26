// Package shop is a pet shop's data, in plain Go: the types its GraphQL
// schema serves.
package shop

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Pet is an animal for sale.
type Pet struct {
	ID      int64 `graphql:",id"`
	Name    string
	Kind    Kind
	Tag     *string
	Born    time.Time
	Price   float64
	Tricks  []string `graphql:",omitempty"`
	OwnerID int64    `graphql:"-"`
	notes   string
}

// Kind is what kind of animal a pet is.
type Kind string

const (
	KindDog  Kind = "dog"
	KindCat  Kind = "cat"
	KindBird Kind = "bird"
)

// Owner is who bought a pet.
type Owner struct {
	ID    int64 `graphql:",id"`
	Name  string
	Email string
}

// Store holds the shop's pets and owners in memory.
type Store struct {
	mu     sync.Mutex
	pets   []Pet
	owners []Owner
}

func NewStore() *Store {
	tag := "good boy"
	return &Store{
		pets: []Pet{
			{ID: 1, Name: "Rex", Kind: KindDog, Tag: &tag, Born: time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC), Price: 120, Tricks: []string{"sit", "roll"}, OwnerID: 1},
			{ID: 2, Name: "Tom", Kind: KindCat, Born: time.Date(2021, 7, 9, 12, 30, 0, 0, time.UTC), Price: 80.5},
			{ID: 3, Name: "Kiwi", Kind: KindBird, Born: time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC), Price: 25},
		},
		owners: []Owner{{ID: 1, Name: "Ada", Email: "ada@example.com"}},
	}
}

// Pets are the pets of kind (all of them for ""), at most first.
func (s *Store) Pets(kind Kind, first int) []Pet {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Pet
	for _, p := range s.pets {
		if (kind == "" || p.Kind == kind) && len(out) < first {
			out = append(out, p)
		}
	}
	return out
}

// Pet is the pet with id, nil for none.
func (s *Store) Pet(id int64) *Pet {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.pets {
		if s.pets[i].ID == id {
			p := s.pets[i]
			return &p
		}
	}
	return nil
}

// Add adds a pet.
func (s *Store) Add(p Pet) Pet {
	s.mu.Lock()
	defer s.mu.Unlock()
	p.ID = int64(len(s.pets) + 1)
	s.pets = append(s.pets, p)
	return p
}

// Named reports whether a pet is named name.
func (s *Store) Named(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.pets {
		if p.Name == name {
			return true
		}
	}
	return false
}

// ErrBroken is what a broken query returns: shown as "internal error".
var ErrBroken = errors.New("the database is on fire")

// Owner is the pet's owner, if it has one.
func (p Pet) Owner(ctx context.Context, store *Store) (*Owner, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, o := range store.owners {
		if o.ID == p.OwnerID {
			return &o, nil
		}
	}
	return nil, nil
}

// Age is the pet's age in whole years on the shop's clock.
func (p Pet) Age(at string) (int, error) {
	now, err := time.Parse(time.DateOnly, at)
	if err != nil {
		return 0, errors.New("at is a date, like 2026-01-31")
	}
	years := now.Year() - p.Born.Year()
	if now.YearDay() < p.Born.YearDay() {
		years--
	}
	return years, nil
}
