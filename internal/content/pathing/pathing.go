package pathing

import (
	"fmt"
	"regexp"
	"strings"
)

var rfc3339DatePrefix = regexp.MustCompile(`^(?P<datetime>(\d{4})-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])(T([01][0-9]|2[0-3]):([0-5][0-9]):([0-5][0-9]|60)(\.[0-9]+)?(Z|(\+|-)([01][0-9]|2[0-3]):([0-5][0-9])))?)(\s?(_|-)(?P<slug>.+$))?`)

func slugifyPath(input string) string {
	s := strings.TrimSpace(strings.ToLower(input))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")

	var b strings.Builder
	b.Grow(len(s))
	lastDash := false
	for _, r := range s {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if r == '-' {
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "index"
	}
	return out
}

func ExtractDateAndSlugFromFilename(input string) (date string, slug string, ok bool) {
	matches := rfc3339DatePrefix.FindStringSubmatch(input)
	if matches == nil {
		return "", "", false
	}
	dateIdx := rfc3339DatePrefix.SubexpIndex("datetime")
	if dateIdx > 0 && dateIdx < len(matches) {
		date = matches[dateIdx]
	}
	slugIdx := rfc3339DatePrefix.SubexpIndex("slug")
	if slugIdx > 0 && slugIdx < len(matches) {
		slug = matches[slugIdx]
	}
	return date, slug, true
}

func ComputePageSlug(metaSlug string, filePathForSlug string, pathsKeepDates bool) (slug string, extractedDate string) {
	if strings.TrimSpace(metaSlug) != "" {
		return slugifyPath(metaSlug), ""
	}

	if dt, datedSlug, ok := ExtractDateAndSlugFromFilename(filePathForSlug); ok {
		extractedDate = dt
		if datedSlug != "" && !pathsKeepDates {
			return slugifyPath(datedSlug), extractedDate
		}
	}

	return slugifyPath(filePathForSlug), extractedDate
}

func ComputePagePath(metaPath string, slug string, components []string, fileName string, hasColocatedPath bool, lang string, defaultLang string) string {
	if strings.TrimSpace(metaPath) != "" {
		path := strings.TrimSpace(metaPath)
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		if !strings.HasSuffix(path, "/") {
			path += "/"
		}
		return path
	}

	var p string
	if len(components) == 0 {
		if fileName == "index" && !hasColocatedPath {
			p = ""
		} else {
			p = slug
		}
	} else {
		p = fmt.Sprintf("%s/%s", strings.Join(components, "/"), slug)
	}

	if lang != "" && defaultLang != "" && lang != defaultLang {
		if p == "" {
			p = lang
		} else {
			p = fmt.Sprintf("%s/%s", lang, p)
		}
	}

	path := "/" + p
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return path
}

func MakePermalink(baseURL string, path string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if base == "/" {
		return path
	}
	return base + path
}
