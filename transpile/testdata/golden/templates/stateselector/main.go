package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
)

// Employer is the card's record.
type Employer struct {
	Name string
	Addr Addr
	Tags []string
}

// Addr is where it is.
type Addr struct{ City string }

// Label is how a card names it.
func (e Employer) Label() string { return e.Name + " (" + e.Addr.City + ")" }

func main() {
	c := &Card{}
	c.employer.Set(Employer{Tags: []string{"old"}})
	c.Load("Acme", "Arusha")
	c.Rename("Acme Ltd")
	out, err := ui.String(context.Background(), c)
	fmt.Println(c.Title())
	fmt.Println(out, err)
}

// Rename changes the record.
func (e *Employer) Rename(name string) { e.Name = name }

// Fix changes where it is.
func (a *Addr) Fix() { a.City += "!" }
