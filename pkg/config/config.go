package config

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	kitlog "github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

var (
	// DefaultConfig is the default top-level configuration.
	DefaultConfig = Config{
		Server: DefaultServerConfig,
	}

	// DefaultServerConfig is the default global configuration.
	DefaultServerConfig = ServerConfig{
		Port: "8080",
		// URI:            "/federate",
		// URIPrefix:      "",
		API:            true,
		Federate:       true,
		Proxy:          "http://localhost:9090/",
		Authentication: true,
		HeaderName:     "X-Prom-Liver-Id",
		AdminAPI:       true,
		AdminPort:      "8888",
	}
)

// Config includes all config.yaml
type Config struct {
	Server       ServerConfig `yaml:"server,omitempty"`
	ClientsFiles []string     `yaml:"clients_files,omitempty"`
	Clients      Clients      `yaml:"clients,omitempty"`
}

// ServerConfig includes only "server:" three
type ServerConfig struct {
	Port       string `yaml:"port,omitempty"`
	Proxy      string `yaml:"proxy,omitempty"`
	HeaderName string `yaml:"id-header,omitempty"`
	// URI            string `yaml:"uri,omitempty"`
	// URIPrefix      string `yaml:"uri-prefix,omitempty"`
	API            bool `yaml:"api-enable,omitempty"`
	Federate       bool `yaml:"federate-enable,omitempty"`
	Authentication bool `yaml:"authentication,omitempty"`

	AdminAPI  bool   `yaml:"admin-api-enable,omitempty"`
	AdminPort string `yaml:"admin-port,omitempty"`
}

//Clients includes configuration for each client
type Clients map[ClientID]ClientConfig

//ClientID is client id
type ClientID string

//ClientConfig client configuration
type ClientConfig struct {
	// ID    string     `yaml:"id"`
	Auth  AuthSchema `yaml:"auth,omitempty"`
	Match []string   `yaml:"match"`
}

// AuthSchema describe all available auth schemes
type AuthSchema struct {
	Header bool             `yaml:"header,omitempty"` //header 'X-Prom-Liver-Id' value
	Basic  AuthSchemaBasic  `yaml:"basic,omitempty"`
	Bearer AuthSchemaBearer `yaml:"bearer,omitempty"`
}

// AuthSchemaBasic basic yaml
type AuthSchemaBasic struct {
	User     string   `yaml:"user,omitempty"`
	Password string   `yaml:"password,omitempty"`
	Base64   []string `yaml:"base64,omitempty"`
	Files    []string `yaml:"files,omitempty"`
}

// AuthSchemaBearer bearer yaml
type AuthSchemaBearer struct {
	Tokens []string `yaml:"tokens,omitempty"`
	Files  []string `yaml:"files,omitempty"`
}

type ConfigManager struct {
	configFile string
	logger     kitlog.Logger
}

// New creates ConfigManager
func New(cf string, l *kitlog.Logger) (*ConfigManager, error) {
	if l == nil {
		return nil, fmt.Errorf("logger is nil")
	}
	if cf == "" {
		return nil, fmt.Errorf("filename is empty")
	}
	cm := &ConfigManager{
		configFile: cf,
		logger:     *l,
	}
	return cm, nil
}

// LoadConfig returns Config after it reads and parse all config files
func (cm *ConfigManager) LoadConfig() (Config, error) {
	newCfg, err := cm.loadConfigFile()
	if err != nil {
		return newCfg, errors.Wrapf(err, "cannot load config file ")
	}

	if len(newCfg.ClientsFiles) > 0 {
		newCfg, err = cm.loadClientsConfigFiles(newCfg)
		if err != nil {
			return newCfg, errors.Wrapf(err, "cannot load clients config files ")
		}
	}

	if len(newCfg.Clients) == 0 {
		return newCfg, fmt.Errorf("the set of clients is empty. Are you sure?")
	}

	newCfg, err = cm.loadCredsFiles(newCfg)
	if err != nil {
		return newCfg, errors.Wrapf(err, "cannot load credentials files ")
	}

	return newCfg, nil
}

func (cm *ConfigManager) loadConfigFile() (Config, error) {
	newCfg := DefaultConfig
	file, err := ioutil.ReadFile(cm.configFile)
	if err != nil {
		return newCfg, errors.Wrapf(err, "cannot read config file")
	}
	err = yaml.UnmarshalStrict(file, &newCfg)
	if err != nil {
		return newCfg, errors.Wrapf(err, "cannot parse config file")
	}

	return newCfg, nil
}

func findFiles(patFiles []string) (files []string, err error) {
	for _, p := range patFiles {
		fs, err := filepath.Glob(p)
		if err != nil {
			return nil, err
		}
		files = append(files, fs...)
	}
	return
}

func (cm *ConfigManager) loadClientsConfigFiles(newCfg Config) (Config, error) {
	level.Debug(cm.logger).Log("msg", "try to find clients config files", "patterns", fmt.Sprint(newCfg.ClientsFiles))
	files, err := findFiles(newCfg.ClientsFiles)
	if err != nil {
		return newCfg, errors.Wrapf(err, "error finding clients config files")
	}
	if len(files) == 0 {
		return newCfg, nil
	}

	level.Debug(cm.logger).Log("msg", "found client config files", "files", fmt.Sprint(files))
	for _, f := range files {
		level.Debug(cm.logger).Log("msg", "read file", "file", f)
		file, err := ioutil.ReadFile(f)
		if err != nil {
			return newCfg, errors.Wrapf(err, "cannot read client config file %v", f)
		}
		clients := make(Clients)
		err = yaml.UnmarshalStrict(file, &clients)
		if err != nil {
			return newCfg, errors.Wrapf(err, "cannot parse client config file %v", f)
		}
		for id, conf := range clients {
			if _, ok := newCfg.Clients[id]; ok {
				return newCfg, fmt.Errorf("duplicate client ID from files: ID=%v, file=%v", id, f)
			}
			newCfg.Clients[id] = conf
		}
	}

	return newCfg, nil
}

func (cm *ConfigManager) loadCredsFiles(newCfg Config) (Config, error) {
	for id, clientConfig := range newCfg.Clients {
		// read base64 files and copy to []Base64
		if len(clientConfig.Auth.Basic.Files) > 0 {
			base64, err := cm.readCredsFiles(clientConfig.Auth.Basic.Files)
			if err != nil {
				return newCfg, err
			}
			clientConfig.Auth.Basic.Base64 = append(clientConfig.Auth.Basic.Base64, base64...)
		}
		// read tokens files and copy to []Tokens
		if len(clientConfig.Auth.Bearer.Files) > 0 {
			tokens, err := cm.readCredsFiles(clientConfig.Auth.Bearer.Files)
			if err != nil {
				return newCfg, err
			}
			clientConfig.Auth.Bearer.Tokens = append(clientConfig.Auth.Bearer.Tokens, tokens...)
		}
		newCfg.Clients[id] = clientConfig
	}
	return newCfg, nil
}

func (cm *ConfigManager) readCredsFiles(patFiles []string) ([]string, error) {
	filesContent := make([]string, 0)
	files, err := findFiles(patFiles)
	if err != nil {
		return nil, errors.Wrapf(err, "error finding creds files")
	}
	level.Debug(cm.logger).Log("msg", "found base64 credentials files", "files", fmt.Sprint(files))
	for _, f := range files {
		content, err := ioutil.ReadFile(f)
		if err != nil {
			return nil, errors.Wrapf(err, "cannot read file")
		}
		filesContent = append(filesContent, string(content))
	}

	return filesContent, nil
}