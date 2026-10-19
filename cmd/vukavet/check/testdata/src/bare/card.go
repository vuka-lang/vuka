package bare

type Card struct {
	Title string
	Icon  string
	Items []string
}

func (c Card) Label() string { return "card" }

func Title() string { return "package" }

func Icon(name string) string { return name }

var Label = "package"
