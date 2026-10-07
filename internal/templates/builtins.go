package templates

func BuiltinTemplates() map[string]string {
	return map[string]string{
		"__zola_builtins/taxonomy_list.html":   "\n{% for term in terms %}    {{ term.name }}  {{ term.slug }} {{ term.count }}\n{% endfor %}\n",
		"__zola_builtins/taxonomy_single.html": "Category: {{ term.name }}\n\n\n{% for page in term.pages %}    <article>\n        <h3 class=\"post__title\"><a href=\"{{ page.permalink }}\">{{ page.title }}</a></h3>\n    </article>\n{% endfor %}\n",
		"__zola_builtins/404.html":             "<!doctype html>\n<title>404 Not Found</title>\n<h1>404 Not Found</h1>",
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
