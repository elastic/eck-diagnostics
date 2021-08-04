{{- define "depInfo" -}}
{{- range $i, $dep := . }}
{{ "-" | line }}
Module  : {{ $dep.Name }}
Version : {{ $dep.Version }}
Time    : {{ $dep.VersionTime }}
Licence : {{ $dep.LicenceType }}

{{ $dep | licenceText }}
{{ end }}
{{- end -}}

Copyright 2021-{{ currentYear }} Elasticsearch BV

This product includes software developed by The Apache Software
Foundation (http://www.apache.org/).

{{ "=" | line }}
Third party libraries used by the Elastic Cloud on Kubernetes diagnostics tool
{{ "=" | line }}

{{ template "depInfo" .Direct }}

{{ if .Indirect }}
{{ "=" | line }}
Indirect dependencies

{{ template "depInfo" .Indirect }}
{{ end }}
