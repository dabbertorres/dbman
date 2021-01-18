package dbman

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

var DefaultConfigFile = func() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal("could not identify user home directory:", err)
	}
	return filepath.Join(homeDir, ".config", "dbman", "config.json")
}()

type Config struct {
	Connections map[string]Connection `json:"connections"`
	Tunnels     map[string]SSHTunnel  `json:"tunnels"`
}

type Connection struct {
	Host              string            `json:"host,omitempty"`
	Port              int               `json:"port,omitempty"`
	Database          string            `json:"database,omitempty"`
	Username          string            `json:"username,omitempty"`
	Password          string            `json:"password,omitempty"` // optional, prompted for if empty
	Driver            string            `json:"driver,omitempty"`
	DriverOpts        map[string]string `json:"driver_opts,omitempty"`
	Tunnel            string            `json:"tunnel,omitempty"`              // optional
	ConnectTimeoutSec int               `json:"connect_timeout_sec,omitempty"` // optional
	MaxOpenConns      int               `json:"max_open_conns,omitempty"`
}

type SSHTunnel struct {
	Host                   string     `json:"host,omitempty"`
	Port                   int        `json:"port,omitempty"`
	User                   string     `json:"user,omitempty"`
	AuthMethod             AuthMethod `json:"auth_method,omitempty"`
	Password               string     `json:"password,omitempty"`               // only used if auth_method is 'password'; optional, prompted for if empty
	PrivateKeyFile         string     `json:"private_key_file,omitempty"`       // only used if auth_method is 'public_key'
	PrivateKeyPassphrase   string     `json:"private_key_passphrase,omitempty"` // only used if auth_method is 'public_key' and private key is encrypted
	ConnectTimeoutSec      int        `json:"connect_timeout_sec,omitempty"`    // optional
	DisableVerifyKnownHost bool       `json:"disable_verify_known_host,omitempty"`
	HostPublicKeyFile      string     `json:"host_public_key_file,omitempty"` // optional
}

type AuthMethod string

const (
	PasswordAuth  AuthMethod = "password"
	PublicKeyAuth AuthMethod = "public_key"
	AgentAuth     AuthMethod = "agent"
)

func LoadConfig(filepath string, isDefault bool, cfg *Config) {
	f, err := os.Open(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			if isDefault {
				f, err = os.Create(filepath)
				if err != nil {
					log.Fatal("failed to create default config file:", err)
				}
				defer f.Close()
				cfg.Connections = make(map[string]Connection)
				cfg.Tunnels = make(map[string]SSHTunnel)
				json.NewEncoder(f).Encode(cfg)

				log.Print("Default config file could not be found at ~/.config/dbman/config.json")
				log.Fatal("An empty config has been created - please fill it out.")
			}

			log.Fatalf("%s could not be found: %v", filepath, err)
		}

		log.Fatalf("could not open %s: %v", filepath, err)
	}
	defer f.Close()

	if err := json.NewDecoder(f).Decode(cfg); err != nil {
		log.Fatal("invalid config json:", err)
	}

	if err := cfg.validate(); err != nil {
		log.Fatal("invalid config:", err)
	}

	if len(cfg.Connections) == 0 {
		log.Fatal("no connections defined in " + filepath)
	}
}

func (c *Config) validate() error {
	var errs errorList

	for k, v := range c.Connections {
		if err := v.validate(k); err != nil {
			errs = append(errs, err)
		}

		if v.Tunnel != "" {
			if c.Tunnels == nil {
				errs = append(errs, fmt.Errorf("tunnel '%s' does not exist", v.Tunnel))
			} else if _, ok := c.Tunnels[v.Tunnel]; !ok {
				errs = append(errs, fmt.Errorf("tunnel '%s' does not exist", v.Tunnel))
			}
		}
	}

	for k, v := range c.Tunnels {
		if err := v.validate(k); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) != 0 {
		return errs
	}
	return nil
}

func (c *Connection) validate(prefix string) error {
	var errs errorList

	if c.Host == "" {
		errs = append(errs, errors.New(prefix+".host: required"))
	}
	if c.Port == 0 {
		errs = append(errs, errors.New(prefix+".port: required"))
	}
	if c.Database == "" {
		errs = append(errs, errors.New(prefix+".database: required"))
	}
	if c.Username == "" {
		errs = append(errs, errors.New(prefix+".username: required"))
	}
	if c.Driver == "" {
		errs = append(errs, errors.New(prefix+".driver: required"))
	} else if !stringsContains(sql.Drivers(), c.Driver) {
		errs = append(errs, errors.New(prefix+".driver: not a supported driver"))
	}
	if c.ConnectTimeoutSec < 0 {
		errs = append(errs, errors.New(prefix+".connect_timeout: must be greater than or equal to 0"))
	}

	if len(errs) != 0 {
		return errs
	}
	return nil
}

func (s *SSHTunnel) validate(prefix string) error {
	var errs errorList

	if s.Host == "" {
		errs = append(errs, errors.New(prefix+".host: required"))
	}
	if s.Port == 0 {
		errs = append(errs, errors.New(prefix+".port: required"))
	}
	if s.User == "" {
		errs = append(errs, errors.New(prefix+".user: required"))
	}
	if err := s.AuthMethod.validate(); err != nil {
		errs = append(errs, errors.New(prefix+".auth_method: "+err.Error()))
	}
	if s.ConnectTimeoutSec < 0 {
		errs = append(errs, errors.New(prefix+".connect_timeout: must be greater than or equal to 0"))
	}

	if len(errs) != 0 {
		return errs
	}
	return nil
}

func (a AuthMethod) validate() error {
	switch a {
	case PasswordAuth, PublicKeyAuth, AgentAuth:
		return nil

	case "":
		return errors.New("required")

	default:
		return errors.New("must be one of: password, public_key, agent")
	}
}

type errorList []error

func makeErrorList(errs ...error) error {
	list := make(errorList, 0, len(errs))

	for _, err := range errs {
		switch v := err.(type) {
		case errorList:
			list = append(list, v...)
		case nil:
			continue
		default:
			list = append(list, err)
		}
	}

	if len(list) != 0 {
		return list
	}
	return nil
}

func (e errorList) Error() string {
	var sb strings.Builder

	for _, err := range e {
		sb.WriteString(err.Error())
		sb.WriteByte('\n')
	}

	return sb.String()
}
