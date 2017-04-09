package main

import (
	"bytes"
	"errors"
	"flag"
	"html/template"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	log "github.com/inconshreveable/log15"
	"github.com/kevinburke/go-html-boilerplate/assets"
	"github.com/kevinburke/handlers"
	"github.com/kevinburke/rest"
	yaml "gopkg.in/yaml.v2"
)

// DefaultPort is the listening port if no other port is specified.
const DefaultPort = 7065

var errWrongLength = errors.New("Secret key has wrong length. Should be a 64-byte hex string")
var homepageTpl *template.Template
var cfg = flag.String("config", "config.yml", "Path to a config file")
var logger log.Logger

func init() {
	homepageHTML := assets.MustAssetString("templates/index.html")
	homepageTpl = template.Must(template.New("homepage").Parse(homepageHTML))
	logger = handlers.Logger

	// Add more templates here.
}

// A HTTP server for static files. All assets are packaged up in the assets
// directory with the go-bindata binary. Run "make assets" to rerun the
// go-bindata binary.
type static struct {
	modTime time.Time
}

func (s *static) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/favicon.ico" {
		r.URL.Path = "/static/favicon.ico"
	}
	bits, err := assets.Asset(strings.TrimPrefix(r.URL.Path, "/"))
	if err != nil {
		rest.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, r.URL.Path, s.modTime, bytes.NewReader(bits))
}

func render(w http.ResponseWriter, tpl *template.Template, name string, data interface{}) {
	buf := new(bytes.Buffer)
	if err := tpl.ExecuteTemplate(buf, name, data); err != nil {
		http.Error(w, err.Error(), 500)
	}
	w.Write(buf.Bytes())
}

func NewServeMux() http.Handler {
	staticServer := &static{
		modTime: time.Now().UTC(),
	}

	r := new(handlers.Regexp)
	r.Handle(regexp.MustCompile(`(^/static|^/favicon.ico$)`), []string{"GET"}, handlers.GZip(staticServer))
	r.HandleFunc(regexp.MustCompile(`^/$`), []string{"GET"}, func(w http.ResponseWriter, r *http.Request) {
		push(w, "/static/style.css", "style")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		render(w, homepageTpl, "homepage", nil)
	})
	// Add more routes here.
	return r
}

// FileConfig represents the data in a config file.
type FileConfig struct {
	// SecretKey is used to encrypt sessions and other data before serving it to
	// the client. It should be a hex string that's exactly 64 bytes long. For
	// example:
	//
	//   d7211b215341871968869dontusethisc0ff1789fc88e0ac6e296ba36703edf8
	//
	// That key is invalid - you can generate a random key by running:
	//
	//   openssl rand -hex 32
	//
	// If no secret key is present, we'll generate one when the server starts.
	// However, this means that sessions may error when the server restarts.
	//
	// If a server key is present, but invalid, the server will not start.
	SecretKey string `yaml:"secret_key"`

	// Port to listen on. Set to 0 to choose a port at random. If unspecified,
	// defaults to 7065.
	Port *int `yaml:"port"`

	// For TLS configuration.
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`

	// Add other configuration settings here.
}

func main() {
	flag.Parse()
	data, err := ioutil.ReadFile(*cfg)
	c := new(FileConfig)
	if err == nil {
		if err := yaml.Unmarshal(data, c); err != nil {
			logger.Error("Couldn't parse config file", "err", err)
			os.Exit(2)
		}
	} else {
		logger.Error("Couldn't find config file", "err", err)
		os.Exit(2)
	}
	key, err := getSecretKey(c.SecretKey)
	if err != nil {
		logger.Error("Error getting secret key", "err", err)
		os.Exit(2)
	}
	// You can use the secret key with secretbox
	// (godoc.org/golang.org/x/crypto/nacl/secretbox/) to generate cookies and
	// secrets. See flash.go and crypto.go for examples.
	_ = key

	mux := NewServeMux()
	if c.Port == nil {
		port, ok := os.LookupEnv("PORT")
		if ok {
			*c.Port, err = strconv.Atoi(port)
			if err != nil {
				logger.Error("Invalid port", "err", err, "port", port)
				os.Exit(2)
			}
		} else {
			*c.Port = DefaultPort
		}
	}
	if c.CertFile == "" {
		c.CertFile = "cert.pem"
	}
	if _, err := os.Stat(c.CertFile); os.IsNotExist(err) {
		logger.Error("Could not find a cert file; generate using 'make generate_cert'", "file", c.CertFile)
		os.Exit(2)
	}
	if c.KeyFile == "" {
		c.KeyFile = "key.pem"
	}
	if _, err := os.Stat(c.KeyFile); os.IsNotExist(err) {
		logger.Error("Could not find a key file; generate using 'make generate_cert'", "file", c.KeyFile)
		os.Exit(2)
	}
	mux = handlers.UUID(mux)
	mux = handlers.Server(mux, "go-html-boilerplate")
	mux = handlers.Log(mux)
	mux = handlers.Duration(mux)
	logger.Info("Starting server", "port", *c.Port)
	listenErr := http.ListenAndServeTLS("127.0.0.1:"+strconv.Itoa(*c.Port), c.CertFile, c.KeyFile, mux)
	logger.Error("server shut down", "err", listenErr)
}
