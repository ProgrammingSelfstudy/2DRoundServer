package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"2droundserver/internal/store"
)

// Finding is something the configuration gets wrong or that deserves a second look.
type Finding struct {
	Error bool // true: something will not work; false: worth a look
	Msg   string
}

func (f Finding) String() string {
	if f.Error {
		return "ERROR " + f.Msg
	}
	return "warning " + f.Msg
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func isWildcard(host string) bool {
	if host == "" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsUnspecified()
}

// splitAddr gives the host and the port of a listen address like "0.0.0.0:8800" or ":8800".
func splitAddr(addr string) (host string, port int) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, 0
	}
	n, _ := strconv.Atoi(p)
	return h, n
}

// reachableFromNetwork reports whether a listen address accepts connections from other
// machines: a wildcard or a non-loopback address does, a loopback one does not.
func reachableFromNetwork(addr string) bool {
	h, _ := splitAddr(addr)
	return !isLoopback(h)
}

// Check looks for settings that contradict each other. srv is this gateway's row of the
// server list (nil when there is none yet). Every finding says what is wrong and what
// it will do; none of them stops the services, so a development setup can ignore them.
func (c *Config) Check(srv *store.Server) []Finding {
	var out []Finding
	add := func(isErr bool, format string, a ...any) {
		out = append(out, Finding{isErr, fmt.Sprintf(format, a...)})
	}

	if srv != nil {
		switch {
		case isWildcard(srv.Host):
			add(true, "the server list tells clients to connect to %q (server %s); no client can reach that, put the machine's real address in the server's host", srv.Host, srv.SrvID)
		case !isLoopback(srv.Host) && !reachableFromNetwork(c.Gateway.TCPAddr):
			add(true, "the server list gives clients %s:%d, but the gateway listens on %s, which only this machine can reach (gateway.tcp_addr)", srv.Host, srv.Port, c.Gateway.TCPAddr)
		}
		if _, p := splitAddr(c.Gateway.TCPAddr); p != 0 && srv.Port != 0 && srv.Port != p {
			add(false, "the server list says port %d but the gateway listens on %d (fine only behind a port mapping)", srv.Port, p)
		}
	}

	if u, err := url.Parse(c.LoginHTTP.PublicBase); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		add(true, "login_http.public_base %q is not a URL like http://192.168.1.10:8081", c.LoginHTTP.PublicBase)
	} else if !isLoopback(u.Hostname()) && !reachableFromNetwork(c.LoginHTTP.Addr) {
		add(true, "login_http.public_base points clients at %s, but the login service listens on %s, which only this machine can reach (login_http.addr)", u.Hostname(), c.LoginHTTP.Addr)
	}

	if reachableFromNetwork(c.Gateway.AdminAddr) {
		add(true, "gateway.admin_addr %s is reachable from the network, and that control endpoint has no authentication (anyone could kick players or push mail); bind it to 127.0.0.1", c.Gateway.AdminAddr)
	}
	if reachableFromNetwork(c.Admin.Addr) {
		add(false, "admin.addr %s is reachable from the network and speaks plain HTTP (logins and the JWT cross the network in the clear); fine at home, put TLS in front of it otherwise", c.Admin.Addr)
	}
	if c.Gateway.LoginTimeoutSeconds > c.Gateway.IdleSeconds && c.Gateway.IdleSeconds > 0 {
		add(false, "gateway.login_timeout_seconds (%d) is longer than idle_seconds (%d), so it never applies", c.Gateway.LoginTimeoutSeconds, c.Gateway.IdleSeconds)
	}
	if c.Admin.BackupEveryHours <= 0 {
		add(false, "automatic database backups are off (admin.backup_every_hours is %d)", c.Admin.BackupEveryHours)
	}
	if !c.Codec.Confirmed {
		add(true, "codec.confirmed is false: the gateway logs frames but answers none")
	}
	return out
}
