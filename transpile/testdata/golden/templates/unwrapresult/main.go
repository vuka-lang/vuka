package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/vuka-lang/vuka/v2"
	ui "vukatest.local/stateui"
)

type User struct {
	ID   int
	Name string
}

type Store struct{ down bool }

func (s Store) Users() vuka.Result[[]User] {
	if s.down {
		return vuka.Err[[]User](errors.New("users: store down"))
	}
	return vuka.Ok([]User{{1, "ann"}, {2, "bob"}})
}

func (Store) Title() vuka.Result[string] { return vuka.Ok("Users") }
func (Store) Admin() vuka.Result[bool]   { return vuka.Ok(true) }

func main() {
	out, err := ui.String(context.Background(), Page{Store: Store{}, Count: vuka.Ok(2)})
	fmt.Println(out, err)
	fmt.Println(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("render failed: %v", r)
			}
		}()
		ui.String(context.Background(), Page{Store: Store{down: true}, Count: vuka.Ok(0)})
		return nil
	}())
}
