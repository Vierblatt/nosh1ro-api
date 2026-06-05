package main

import (
	"bytes"
	"regexp"

	chromaHTML "github.com/alecthomas/chroma/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

var md goldmark.Markdown

func init() {
	md = goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			highlighting.NewHighlighting(
				highlighting.WithFormatOptions(
					chromaHTML.WithLineNumbers(true),
				),
			),
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
		),
	)
}

func renderMarkdown(content string) string {
	var buf bytes.Buffer
	if err := md.Convert([]byte(content), &buf); err != nil {
		return content
	}
	return buf.String()
}

var stripHTML = regexp.MustCompile(`<[^>]*>`)

func extractSummary(html string, maxLen int) string {
	plain := stripHTML.ReplaceAllString(html, "")
	runes := []rune(plain)
	if len(runes) <= maxLen {
		return plain
	}
	return string(runes[:maxLen]) + "..."
}
