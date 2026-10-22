package config

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"2droundserver/internal/store"
)

// good is a consistent LAN setup: nothing may be reported for it except the backup note
// when backups are off.
func good() (*Config, *store.Server) {
	c := Default()
	c.LoginHTTP.Addr = "0.0.0.0:8081"
	c.LoginHTTP.PublicBase = "http://192.168.110.186:8081"
	c.Gateway.TCPAddr = "0.0.0.0:8800"
	c.Admin.Addr = "127.0.0.1:8090"
	c.Codec.Confirmed = true
	return c, &store.Server{SrvID: "symlf_1", Host: "192.168.110.186", Port: 8800}
}

func has(fs []Finding, isErr bool, sub string) bool {
	for _, f := range fs {
		if f.Error == isErr && strings.Contains(f.Msg, sub) {
			return true
		}
	}
	return false
}

func TestCheckAcceptsConsistentSetups(t *testing.T) {
	c, srv := good()
	if fs := c.Check(srv); len(fs) != 0 {
		t.Fatalf("a consistent LAN setup was reported: %v", fs)
	}
	// the all-loopback development setup (adb reverse) is consistent too
	d := Default()
	d.Codec.Confirmed = true
	if fs := d.Check(&store.Server{SrvID: "symlf_1", Host: "127.0.0.1", Port: 8800}); len(fs) != 0 {
		t.Fatalf("the loopback setup was reported: %v", fs)
	}
	// a server list entry that is not there yet is not an error
	if fs := d.Check(nil); len(fs) != 0 {
		t.Fatalf("no server row: %v", fs)
	}
}

func TestCheckFindsContradictions(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edit  func(*Config, *store.Server)
		isErr bool
		want  string
	}{
		{"server host is a wildcard", func(c *Config, s *store.Server) { s.Host = "0.0.0.0" }, true, "no client can reach"},
		{"server host is empty", func(c *Config, s *store.Server) { s.Host = "" }, true, "no client can reach"},
		{"LAN host but loopback-only gateway", func(c *Config, s *store.Server) { c.Gateway.TCPAddr = "127.0.0.1:8800" }, true, "only this machine can reach (gateway.tcp_addr)"},
		{"port mismatch", func(c *Config, s *store.Server) { s.Port = 9000 }, false, "says port 9000 but the gateway listens on 8800"},
		{"LAN public_base but loopback-only login service", func(c *Config, s *store.Server) { c.LoginHTTP.Addr = "127.0.0.1:8081" }, true, "login_http.addr"},
		{"public_base is not a URL", func(c *Config, s *store.Server) { c.LoginHTTP.PublicBase = "192.168.1.10:8081" }, true, "is not a URL"},
		{"public_base without a scheme or host", func(c *Config, s *store.Server) { c.LoginHTTP.PublicBase = "" }, true, "is not a URL"},
		{"unauthenticated control endpoint on the network", func(c *Config, s *store.Server) { c.Gateway.AdminAddr = "0.0.0.0:8801" }, true, "no authentication"},
		{"control endpoint on a LAN address", func(c *Config, s *store.Server) { c.Gateway.AdminAddr = "192.168.1.5:8801" }, true, "no authentication"},
		{"admin on the network", func(c *Config, s *store.Server) { c.Admin.Addr = "0.0.0.0:8090" }, false, "plain HTTP"},
		{"login timeout longer than idle", func(c *Config, s *store.Server) { c.Gateway.LoginTimeoutSeconds = 300 }, false, "never applies"},
		{"backups off", func(c *Config, s *store.Server) { c.Admin.BackupEveryHours = 0 }, false, "backups are off"},
		{"codec not confirmed", func(c *Config, s *store.Server) { c.Codec.Confirmed = false }, true, "answers none"},
	} {
		c, srv := good()
		tc.edit(c, srv)
		if fs := c.Check(srv); !has(fs, tc.isErr, tc.want) {
			t.Errorf("%s: want a finding (error=%v) containing %q, got %v", tc.name, tc.isErr, tc.want, fs)
		}
	}
}

func TestCheckOnlyBlamesWhatIsWrong(t *testing.T) {
	// a loopback public_base with a loopback-only login service is fine whatever the server list says
	c, srv := good()
	c.LoginHTTP.PublicBase = "http://127.0.0.1:8081"
	c.LoginHTTP.Addr = "127.0.0.1:8081"
	if fs := c.Check(srv); len(fs) != 0 {
		t.Fatalf("%v", fs)
	}
	// IPv6 loopback spellings count as loopback
	c, srv = good()
	c.Gateway.AdminAddr = "[::1]:8801"
	if fs := c.Check(srv); len(fs) != 0 {
		t.Fatalf("::1 control endpoint: %v", fs)
	}
	// every finding prints with its level
	if s := (Finding{true, "x"}).String(); s != "ERROR x" {
		t.Fatal(s)
	}
	if s := (Finding{false, "x"}).String(); s != "warning x" {
		t.Fatal(s)
	}
}

func TestOpenLog(t *testing.T) {
	c := Default()
	// no log_dir: the terminal only, and nothing is created
	w, closeLog, err := c.OpenLog("gateway")
	if err != nil || w != os.Stdout {
		t.Fatalf("no log_dir: %v %v", w, err)
	}
	closeLog()

	c.LogDir = filepath.Join(t.TempDir(), "logs")
	c.LogMaxMB = 1
	c.LogKeep = 2
	out := log.Writer()
	defer log.SetOutput(out)
	w, closeLog, err = c.OpenLog("gateway")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("hello from the service\n"))
	log.Print("from the standard logger") // log.Fatal and log.Printf go to the file too
	closeLog()
	b, err := os.ReadFile(filepath.Join(c.LogDir, "gateway.log"))
	if err != nil || !strings.Contains(string(b), "hello from the service") || !strings.Contains(string(b), "from the standard logger") {
		t.Fatalf("log file: %q %v", b, err)
	}

	// a log_dir that cannot be made is an error, not a silent loss of the logs
	file := filepath.Join(t.TempDir(), "afile")
	os.WriteFile(file, []byte("x"), 0o600)
	c.LogDir = filepath.Join(file, "sub") // below a regular file
	if _, _, err := c.OpenLog("gateway"); err == nil {
		t.Fatal("an unusable log_dir must fail")
	}
}
