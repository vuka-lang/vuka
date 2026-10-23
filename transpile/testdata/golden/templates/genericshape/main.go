package main

import (
	"context"
	"fmt"
	"net/url"

	"github.com/vuka-lang/ui/v2"
)

type Task struct{ Name string }
type User struct{ Email string }

func main() {
	u, _ := url.Parse("/?tasks.page=3&users.sort=email")
	ctx := ui.WithPageURL(context.Background(), u, func(string) string { return "" })
	tasks := &Table[Task]{Title: "tasks", Rows: []Task{{"a"}, {"b"}}}
	users := &Table[User]{Title: "users", Rows: []User{{"x"}}}
	box := &Box[int]{Items: []int{1, 2, 3}}
	out, err := ui.String(ctx, ui.HTML(`{a}{b}{c}`, ui.URLKey("tasks", tasks), ui.URLKey("users", users), box))
	fmt.Println(out, err)
}
