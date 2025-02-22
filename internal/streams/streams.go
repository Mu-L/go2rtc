package streams

import (
	"net/http"
	"net/url"
	"regexp"
	"sync"

	"github.com/AlexxIT/go2rtc/internal/api"
	"github.com/AlexxIT/go2rtc/internal/app"
	"github.com/AlexxIT/go2rtc/internal/app/store"
	"github.com/rs/zerolog"
)

func Init() {
	var cfg struct {
		Mod map[string]any `yaml:"streams"`
	}

	app.LoadConfig(&cfg)

	log = app.GetLogger("streams")

	for name, item := range cfg.Mod {
		streams[name] = NewStream(item)
	}

	for name, item := range store.GetDict("streams") {
		streams[name] = NewStream(item)
	}

	api.HandleFunc("api/streams", streamsHandler)
}

func Get(name string) *Stream {
	return streams[name]
}

var sanitize = regexp.MustCompile(`\S`)

func New(name string, source string) *Stream {
	// not allow creating dynamic streams with spaces in the source
	if sanitize.MatchString(source) {
		return nil
	}

	stream := NewStream(source)
	streams[name] = stream
	return stream
}

func Patch(name string, source string) *Stream {
	streamsMu.Lock()
	defer streamsMu.Unlock()

	// check if source links to some stream name from go2rtc
	if u, err := url.Parse(source); err == nil && u.Scheme == "rtsp" && len(u.Path) > 1 {
		rtspName := u.Path[1:]
		if stream, ok := streams[rtspName]; ok {
			// link (alias) stream[name] to stream[rtspName]
			streams[name] = stream
			return stream
		}
	}

	// check if src has supported scheme
	if !HasProducer(source) {
		return nil
	}

	// check an existing stream with this name
	if stream, ok := streams[name]; ok {
		stream.SetSource(source)
		return stream
	}

	// create new stream with this name
	return New(name, source)
}

func GetOrPatch(query url.Values) *Stream {
	// check if src param exists
	source := query.Get("src")
	if source == "" {
		return nil
	}

	// check if src is stream name
	if stream, ok := streams[source]; ok {
		return stream
	}

	// check if name param provided
	if name := query.Get("name"); name == "" {
		log.Info().Msgf("[streams] create new stream url=%s", source)

		return Patch(name, source)
	}

	// return new stream with src as name
	return Patch(source, source)
}

func GetAll() (names []string) {
	for name := range streams {
		names = append(names, name)
	}
	return
}

func streamsHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	src := query.Get("src")

	// without source - return all streams list
	if src == "" && r.Method != "POST" {
		api.ResponseJSON(w, streams)
		return
	}

	// Not sure about all this API. Should be rewrited...
	switch r.Method {
	case "GET":
		api.ResponsePrettyJSON(w, streams[src])

	case "PUT":
		name := query.Get("name")
		if name == "" {
			name = src
		}

		if New(name, src) == nil {
			http.Error(w, "", http.StatusBadRequest)
		}

	case "PATCH":
		name := query.Get("name")
		if name == "" {
			http.Error(w, "", http.StatusBadRequest)
			return
		}

		// support {input} templates: https://github.com/AlexxIT/go2rtc#module-hass
		if Patch(name, src) == nil {
			http.Error(w, "", http.StatusBadRequest)
		}

	case "POST":
		// with dst - redirect source to dst
		if dst := query.Get("dst"); dst != "" {
			if stream := Get(dst); stream != nil {
				if err := stream.Play(src); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				} else {
					api.ResponseJSON(w, stream)
				}
			} else {
				http.Error(w, "", http.StatusNotFound)
			}
		} else {
			http.Error(w, "", http.StatusBadRequest)
		}

	case "DELETE":
		delete(streams, src)
	}
}

var log zerolog.Logger
var streams = map[string]*Stream{}
var streamsMu sync.Mutex
