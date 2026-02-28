package content

import "time"

type PageFrontMatter struct {
	Title       string              `toml:"title" yaml:"title"`
	Description string              `toml:"description" yaml:"description"`
	Slug        string              `toml:"slug" yaml:"slug"`
	Path        string              `toml:"path" yaml:"path"`
	Date        any                 `toml:"date" yaml:"date"`
	Template    string              `toml:"template" yaml:"template"`
	Aliases     []string            `toml:"aliases" yaml:"aliases"`
	Taxonomies  map[string][]string `toml:"taxonomies" yaml:"taxonomies"`
	Draft       bool                `toml:"draft" yaml:"draft"`
}

type SectionFrontMatter struct {
	Title       string   `toml:"title" yaml:"title"`
	Description string   `toml:"description" yaml:"description"`
	Template    string   `toml:"template" yaml:"template"`
	Aliases     []string `toml:"aliases" yaml:"aliases"`
	SortBy      string   `toml:"sort_by" yaml:"sort_by"`
}

type Page struct {
	SourcePath    string
	RelativePath  string
	Lang          string
	Meta          PageFrontMatter
	RawContent    string
	Content       string
	Summary       *string
	Slug          string
	Path          string
	Permalink     string
	Components    []string
	Assets        []string
	TOC           []Heading
	InternalLinks []InternalLink
	ExternalLinks []string
	Date          *time.Time
	ParentSection string
}

type Section struct {
	SourcePath   string
	RelativePath string
	Lang         string
	Meta         SectionFrontMatter
	RawContent   string
	Content      string
	Path         string
	Permalink    string
	Components   []string
	Pages        []string
}

type Heading struct {
	ID    string
	Level int
	Title string
}

type InternalLink struct {
	Path   string
	Anchor *string
}

type TaxonomyTerm struct {
	Name  string
	Pages []string
}

type Taxonomy struct {
	Name  string
	Terms map[string]*TaxonomyTerm
}

type Library struct {
	Pages      map[string]*Page
	Sections   map[string]*Section
	Taxonomies map[string]*Taxonomy
	Permalinks map[string]string
}

func NewLibrary() *Library {
	return &Library{
		Pages:      map[string]*Page{},
		Sections:   map[string]*Section{},
		Taxonomies: map[string]*Taxonomy{},
		Permalinks: map[string]string{},
	}
}
