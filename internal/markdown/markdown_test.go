package markdown

import (
	"strings"
	"testing"
)

func TestRenderMarkdown_Basic(t *testing.T) {
	out := Render("# Hello")
	if !strings.Contains(out, "<h1") || !strings.Contains(out, "Hello") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestRenderMarkdown_CodeBlock(t *testing.T) {
	out := Render("```go\nfunc main() {}\n```")
	if !strings.Contains(out, "<code") {
		t.Errorf("expected code block: %s", out)
	}
}

func TestRenderMarkdown_GFM_Table(t *testing.T) {
	out := Render("| a | b |\n|---|---|\n| 1 | 2 |")
	if !strings.Contains(out, "<table") {
		t.Errorf("expected table: %s", out)
	}
}

func TestRenderMarkdown_Empty(t *testing.T) {
	out := Render("")
	_ = out
}

func TestExtractSummary(t *testing.T) {
	html := "<p>" + strings.Repeat("x", 300) + "</p>"
	summary := ExtractSummary(html, 100)
	if len([]rune(summary)) != 103 {
		t.Errorf("summary len = %d, want 103", len([]rune(summary)))
	}
	if !strings.HasSuffix(summary, "...") {
		t.Error("expected trailing ellipsis")
	}
}

func TestExtractSummary_Short(t *testing.T) {
	short := "short text"
	summary := ExtractSummary(short, 200)
	if summary != short {
		t.Errorf("short summary should be unchanged: got %q", summary)
	}
}

func TestExtractSummary_StripHTML(t *testing.T) {
	summary := ExtractSummary("<p><strong>bold</strong> text</p>", 100)
	if strings.Contains(summary, "<") {
		t.Errorf("summary should not contain HTML: %s", summary)
	}
}
