package web

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"strings"
)

//go:embed static/*
var staticFS embed.FS

// Templates handles template parsing and rendering.
type Templates struct {
	t *template.Template
}

func newTemplates() (*Templates, error) {
	t := template.New("").Funcs(template.FuncMap{
		"last5": func(s string) string {
			if len(s) < 5 {
				return s
			}
			return s[len(s)-5:]
		},
		"contains": strings.Contains,
		"add": func(a, b int) int {
			return a + b
		},
	})

	// Parse all HTML files in static/ and static/tabs/
	// We need to be careful with the patterns to include subdirectories.
	// template.ParseFS doesn't support recursive globbing like **/*.html.
	
	// List of files to parse
	files := []string{
		"static/base.html",
		"static/footer.html",
		"static/index.html",
		"static/tabs/debug.html",
		"static/tabs/filaments.html",
		"static/tabs/gcode.html",
		"static/tabs/history.html",
		"static/tabs/home.html",
		"static/tabs/instructions.html",
		"static/tabs/setup.html",
		"static/tabs/timelapse.html",
	}

	for _, f := range files {
		// Use the base name as the template name for sub-templates
		name := f
		if strings.HasPrefix(f, "static/tabs/") {
			name = strings.TrimPrefix(f, "static/")
		} else {
			name = strings.TrimPrefix(f, "static/")
		}

		b, err := staticFS.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("read template %s: %w", f, err)
		}

		_, err = t.New(name).Parse(string(b))
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", f, err)
		}
	}

	return &Templates{t: t}, nil
}

func (t *Templates) Render(w io.Writer, name string, data any) error {
	return t.t.ExecuteTemplate(w, name, data)
}
