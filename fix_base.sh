cat << 'INNER_EOF' | patch internal/web/static/base.html
--- internal/web/static/base.html
+++ internal/web/static/base.html
@@ -9,7 +9,7 @@
     <link rel="stylesheet" href="/static/vendor/bootstrap-5.3.0-alpha3-dist/css/bootstrap.min.css" />
     <link rel="stylesheet" href="/static/vendor/bootstrap-icons-1.10.5/font/bootstrap-icons.min.css" />
     <link rel="stylesheet" href="/static/ankersrv.css" />
-    <script>var initialAccentColor = "{{if .AccentColor}}{{.AccentColor}}{{else}}#88f387{{end}}";</script>
+    <script>var initialAccentColor = "{{if .AccentColor}}{{.AccentColor}}{{else}}#88f387{{end}}";</script>
     {{block "head" .}}{{end}}
 </head>
INNER_EOF
