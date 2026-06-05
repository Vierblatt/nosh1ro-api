package main

import (
	"encoding/xml"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type rssFeed struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	Atom    string   `xml:"xmlns:atom,attr"`
	Channel rssChannel
}

type rssChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Language    string    `xml:"language"`
	AtomLink    atomLink  `xml:"atom:link"`
	Items       []rssItem `xml:"item"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
	GUID        string `xml:"guid"`
}

func handleFeed(store *Store, cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		bs, err := store.getSettings(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get settings"})
			return
		}

		blogTitle := bs.Title
		if blogTitle == "" {
			blogTitle = cfg.BlogTitle
		}
		blogDesc := bs.Subtitle

		result, err := store.findPosts(c.Request.Context(), PostFilter{Status: "published"}, 1, 20)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch posts"})
			return
		}

		feedURL := "https://nosh1ro.top/api/feed.xml"
		items := make([]rssItem, 0, len(result.Posts))
		for _, p := range result.Posts {
			if p.Encrypted {
				continue
			}
			desc := p.Summary
			if desc == "" {
				desc = extractSummary(p.ContentHTML, 300)
			}
			pubDate, _ := time.Parse("2006-01-02", p.Date)
			items = append(items, rssItem{
				Title:       p.Title,
				Link:        "https://nosh1ro.top/posts/" + p.ID,
				Description: desc,
				PubDate:     pubDate.Format(time.RFC1123Z),
				GUID:        "https://nosh1ro.top/posts/" + p.ID,
			})
		}

		feed := rssFeed{
			Version: "2.0",
			Atom:    "http://www.w3.org/2005/Atom",
			Channel: rssChannel{
				Title:       blogTitle,
				Link:        "https://nosh1ro.top",
				Description: blogDesc,
				Language:    "zh-CN",
				AtomLink:    atomLink{Href: feedURL, Rel: "self", Type: "application/rss+xml"},
				Items:       items,
			},
		}

		c.Header("Content-Type", "application/rss+xml; charset=utf-8")
		c.String(http.StatusOK, xml.Header+"\n"+mustMarshalXML(feed))
	}
}

func mustMarshalXML(v interface{}) string {
	b, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}
