package design

import "testing"

func TestCn(t *testing.T) {
	for _, c := range []struct {
		in   []string
		want string
	}{
		{[]string{"px-4 py-2", "", "  font-medium "}, "px-4 py-2 font-medium"},

		// Padding and margin, per side.
		{[]string{"px-4 py-2", "px-2"}, "py-2 px-2"},
		{[]string{"px-2 pt-1", "p-4"}, "p-4"},
		{[]string{"p-4", "px-2"}, "p-4 px-2"},
		{[]string{"py-2", "pt-0"}, "py-2 pt-0"},
		{[]string{"pt-1 pb-1", "py-3"}, "py-3"},
		{[]string{"pl-2", "px-3"}, "px-3"},
		{[]string{"mt-2 mx-auto", "-mt-1"}, "mx-auto -mt-1"},
		{[]string{"m-2", "mx-auto"}, "m-2 mx-auto"},
		{[]string{"p-2", "m-2"}, "p-2 m-2"},

		// The background colour, not its other utilities.
		{[]string{"bg-primary hover:bg-primary/90", "bg-red-600"}, "hover:bg-primary/90 bg-red-600"},
		{[]string{"bg-cover bg-center bg-muted", "bg-[#fafafa]"}, "bg-cover bg-center bg-[#fafafa]"},
		{[]string{"bg-[url(/a.png)]", "bg-white"}, "bg-[url(/a.png)] bg-white"},

		// Text colour and size are apart.
		{[]string{"text-sm text-muted-foreground", "text-red-600"}, "text-sm text-red-600"},
		{[]string{"text-sm text-foreground", "text-lg"}, "text-foreground text-lg"},
		{[]string{"text-sm/6", "text-base"}, "text-base"},
		{[]string{"text-[13px] text-white", "text-xs"}, "text-white text-xs"},
		{[]string{"text-left text-sm", "text-center"}, "text-sm text-center"},

		// Rounded corners.
		{[]string{"rounded-md", "rounded-full"}, "rounded-full"},
		{[]string{"rounded", "rounded-lg"}, "rounded-lg"},
		{[]string{"rounded-t-lg rounded-tl-none", "rounded-xl"}, "rounded-xl"},
		{[]string{"rounded-xl", "rounded-t-none"}, "rounded-xl rounded-t-none"},
		{[]string{"rounded-tl-sm", "rounded-t-lg"}, "rounded-t-lg"},

		// Font weight and family.
		{[]string{"font-medium font-mono", "font-bold"}, "font-mono font-bold"},
		{[]string{"font-sans", "font-serif"}, "font-serif"},

		// Display.
		{[]string{"inline-flex items-center", "hidden"}, "items-center hidden"},
		{[]string{"flex flex-1", "grid"}, "flex-1 grid"},

		// Width, height and size.
		{[]string{"w-full h-9", "w-auto"}, "h-9 w-auto"},
		{[]string{"w-2 h-2", "size-4"}, "size-4"},
		{[]string{"size-9", "w-12"}, "size-9 w-12"},
		{[]string{"min-w-0 w-full", "w-1/2"}, "min-w-0 w-1/2"},

		// Gap.
		{[]string{"gap-x-2 gap-y-1", "gap-4"}, "gap-4"},
		{[]string{"gap-2", "gap-x-4"}, "gap-2 gap-x-4"},

		// Variants and importance.
		{[]string{"hover:bg-accent bg-transparent", "hover:bg-muted"}, "bg-transparent hover:bg-muted"},
		{[]string{"md:hover:px-2", "hover:md:px-4"}, "hover:md:px-4"},
		{[]string{"[&_svg]:size-4", "[&_svg]:size-3"}, "[&_svg]:size-3"},
		{[]string{"!p-0 p-4", "p-2"}, "!p-0 p-2"},
		{[]string{"focus-visible:ring-[3px] ring-ring", "focus-visible:ring-2"}, "focus-visible:ring-[3px] ring-ring focus-visible:ring-2"},

		// Classes cn doesn't know are kept; a repeat keeps the last.
		{[]string{"shadow-xs border", "shadow-xs underline"}, "border shadow-xs underline"},
	} {
		if got := cn(c.in...); got != c.want {
			t.Errorf("cn(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
