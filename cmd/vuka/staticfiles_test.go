package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const staticMain = `package main

import (
	"embed"
	"io/fs"
	"os"

	"example.com/app/pages"
	"github.com/vuka-lang/web/v2"
)

//go:embed static
var static embed.FS

//go:embed public
var public embed.FS

func main() {
	app := web.New(pages.Routes())
	assets, _ := fs.Sub(static, "static")
	app.Static("/static", assets)
	app.Static("/public", public)
	app.Static("/uploads", os.DirFS("uploads"))
	if err := app.Run(""); err != nil {
		panic(err)
	}
}
`

const staticPage = `<script lang="go">
package pages

import "github.com/vuka-lang/web/v2"

type Index struct{}
</script>

<img src={web.Asset("/static/images/logo.png")} alt="Logo" />
<link rel="stylesheet" href={web.Asset("/static/site.css")} />
<img src={web.Asset("/uploads/avatar.png")} alt="" />
`

func staticTree(t *testing.T) string {
	t.Helper()
	return tempModule(t, map[string]string{
		"go.mod":                    "module example.com/app\n\ngo 1.25.0\n\nrequire github.com/vuka-lang/web/v2 v2.0.0\n",
		"main.go":                   staticMain,
		"pages/index.vuka.html":     staticPage,
		"static/images/logo.png":    "\x89PNG logo",
		"static/site.css":           "body{background:url(images/logo.png)}",
		"static/.hidden":            "x",
		"public/robots.txt":         "User-agent: *",
		"uploads/avatar.png":        "avatar",
		"pages/other.go":            "package pages\n\nimport \"github.com/vuka-lang/web/v2\"\n\nvar _ = web.Asset\n",
		"pages/plain.vuka.html":     "<script lang=\"go\">\npackage pages\n</script>\n\n<img src=\"/static/images/logo.png\" srcset=\"/static/images/logo.png 1x, /x.png 2x\"/>\n<a href=\"/elsewhere\">x</a>\n",
		"pages/handler.go":          "package pages\n\nimport (\n\t\"context\"\n\n\tvw \"github.com/vuka-lang/web/v2\"\n)\n\nfunc logo(ctx context.Context, p string) string {\n\t// vw.Asset(ctx, \"/static/commented.png\")\n\t_ = vw.Asset(ctx, p)\n\treturn vw.AssetURL(ctx, \"/static/images/lgo.png\")\n}\n",
		"pages/typo.vuka.html":      "<script lang=\"go\">\npackage pages\n\nimport \"github.com/vuka-lang/web/v2\"\n</script>\n\n<img src={web.Asset(\"/statc/images/logo.png\")} />\n<img src={web.Asset(\"/nowhere/x.png\")} />\n",
		"static/images/logo.png.br": "br",
	})
}

func TestStaticMounts(t *testing.T) {
	root := staticTree(t)
	mounts := findStaticMounts(root)
	got := map[string]bool{}
	for _, m := range mounts {
		got[m.prefix] = m.embedded()
	}
	if len(mounts) != 3 || !got["/static"] || !got["/public"] || got["/uploads"] {
		t.Fatalf("mounts: %v", got)
	}
	urls := staticURLs(mounts)
	for _, u := range []string{"/static/images/logo.png", "/static/site.css", "/public/public/robots.txt"} {
		if urls[u] == "" {
			t.Errorf("no %s in %v", u, urls)
		}
	}
	if _, ok := urls["/static/.hidden"]; ok {
		t.Error("a dotfile is a static file")
	}

	m, err := staticManifest(mounts)
	if err != nil {
		t.Fatal(err)
	}
	logo := m["/static/images/logo.png"]
	if !regexp.MustCompile(`^/static/images/logo\.[0-9a-f]{8}\.png$`).MatchString(logo) {
		t.Errorf("logo: %q", logo)
	}
	if _, ok := m["/static/images/logo.png.br"]; ok {
		t.Error("a precompressed sibling is in the manifest")
	}
	css := m["/static/site.css"]
	if css == "" || css == hashedName("/static/site.css", []byte("body{background:url(images/logo.png)}")) {
		t.Errorf("the stylesheet's hash doesn't cover the image's: %q", css)
	}
	if got := string(rewriteCSSURLs([]byte(`a{b:url("/static/images/logo.png")} c{d:url(images/logo.png#x)}`), "/static/site.css", m)); got !=
		`a{b:url("`+logo+`")} c{d:url(images/`+filepath.Base(logo)+`#x)}` {
		t.Errorf("rewritten: %s", got)
	}
	src := string(staticGo(m))
	if !strings.Contains(src, `vukaweb.RegisterStatic(map[string]string{`) || !strings.Contains(src, `"/static/images/logo.png": "`+logo+`"`) {
		t.Errorf("the generated file:\n%s", src)
	}
}

