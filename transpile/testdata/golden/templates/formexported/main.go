package main

import (
	"bytes"
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
)

func main() {
	var b bytes.Buffer
	info, err := ui.RenderPage(context.Background(), &SignIn{}, &b)
	fmt.Println(info.Live, bytes.Contains(b.Bytes(), []byte("data-vk-http")), err)
}
