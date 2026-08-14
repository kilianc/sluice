// Command build renders the repository's markdown into the docs site.
//
// It is a separate module so that the library keeps depending on nothing
// outside the standard library while this can use a real markdown parser —
// the docs use tables, and hand-rolling GFM is how you end up with a site that
// quietly mangles a row.
//
//	go run ./site/build -out .site/docs
package main

import (
	"bytes"
	"flag"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	gmhtml "github.com/yuin/goldmark/renderer/html"
)

// page is one markdown file, and where it belongs in the sidebar.
type page struct {
	Source  string // path relative to the repository root
	Slug    string // output name, without .html
	Title   string // sidebar label
	Section string
	Blurb   string
}

var pages = []page{
	{"docs/tutorial-postgres.md", "tutorial-postgres", "Postgres in 20 minutes", "Guides",
		"Put a filter bar in front of a database you already have."},
	{"docs/language.md", "language", "The language", "Reference",
		"Everything someone can type, in full."},
	{"docs/schema.md", "schema", "Schemas", "Reference",
		"Declaring fields, dynamic enums, custom emitters."},
	{"docs/dialects.md", "dialects", "Dialects", "Reference",
		"What a dialect controls, and writing one."},
	{"docs/editor.md", "editor", "Editors", "Reference",
		"Completions and diagnostics in a filter bar."},
	{"docs/security.md", "security", "Security", "Reference",
		"The invariants, and compiling in the browser."},
	{"docs/porting.md", "porting", "Porting", "Reference",
		"Implementing Sluice in another language."},
	{"AGENTS.md", "spec", "Specification", "Project",
		"The normative document. Everything else is a projection of it."},
	{"CONTRIBUTING.md", "contributing", "Contributing", "Project",
		"The corpus rule, the versioning policy, releasing."},
	{"PLAN.md", "plan", "Plan", "Project",
		"The roadmap and the decision log."},
}

const repo = "https://github.com/kilianc/sluice/blob/main/"

func main() {
	out := flag.String("out", ".site/docs", "output directory")
	root := flag.String("root", ".", "repository root")
	flag.Parse()

	if err := run(*root, *out); err != nil {
		fmt.Fprintln(os.Stderr, "site/build:", err)
		os.Exit(1)
	}
}

func run(root, out string) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}

	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(gmhtml.WithUnsafe()),
	)

	slugs := map[string]string{}
	for _, p := range pages {
		slugs[filepath.Base(p.Source)] = p.Slug
	}

	for _, p := range pages {
		source, err := os.ReadFile(filepath.Join(root, p.Source))
		if err != nil {
			return err
		}

		var body bytes.Buffer
		if err := md.Convert(source, &body); err != nil {
			return fmt.Errorf("%s: %w", p.Source, err)
		}

		rendered := rewriteLinks(body.String(), slugs)
		name := filepath.Join(out, p.Slug+".html")
		if err := os.WriteFile(name, []byte(shell(p, rendered)), 0o644); err != nil {
			return err
		}
	}

	return os.WriteFile(filepath.Join(out, "index.html"), []byte(index()), 0o644)
}

var linkRe = regexp.MustCompile(`(href|src)="([^"]+)"`)

// rewriteLinks points cross-document links at the rendered pages, and anything
// else that lives in the repository at the repository. A doc that links to
// emit_test.go means the file, not a page that does not exist here.
func rewriteLinks(body string, slugs map[string]string) string {
	return linkRe.ReplaceAllStringFunc(body, func(m string) string {
		parts := linkRe.FindStringSubmatch(m)
		attr, target := parts[1], parts[2]

		switch {
		case strings.HasPrefix(target, "http"), strings.HasPrefix(target, "#"),
			strings.HasPrefix(target, "mailto:"):
			return m
		}

		path, anchor, _ := strings.Cut(target, "#")
		if anchor != "" {
			anchor = "#" + anchor
		}
		if slug, ok := slugs[filepath.Base(path)]; ok && strings.HasSuffix(path, ".md") {
			return fmt.Sprintf(`%s="./%s.html%s"`, attr, slug, anchor)
		}
		return fmt.Sprintf(`%s="%s%s%s"`, attr, repo, strings.TrimPrefix(path, "../"), anchor)
	})
}