func TestStaticDiags(t *testing.T) {
	root := staticTree(t)
	t.Chdir(root)
	var b bytes.Buffer
	ds := staticDiags(root, true)
	writeDiags(&b, ds, false)
	out := b.String()
	for _, want := range []string{
		"error[E0429]: /static/images/lgo.png isn't a static file", "--> pages/handler.go:12:26",
		"help: did you mean /static/images/logo.png?",
		"error[E0429]: /statc/images/logo.png isn't a static file", "--> pages/typo.vuka.html:7:21",
		"warning[W0404]: src=\"/static/images/logo.png\" is written by hand",
		"warning[W0404]: srcset=\"/static/images/logo.png 1x, /x.png 2x\" is written by hand",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	for _, not := range []string{"avatar", "commented", "/nowhere/", "/elsewhere", "site.css", "index.vuka.html"} {
		if strings.Contains(out, not) {
			t.Errorf("reports %s:\n%s", not, out)
		}
	}
	if n := len(staticDiags(root, false)); n != 2 {
		t.Errorf("without warnings: %d diagnostics", n)
	}
}

func TestFixStaticPaths(t *testing.T) {
	root := staticTree(t)
	mounts := findStaticMounts(root)
	file := filepath.Join(root, "pages", "plain.vuka.html")
	src, _ := os.ReadFile(file)
	got := string(spliceEdits(src, staticPathEdits(file, src, mounts)))
	for _, want := range []string{
		`import "github.com/vuka-lang/web/v2"`,
		`<img src={web.Asset("/static/images/logo.png")} srcset={web.Asset("/static/images/logo.png") + " 1x, /x.png 2x"}/>`,
		`<a href="/elsewhere">x</a>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %s:\n%s", want, got)
		}
	}
	if eds := staticPathEdits(file, []byte(got), mounts); len(eds) != 0 {
		t.Errorf("fixing twice: %v", eds)
	}
}

// TestStaticAssetsE2E: vuka build hashes the embedded static files; the page
// links the hashed name, served cached for good; a literal path naming no
// file fails the build with a suggestion.
func TestStaticAssetsE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	web := webCheckout(t)
	if !hasWebFunc(web, "RegisterStatic") {
		needs(t, "github.com/vuka-lang/web/v2 at %s has no RegisterStatic; set VUKA_WEB", web)
	}
	root := localModule(t, web, map[string]string{
		"main.go":                staticMain,
		"pages/index.vuka.html":  staticPage,
		"static/images/logo.png": "\x89PNG logo",
		"static/site.css":        "body{background:url(images/logo.png)}",
		"public/robots.txt":      "User-agent: *",
	})
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv(twEnv, "off")
	t.Chdir(root)
	app := filepath.Join(t.TempDir(), "app")
	var stderr bytes.Buffer
	if err := runGo("build", []string{"-o", app, "."}, io.Discard, &stderr); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, stderr.Bytes())
	}
	if _, err := os.Stat(filepath.Join(root, staticGoName)); err == nil {
		t.Errorf("vuka build wrote %s into the source tree", staticGoName)
	}

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cmd := exec.Command(app)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), fmt.Sprint("PORT=", port), twCSSEnv+"=", assetsEnv+"=")
	var appOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &appOut, &appOut
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		if t.Failed() {
			t.Logf("the app says:\n%s", appOut.String())
		}
	})
	get := func(path string) (string, http.Header, int) {
		var res *http.Response
		waitFor(t, func() bool {
			var err error
			res, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
			return err == nil
		})
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return string(b), res.Header, res.StatusCode
	}
	page, _, _ := get("/")
	logo := regexp.MustCompile(`/static/images/logo\.[0-9a-f]{8}\.png`).FindString(page)
	css := regexp.MustCompile(`/static/site\.[0-9a-f]{8}\.css`).FindString(page)
	if logo == "" || css == "" || !strings.Contains(page, `src="/uploads/avatar.png"`) {
		t.Fatalf("the page doesn't link the hashed names:\n%s", page)
	}
	body, hdr, code := get(logo)
	if code != 200 || body != "\x89PNG logo" || hdr.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Errorf("%s: %d %q %v", logo, code, body, hdr)
	}
	if body, _, _ := get(css); !strings.Contains(body, "url(images/"+filepath.Base(logo)+")") {
		t.Errorf("%s: %s", css, body)
	}
	if _, _, code := get("/static/images/logo.png"); code != 200 {
		t.Errorf("the plain name: %d", code)
	}

	os.WriteFile(filepath.Join(root, "pages", "typo.vuka.html"), []byte("<script lang=\"go\">\npackage pages\n\nimport \"github.com/vuka-lang/web/v2\"\n\ntype Typo struct{}\n</script>\n\n<img src={web.Asset(\"/static/images/lgo.png\")} />\n"), 0o644)
	stderr.Reset()
	err := runGo("build", []string{"-o", app, "."}, io.Discard, &stderr)
	if err == nil {
		t.Fatal("the build passed with a missing static file")
	}
	if out := stderr.String() + err.Error(); !strings.Contains(out, "error[E0429]: /static/images/lgo.png isn't a static file") ||
		!strings.Contains(out, "pages/typo.vuka.html:9:21") || !strings.Contains(out, "did you mean /static/images/logo.png?") {
		t.Errorf("vuka build says:\n%s", out)
	}
}
