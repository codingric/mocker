package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/itchyny/gojq"
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

var config Config

func main() {
	err := reload()
	if err != nil {
		log.Fatal().Err(err).Msg("Error reloading config")
	}

	// Set up file watcher for mock.yaml
	go func() {
		for {
			err := watchConfigFile("mocker.yaml")
			if err != nil {
				log.Error().Err(err).Msg("Error watching config file")
			}
		}
	}()

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

func watchConfigFile(filename string) error {
	initialStat, err := os.Stat(filename)
	if err != nil {
		return err
	}

	for {
		time.Sleep(1 * time.Second)

		stat, err := os.Stat(filename)
		if err != nil {
			return err
		}

		if stat.Size() != initialStat.Size() || stat.ModTime() != initialStat.ModTime() {
			log.Info().Msg("Config file changed, reloading...")
			err := reload()
			if err != nil {
				log.Error().Err(err).Msg("Error reloading config")
			}
			initialStat = stat
		}
	}
}
func reload() error {
	// Read config file
	configFile, err := os.ReadFile("mocker.yaml")
	if err != nil {
		return err
	}

	// Parse config

	err = yaml.Unmarshal(configFile, &config)
	if err != nil {
		return err
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
	return nil
}

func paramMatch(url, route string) (bool, map[string]any) {
	url_parts := strings.Split(url, "/")
	route_parts := strings.Split(route, "/")

	params := make(map[string]any)

	if len(url_parts) != len(route_parts) {
		return false, nil
	}

	for i, part := range route_parts {
		if part == "*" {
			continue
		}

		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			key := strings.Trim(part, "{}")
			route_parts[i] = key
			params[key] = url_parts[i]
			continue
		}

		if part != url_parts[i] {
			return false, nil
		}
	}
	return true, params
}

func (r Routes) Handle(w http.ResponseWriter, req *http.Request) {
	l := log.Info().Str("url", req.URL.Path).Str("method", strings.ToLower(req.Method))

	data := make(map[string]any)
	var methods map[string][]Route

	for url, m := range config.Routes {
		if match, params := paramMatch(req.URL.Path, url); match {
			methods = m
			data["params"] = params
			break
		}
	}

	if methods == nil {
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

	data["request"] = req
	var _json any
	body, _ := io.ReadAll(req.Body)
	json.Unmarshal(body, &_json)
	data["body"] = string(body)
	data["json"] = _json
	data["url"] = req.URL.Path
	data["method"] = req.Method
	data["headers"] = func() map[string]any {
		headers := make(map[string]any)
		for k, v := range req.Header {
			headers[strings.ToLower(k)] = v[0]
		}
		return headers
	}()

	for _, route := range routes {
		matched := true
		if len(route.Conditions) > 0 {
			for _, condition := range route.Conditions {
				if result := render_jq(condition, data); !result.(bool) {
					matched = result.(bool)
					break
				}
			}
		}
		if matched {
			if route.Code == 0 {
				route.Code = 200
			}
			route.Response = render(route.Response, data)
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
					w.Header().Set(k, render(v, data))
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

func render(query string, data map[string]any) string {
	re := regexp.MustCompile(`\$\{(.*?)\}`)
	matches := re.FindAllStringSubmatch(query, -1)

	for _, match := range matches {
		fullMatch := match[0]
		jqQuery := match[1]

		results := render_jq(jqQuery, data)
		var replacement string
		replacement = fmt.Sprintf("%v", results)
		query = strings.Replace(query, fullMatch, replacement, 1)
	}

	return query
}

func render_jq(query string, data map[string]any) any {
	q, err := gojq.Parse(query)
	if err != nil {
		log.Error().Err(err).Msg("Error parsing jq query")
		return ""
	}

	log.Debug().Msg("jq query: " + query)
	iter := q.Run(data)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := v.(error); ok {
			log.Error().Err(err).Msg("Error running query")
			continue
		} else {
			log.Debug().Msgf("jq result: %#v", v)
			return v
		}
	}
	return nil
}
