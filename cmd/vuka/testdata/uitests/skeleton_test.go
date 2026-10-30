package design

import (
	"context"
	"testing"

	"github.com/vuka-lang/ui/v2"
)

func TestSkeletonMarkup(t *testing.T) {
	for _, c := range []struct {
		name string
		node ui.Node
		want string
	}{
		{"rectangular", Skeleton{}, `<div class="block bg-skeleton h-4 w-full skeleton-pulse" aria-hidden="true"></div>`},
		{"rounded", Skeleton{Variant: SkeletonRounded}, `<div class="block bg-skeleton h-4 w-full rounded-md skeleton-pulse" aria-hidden="true"></div>`},
		{"circular", Skeleton{Variant: SkeletonCircular}, `<div class="block bg-skeleton size-10 rounded-full skeleton-pulse" aria-hidden="true"></div>`},
		{"text", Skeleton{Variant: SkeletonText}, `<div class="block bg-skeleton my-[calc((1lh-1em)/2)] h-[1em] w-full rounded-sm skeleton-pulse" aria-hidden="true"></div>`},
		{"wave", Skeleton{Animation: SkeletonWave}, `<div class="block bg-skeleton h-4 w-full skeleton-wave" aria-hidden="true"></div>`},
		{"still", Skeleton{Animation: SkeletonNone}, `<div class="block bg-skeleton h-4 w-full" aria-hidden="true"></div>`},
		{"class wins", Skeleton{Variant: SkeletonCircular, Class: "size-16 bg-primary/20", Attrs: []ui.Attr{{Name: "id", Value: "avatar"}}}, `<div class="block rounded-full skeleton-pulse size-16 bg-primary/20" aria-hidden="true" id="avatar"></div>`},
		{"lines", Skeleton{Lines: 3, Class: "w-64"}, `<div class="flex flex-col w-64" aria-hidden="true"><div class="block bg-skeleton my-[calc((1lh-1em)/2)] h-[1em] w-full rounded-sm skeleton-pulse"></div><div class="block bg-skeleton my-[calc((1lh-1em)/2)] h-[1em] w-full rounded-sm skeleton-pulse"></div><div class="block bg-skeleton my-[calc((1lh-1em)/2)] h-[1em] rounded-sm skeleton-pulse w-4/5"></div></div>`},
		{"one line", Skeleton{Lines: 1}, `<div class="flex flex-col" aria-hidden="true"><div class="block bg-skeleton my-[calc((1lh-1em)/2)] h-[1em] w-full rounded-sm skeleton-pulse"></div></div>`},
		{"loading", Skeleton{Loading: true, Variant: SkeletonRounded, Children: ui.Text("Publish")}, `<div class="relative w-fit" role="status" aria-busy="true"><span class="sr-only">Loading…</span><div class="contents invisible" inert>Publish</div><div aria-hidden="true" class="block bg-skeleton inset-0 rounded-md skeleton-pulse absolute"></div></div>`},
		{"loaded", Skeleton{Variant: SkeletonRounded, Children: ui.Text("Publish")}, `<div class="relative w-fit"><div class="contents">Publish</div></div>`},
		{"loading text", Skeleton{Loading: true, Variant: SkeletonText, Label: "Loading the title", Children: ui.Text("Shipping notes")}, `<div class="relative w-fit" role="status" aria-busy="true"><span class="sr-only">Loading the title</span><div class="contents invisible" inert>Shipping notes</div><div aria-hidden="true" class="block bg-skeleton inset-x-0 inset-y-[calc((1lh-1em)/2)] rounded-sm skeleton-pulse absolute"></div></div>`},
		{"of", SkeletonOf{Children: ui.El("p", nil, ui.Text(Words(3)))}, `<div role="status" aria-busy="true" class="relative"><span class="sr-only">Loading…</span><div class="contents" data-skeleton-of="pulse" inert aria-hidden="true"><p>lorem ipsum dolor</p></div></div>`},
		{"of repeat", SkeletonOf{Repeat: 2, Animation: SkeletonWave, Label: "Loading posts", Class: "grid gap-2", Children: ui.El("p", nil, ui.Text(Chars(5)))}, `<div role="status" aria-busy="true" class="relative grid gap-2"><span class="sr-only">Loading posts</span><div class="contents" data-skeleton-of="wave" inert aria-hidden="true"><p>xxxxx</p><p>xxxxx</p></div></div>`},
	} {
		got, err := ui.String(context.Background(), c.node)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}

func TestPlaceholderText(t *testing.T) {
	if got := Words(3); got != "lorem ipsum dolor" {
		t.Errorf("Words(3) = %q", got)
	}
	if got := len(Words(40)); got == 0 {
		t.Errorf("Words(40) is empty")
	}
	if got := Chars(4); got != "xxxx" {
		t.Errorf("Chars(4) = %q", got)
	}
	if Words(0) != "" || Chars(-1) != "" {
		t.Errorf("Words(0), Chars(-1) aren't empty")
	}
}
