package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"

	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v2"
)

type Route struct {
	Conditions []string          `yaml:"conditions"`
	Response   string            `yaml:"response"`
	Code       int               `yaml:"code"`
	Name       string            `yaml:"name"`
	Headers    map[string]string `yaml:"headers"`
}

type Routes map[string]map[string][]Route

type Config struct {
	Routes Routes `yaml:"routes"`
	Port   string `yaml:"port"`
}

func main() {
	// Read config file
	configFile, err := os.ReadFile("mocker.yaml")
	if err != nil {
		log.Fatal().Err(err).Msg("Error reading config file")
	}

	// Parse config
	var config Config
	err = yaml.Unmarshal(configFile, &config)
	if err != nil {
		log.Fatal().Err(err).Msg("Error parsing config file")
	}

	for url, methods := range config.Routes {
		for method, routes := range methods {
			for i, route := range routes {
				if route.Name == "" {
					route.Name = fmt.Sprintf("route #%d", i+1)
				}
				b, _ := json.Marshal(route)
				log.Debug().Str("url", url).Str("method", method).RawJSON("route", b).Msgf("%s loaded", route.Name)
			}
		}
	}

	// Set up HTTP server
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		config.Routes.Handle(w, r)
	})

	port := ":" + config.Port
	if port == ":" {
		port = ":8080"
	}

	// Start server
	log.Info().Msgf("Server starting on %s", port)
	err = http.ListenAndServe(port, nil)
	log.Fatal().Err(err).Msg("Server error")
}

func (r Routes) Handle(w http.ResponseWriter, req *http.Request) {
	l := log.Info().Str("url", req.URL.Path).Str("method", strings.ToLower(req.Method))

	methods, ok := r[req.URL.Path]
	if !ok {
		l.Msg("URL not matched")
		http.NotFound(w, req)
		return
	}

	routes, ok := methods[strings.ToLower(req.Method)]
	if !ok {
		l.Msg("method not matched")
		http.NotFound(w, req)
		return
	}

	for _, route := range routes {
		condition := true
		if len(route.Conditions) > 0 {
			tmp := fmt.Sprintf(`{{if and (%s)}}true{{else}}false{{end}}`, strings.Join(route.Conditions, ") ("))
			condition = renderTemplate(tmp, req) == "true"
		}
		if condition {
			if route.Code == 0 {
				route.Code = 200
			}
			route.Response = renderTemplate(route.Response, req)
			body := route.Response
			if len(route.Response) > 20 {
				body = route.Response[:20] + "..."
			}

			if len(route.Conditions) > 0 {
				l.Interface("conditions", route.Conditions)
			}
			if len(route.Headers) > 0 {
				l.Interface("headers", route.Headers)
				for k, v := range route.Headers {
					w.Header().Set(k, v)
					log.Debug().Str("header", k).Str("value", v).Msg("Header set")
				}
			}
			w.WriteHeader(route.Code)
			w.Write([]byte(route.Response))
			l.Int("status_code", route.Code).Str("response", body).Msg("Route matched")
			return
		}
	}
	l.Msg("conditions not matched")
	http.NotFound(w, req)
}

func renderTemplate(tmpl string, req *http.Request) string {
	data := map[string]any{"Request": req}

	t := template.New("hello")
	t.Funcs(template.FuncMap{
		"toLower": func(target string) string {
			return strings.ToLower(target)
		},
		"method": func(method string) bool {
			return strings.EqualFold(req.Method, method)
		},
		"has_header": func(header string) bool {
			_, ok := req.Header[header]
			return ok
		},
		"header_eq": func(header, value string) bool {

			h, ok := req.Header[header]
			if !ok {
				return false
			}
			for _, v := range h {
				if strings.EqualFold(v, value) {
					return true
				}
			}
			return false
		},
		"and": func(tests ...bool) bool {
			for _, test := range tests {
				if !test {
					return false
				}
			}
			return true
		},
	})
	tt, err := t.Parse(tmpl)
	if err != nil {
		panic(err)
	}

	var buf strings.Builder
	err = tt.Execute(&buf, data)
	if err != nil {
		panic(err)
	}
	return buf.String()
}
