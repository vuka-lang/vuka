package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
	"vukatest.local/stateui/uigen"
)

type Owner struct{ Name string }

func main() {
	s := &Search{}
	uigen.States(s)
	s.tags.Set([]string{"a", "b"})
	s.Find("pets")
	s.Unlimit()
	show := func() {
		out, err := ui.String(context.Background(), s.Render())
		fmt.Println(out, err)
	}
	show()
	fmt.Println(s.Move(4))
	s.Clear()
	show()
	s.Retire()
	show()
	fmt.Println(s.tags.Get() == nil, s.owner == nil)
}
