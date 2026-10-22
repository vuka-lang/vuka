// What vuka gen writes for page.vuka.html, as the analyzer sees an app.

package shape

type Page struct {
	Title string
}

func (p Page) Upper() string { return p.Title }

func header() string { return "h" }

func Title() string { return "package" }
