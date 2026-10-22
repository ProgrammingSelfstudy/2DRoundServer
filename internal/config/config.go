// Package config loads the YAML configuration shared by all binaries.
package config

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"2droundserver/internal/logfile"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

type Config struct {
	DBPath string `yaml:"db_path"`
	// Log files: with log_dir set, every service also writes <log_dir>/<service>.log and rolls
	// it over at log_max_mb megabytes, keeping the newest log_keep rolled files. Empty log_dir =
	// the terminal only.
	LogDir   string `yaml:"log_dir"`
	LogMaxMB int    `yaml:"log_max_mb"`
	LogKeep  int    `yaml:"log_keep"`

	LoginHTTP struct {
		Addr          string `yaml:"addr"`
		AutoRegister  bool   `yaml:"auto_register"` // create account on first maketoken
		TokenTTLHours int    `yaml:"token_ttl_hours"`
		PublicBase    string `yaml:"public_base"` // used in url_config.lua
	} `yaml:"login_http"`

	Gateway struct {
		TCPAddr     string   `yaml:"tcp_addr"`
		WSAddr      string   `yaml:"ws_addr"`     // empty = disabled
		WSOrigins   []string `yaml:"ws_origins"`  // browser Origin hosts allowed (none needed for native / mini-game clients)
		WSTLSCert   string   `yaml:"ws_tls_cert"` // with ws_tls_key serves wss directly; otherwise put a TLS proxy in front
		WSTLSKey    string   `yaml:"ws_tls_key"`
		SrvID       string   `yaml:"srv_id"`
		IdleSeconds int      `yaml:"idle_seconds"`
		// A connection that has not logged in within login_timeout_seconds is dropped;
		// one client address may hold at most max_conns_per_ip connections (0 = no cap).
		LoginTimeoutSeconds int    `yaml:"login_timeout_seconds"`
		MaxConnsPerIP       int    `yaml:"max_conns_per_ip"`
		AdminAddr           string `yaml:"admin_addr"` // loopback endpoint admin uses to push to online roles
		// EmptyReplies answers the commands in game/empty_gen.go with empty messages
		// so the client can get past loading. Development aid; off by default.
		EmptyReplies bool `yaml:"empty_replies"`
	} `yaml:"gateway"`

	Admin struct {
		Addr      string `yaml:"addr"`
		JWTSecret string `yaml:"jwt_secret"`
		TTLHours  int    `yaml:"ttl_hours"`
		// GatewayURL is where the gateway listens for control calls (live mail push, kick).
		// Empty = http:// + gateway.admin_addr.
		GatewayURL string `yaml:"gateway_url"`
		// Database backups (made while the server runs). Empty backup_dir = "backups" next
		// to the database file. A backup is made every backup_every_hours (0 = never on
		// its own; the admin API can still make one) and the newest backup_keep are kept.
		BackupDir        string `yaml:"backup_dir"`
		BackupKeep       int    `yaml:"backup_keep"`
		BackupEveryHours int    `yaml:"backup_every_hours"`
	} `yaml:"admin"`

	// Codec holds the field-encoding details that must be confirmed from a
	// real capture (see testdata/captures). Defaults are placeholders.
	Codec struct {
		ByteOrder   string `yaml:"byte_order"` // big | little
		StrLenBytes int    `yaml:"str_len_bytes"`
		ArrLenBytes int    `yaml:"arr_len_bytes"`
		StrNullTerm bool   `yaml:"str_null_term"`
		Confirmed   bool   `yaml:"confirmed"` // gateway refuses business replies until true
	} `yaml:"codec"`
}

func Default() *Config {
	c := &Config{DBPath: "data/2droundserver.db"}
	c.LoginHTTP.Addr = "127.0.0.1:8081"
	c.LoginHTTP.AutoRegister = true
	c.LoginHTTP.TokenTTLHours = 24
	c.LoginHTTP.PublicBase = "http://127.0.0.1:8081"
	c.Gateway.TCPAddr = "127.0.0.1:8800"
	c.Gateway.SrvID = "symlf_1"
	c.LogMaxMB = 20
	c.LogKeep = 5
	c.Admin.BackupKeep = 14
	c.Admin.BackupEveryHours = 24
	c.Gateway.IdleSeconds = 120
	c.Gateway.LoginTimeoutSeconds = 20
	c.Gateway.MaxConnsPerIP = 50
	c.Gateway.AdminAddr = "127.0.0.1:8801"
	c.Admin.Addr = "127.0.0.1:8080"
	c.Admin.TTLHours = 12
	c.Codec.ByteOrder = "big"
	c.Codec.StrLenBytes = 2
	c.Codec.ArrLenBytes = 2
	return c
}

func Load(path string) (*Config, error) {
	c := Default()
	if path == "" {
		return c, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return c, nil
}

func (c *Config) CodecOptions() (proto.Options, error) {
	o := proto.Options{StrLenBytes: c.Codec.StrLenBytes, ArrLenBytes: c.Codec.ArrLenBytes, StrNullTerm: c.Codec.StrNullTerm}
	switch c.Codec.ByteOrder {
	case "big", "":
		o.ByteOrder = binary.BigEndian
	case "little":
		o.ByteOrder = binary.LittleEndian
	default:
		return o, fmt.Errorf("codec.byte_order must be big or little, got %q", c.Codec.ByteOrder)
	}
	return o, nil
}

// DefaultServer is the row both the gateway and the login service create (only
// if missing) for the gateway's own srv_id, so a fresh database is usable.
func (c *Config) DefaultServer() store.Server {
	host, portStr, err := net.SplitHostPort(c.Gateway.TCPAddr)
	port := 8800
	if err == nil {
		if n, e := strconv.Atoi(portStr); e == nil {
			port = n
		}
	} else {
		host = c.Gateway.TCPAddr
	}
	zone := 1
	if i := strings.LastIndex(c.Gateway.SrvID, "_"); i >= 0 {
		if n, e := strconv.Atoi(c.Gateway.SrvID[i+1:]); e == nil {
			zone = n
		}
	}
	return store.Server{SrvID: c.Gateway.SrvID, Name: "本机测试服", Platform: "symlf", ZoneID: zone, Host: host, Port: port,
		Status: 1, IsNew: true, IsRecommend: true, MainSrvID: c.Gateway.SrvID, GroupID: 1}
}

// BackupDirPath is where backups go: admin.backup_dir, or "backups" next to the database file.
func (c *Config) BackupDirPath() string {
	if c.Admin.BackupDir != "" {
		return c.Admin.BackupDir
	}
	return filepath.Join(filepath.Dir(c.DBPath), "backups")
}

// OpenLog returns where a service writes its log: the terminal, and <log_dir>/<name>.log too
// when log_dir is set. It also makes the standard logger (log.Printf, log.Fatal) write there.
// The returned function closes the file.
func (c *Config) OpenLog(name string) (io.Writer, func(), error) {
	if c.LogDir == "" {
		return os.Stdout, func() {}, nil
	}
	f, err := logfile.Open(c.LogDir, name, int64(c.LogMaxMB)*1024*1024, c.LogKeep)
	if err != nil {
		return nil, nil, fmt.Errorf("log_dir %s: %w", c.LogDir, err)
	}
	w := io.MultiWriter(os.Stdout, f)
	log.SetOutput(w)
	return w, func() { f.Close() }, nil
}
