package content

import "time"

type PageFrontMatter struct {
	Title       string              `toml:"title" yaml:"title"`
	Description string              `toml:"description" yaml:"description"`
	Slug        string              `toml:"slug" yaml:"slug"`
	Path        string              `toml:"path" yaml:"path"`
	Date        any                 `toml:"date" yaml:"date"`
	Weight      int                 `toml:"weight" yaml:"weight"`
	Template    string              `toml:"template" yaml:"template"`
	Aliases     []string            `toml:"aliases" yaml:"aliases"`
	Taxonomies  map[string][]string `toml:"taxonomies" yaml:"taxonomies"`
	Draft       bool                `toml:"draft" yaml:"draft"`
	Render      *bool               `toml:"render" yaml:"render"`
}

type SectionFrontMatter struct {
	Title            string   `toml:"title" yaml:"title"`
	Description      string   `toml:"description" yaml:"description"`
	Template         string   `toml:"template" yaml:"template"`
	PageTemplate     string   `toml:"page_template" yaml:"page_template"`
	Aliases          []string `toml:"aliases" yaml:"aliases"`
	SortBy           string   `toml:"sort_by" yaml:"sort_by"`
	PaginateBy       int      `toml:"paginate_by" yaml:"paginate_by"`
	PaginatePath     string   `toml:"paginate_path" yaml:"paginate_path"`
	PaginateReversed bool     `toml:"paginate_reversed" yaml:"paginate_reversed"`
	GenerateFeed     bool     `toml:"generate_feed" yaml:"generate_feed"`
	GenerateFeeds    bool     `toml:"generate_feeds" yaml:"generate_feeds"`
	Transparent      bool     `toml:"transparent" yaml:"transparent"`
	Render           *bool    `toml:"render" yaml:"render"`
	Draft            bool     `toml:"draft" yaml:"draft"`
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
	Translations  []string
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
	Subsections  []string
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
