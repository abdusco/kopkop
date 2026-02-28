package templates

func BuiltinTemplates() map[string]string {
	return map[string]string{
		"__zola_builtins/404.html":    "<!doctype html>\n<title>404 Not Found</title>\n<h1>404 Not Found</h1>",
		"__zola_builtins/sitemap.xml": `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">{% for p in pages %}<url><loc>{{ p }}</loc></url>{% endfor %}</urlset>`,
		"__zola_builtins/rss.xml":     `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>{{ config.title }}</title>{% for p in pages %}<item><title>{{ p.title }}</title><link>{{ p.permalink }}</link></item>{% endfor %}</channel></rss>`,
		"__zola_builtins/atom.xml":    `<?xml version="1.0" encoding="UTF-8"?><feed xmlns="http://www.w3.org/2005/Atom"><title>{{ config.title }}</title>{% for p in pages %}<entry><title>{{ p.title }}</title><id>{{ p.permalink }}</id><link href="{{ p.permalink }}"/></entry>{% endfor %}</feed>`,
		"__zola_builtins/robots.txt": `User-agent: *
Disallow:
Allow: /
Sitemap: {{ config.base_url }}/sitemap.xml
`,
		"__zola_builtins/internal/alias.html": `<!doctype html>
<meta charset="utf-8">
<title>Redirect</title>
<script>
  const target = "{{ url | safe }}";
  const hash = window.location.hash || "";
  window.location.replace(target + hash);
</script>
<noscript>
  <meta http-equiv="refresh" content="0; url={{ url | safe }}">
</noscript>
<p><a href="{{ url | safe }}">Click here</a> to be redirected.</p>`,
	}
}
