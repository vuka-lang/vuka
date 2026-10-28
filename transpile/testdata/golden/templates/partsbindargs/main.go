package main

import (
 "context"
 "fmt"
 ui "vukatest.local/stateui"
)

func main() {
 var calls []string
 out,err := ui.String(context.Background(),&Page{Calls:&calls})
 fmt.Println(out,err)
 fmt.Println(calls)
}