func sidebar(current string) string {
	var b strings.Builder
	section := ""
	for _, p := range pages {
		if p.Section != section {
			if section != "" {
				b.WriteString("</ul>")
			}
			fmt.Fprintf(&b, `<p class="side-head">%s</p><ul>`, html.EscapeString(p.Section))
			section = p.Section
		}
		class := ""
		if p.Slug == current {
			class = ` class="here"`
		}
		fmt.Fprintf(&b, `<li><a href="./%s.html"%s>%s</a></li>`, p.Slug, class, html.EscapeString(p.Title))
	}
	b.WriteString("</ul>")
	return b.String()
}

func shell(p page, body string) string {
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>%s — Sluice</title>
    <meta name="description" content="%s" />
    <link rel="stylesheet" href="../style.css" />
    <link rel="icon" href="data:image/svg+xml,%%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'%%3E%%3Crect width='32' height='32' rx='7' fill='%%2309090b'/%%3E%%3Cpath d='M11 10v8h9' stroke='white' stroke-width='2.5' fill='none' stroke-linecap='round' stroke-linejoin='round'/%%3E%%3C/svg%%3E" />
  </head>
  <body class="docs">
    <header>
      <div class="wrap">
        <a class="brand" href="../"><span class="mark"></span>Sluice</a>
        <nav>
          <a href="../#demo">Demo</a>
          <a href="./index.html">Docs</a>
          <a href="https://github.com/kilianc/sluice">GitHub</a>
        </nav>
      </div>
    </header>

    <div class="doc-layout">
      <aside class="side">%s</aside>
      <article class="prose">%s</article>
    </div>

    <footer>
      <div class="wrap">
        <span>MIT licensed.</span>
        <nav>
          <a href="../">Home</a>
          <a href="https://github.com/kilianc/sluice">GitHub</a>
        </nav>
      </div>
    </footer>
  </body>
</html>
`, html.EscapeString(p.Title), html.EscapeString(p.Blurb), sidebar(p.Slug), body)
}

func index() string {
	var cards strings.Builder
	section := ""
	for _, p := range pages {
		if p.Section != section {
			fmt.Fprintf(&cards, `<p class="eyebrow">%s</p><div class="doclist">`, html.EscapeString(p.Section))
			if section != "" {
				// closed below by the previous iteration's writer
			}
			section = p.Section
		}
		fmt.Fprintf(&cards,
			`<a href="./%s.html"><span class="name">%s</span><span class="what">%s</span><span class="arrow">→</span></a>`,
			p.Slug, html.EscapeString(p.Title), html.EscapeString(p.Blurb))
		if last := pages[len(pages)-1]; p == last || nextSection(p) != p.Section {
			cards.WriteString(`</div>`)
		}
	}

	return fmt.Sprintf(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Documentation — Sluice</title>
    <link rel="stylesheet" href="../style.css" />
    <link rel="icon" href="data:image/svg+xml,%%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'%%3E%%3Crect width='32' height='32' rx='7' fill='%%2309090b'/%%3E%%3Cpath d='M11 10v8h9' stroke='white' stroke-width='2.5' fill='none' stroke-linecap='round' stroke-linejoin='round'/%%3E%%3C/svg%%3E" />
  </head>
  <body>
    <header>
      <div class="wrap">
        <a class="brand" href="../"><span class="mark"></span>Sluice</a>
        <nav>
          <a href="../#demo">Demo</a>
          <a href="./index.html">Docs</a>
          <a href="https://github.com/kilianc/sluice">GitHub</a>
        </nav>
      </div>
    </header>

    <section style="border-top:0">
      <div class="wrap">
        <h2 style="font-size:2rem">Documentation</h2>
        <p class="sub">
          Start with the tutorial if you have a database and twenty minutes.
          <a href="./spec.html">The specification</a> is the normative document;
          everything else is a projection of it.
        </p>
        %s
      </div>
    </section>

    <footer>
      <div class="wrap">
        <span>MIT licensed.</span>
        <nav>
          <a href="../">Home</a>
          <a href="https://github.com/kilianc/sluice">GitHub</a>
        </nav>
      </div>
    </footer>
  </body>
</html>
`, cards.String())
}

// nextSection reports the section of the page after p, or "" at the end.
func nextSection(p page) string {
	for i, c := range pages {
		if c == p && i+1 < len(pages) {
			return pages[i+1].Section
		}
	}
	return ""
}
