package labels

import (
	"bytes"
	"strings"
	"testing"
)

// TestLabelsPageEmbedsShowNameWithoutDoubleQuoting guards against a regression
// where the show name was double-quoted in the page's JavaScript (printf "%q"
// plus html/template's own JS-string escaping), which made the labels page
// operate on a differently-named sidecar than the player and pass a quoted path
// to ffmpeg.
func TestLabelsPageEmbedsShowNameWithoutDoubleQuoting(t *testing.T) {
	var buf bytes.Buffer
	if err := labelsPageTemplate.Execute(&buf, struct{ Show string }{Show: "quartet_finals"}); err != nil {
		t.Fatalf("execute labels page template: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, `const show = "quartet_finals";`) {
		t.Fatalf("labels page should embed the show as a plain JS string `const show = \"quartet_finals\";`")
	}
	if strings.Contains(out, `\"quartet_finals\"`) {
		t.Fatal("show name is double-quoted in the rendered page (regression)")
	}
}
