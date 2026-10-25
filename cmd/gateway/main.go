package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"2droundserver/internal/config"
	"2droundserver/internal/game"
	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func main() {
	cfgPath := flag.String("config", "configs/dev.yaml", "config file")
	verbose := flag.Bool("v", true, "log every received and sent command")
	flag.Parse()
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	codec, err := cfg.CodecOptions()
	if err != nil {
		log.Fatal(err)
	}
	schema, err := proto.LoadSchema()
	if err != nil {
		log.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	// The server row is created once; later edits (e.g. from the admin) are kept.
	if err := st.EnsureServer(cfg.DefaultServer()); err != nil {
		log.Fatal(err)
	}
	reportConfig(cfg, st)
	// Sessions that ended while no gateway was running (it was killed or crashed)
	// never got a logout time; give them one so friends do not see them as just left.
	if n, err := st.MarkStaleSessionsOffline(); err != nil {
		log.Fatal(err)
	} else if n > 0 {
		log.Printf("marked %d roles offline that were left online by a previous run", n)
	}

	logOut, closeLog, err := cfg.OpenLog("gateway")
	if err != nil {
		log.Fatal(err)
	}
	defer closeLog()
	logger := log.New(logOut, "", log.LstdFlags|log.Lmicroseconds)
	gw := &gateway.Server{Schema: schema, Codec: codec, Idle: time.Duration(cfg.Gateway.IdleSeconds) * time.Second,
		LoginTimeout: time.Duration(cfg.Gateway.LoginTimeoutSeconds) * time.Second, MaxConnsPerIP: cfg.Gateway.MaxConnsPerIP, Log: logger, Verbose: *verbose}
	gw.Hub = gateway.NewHub()
	gw.Router = gateway.NewRouter()
	if cfg.Codec.Confirmed {
		friends := &game.FriendService{Store: st, SrvID: cfg.Gateway.SrvID, Hub: gw.Hub, Log: logger}
		friends.Register(gw.Router)
		online := &game.OnlineGiftService{Store: st, Hub: gw.Hub, Log: logger}
		online.Register(gw.Router)
		(&game.CheckinService{Store: st}).Register(gw.Router)
		(&game.GuildService{Store: st, SrvID: cfg.Gateway.SrvID, Hub: gw.Hub, Log: logger}).Register(gw.Router)
		(&game.InviteService{Store: st, SrvID: cfg.Gateway.SrvID, Hub: gw.Hub, Log: logger}).Register(gw.Router)
		gw.OnDisconnect = func(s *gateway.Session) { friends.Disconnected(s); online.Disconnected(s) }
		chat := &game.ChatService{Store: st, SrvID: cfg.Gateway.SrvID, Hub: gw.Hub, Log: logger, MinInterval: time.Second}
		chat.Register(gw.Router)
		(&game.Login{Store: st, SrvID: cfg.Gateway.SrvID, Hub: gw.Hub, Log: logger, MaxRoles: 3, Friends: friends, OnlineGift: online, Chat: chat, NewRoleFlow: cfg.Gateway.NewRoleFlow}).Register(gw.Router)
		mail := &game.MailService{Store: st, SrvID: cfg.Gateway.SrvID, Hub: gw.Hub, Log: logger}
		mail.Register(gw.Router)
		notices := &game.NoticeService{Store: st, Hub: gw.Hub, Log: logger}
		notices.Register(gw.Router)
		mail.Notices = notices
		if cfg.Gateway.AdminAddr != "" { // loopback control endpoint: live mail push
			go func() {
				logger.Printf("control endpoint on %s: %v", cfg.Gateway.AdminAddr, http.ListenAndServe(cfg.Gateway.AdminAddr, mail.AdminHandler()))
			}()
		}
		game.RegisterBag(gw.Router, st)
		game.RegisterUse(gw.Router, st)
		game.RegisterPartner(gw.Router, st)
		game.RegisterQuest(gw.Router, st)
		game.RegisterLooks(gw.Router, st)
		game.RegisterRoleMisc(gw.Router, st)
		game.RegisterShop(gw.Router, st)
		game.RegisterMarket(gw.Router, st)
		game.RegisterArena(gw.Router, st)
		game.RegisterStarTower(gw.Router, st)
		game.RegisterAdventure(gw.Router, st)
		game.RegisterPlanesafk(gw.Router, st)
		game.RegisterPlanes(gw.Router, st)
		game.RegisterStoneDungeon(gw.Router, st)
		game.RegisterForge(gw.Router, st)
		game.RegisterDramaBattle(gw.Router, st)
		game.RegisterDramaExtras(gw.Router, st)
		game.RegisterDramaMilestones(gw.Router, st)
		game.RegisterRank(gw.Router, st)
		game.RegisterEquipment(gw.Router, st)
		game.RegisterFormation(gw.Router, st)
		game.RegisterFeat(gw.Router, st)
		game.RegisterCompose(gw.Router, st)
		game.RegisterBattle(gw.Router, st)
		game.RegisterElfin(gw.Router, st)
		if cfg.Gateway.EmptyReplies {
			game.RegisterEmpty(gw.Router, logger) // after the real handlers, which always win
		}
	} else {
		logger.Printf("codec.confirmed is false: capture mode, frames are logged but never answered")
	}
	if cfg.Gateway.WSAddr != "" && cfg.Codec.Confirmed {
		mux := http.NewServeMux()
		mux.Handle("/ws", gw.WSHandler(gateway.WSOptions{OriginPatterns: cfg.Gateway.WSOrigins}))
		ws := &http.Server{Addr: cfg.Gateway.WSAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 1 << 14}
		go func() {
			if cfg.Gateway.WSTLSCert != "" {
				logger.Printf("websocket (wss) listening on %s/ws", cfg.Gateway.WSAddr)
				logger.Fatal(ws.ListenAndServeTLS(cfg.Gateway.WSTLSCert, cfg.Gateway.WSTLSKey))
			}
			logger.Printf("websocket listening on %s/ws, PLAIN ws: WeChat and Douyin require wss, so put a TLS proxy in front or set gateway.ws_tls_cert/ws_tls_key", cfg.Gateway.WSAddr)
			logger.Fatal(ws.ListenAndServe())
		}()
	}
	ln, err := net.Listen("tcp", cfg.Gateway.TCPAddr)
	if err != nil {
		log.Fatal(err)
	}
	logger.Printf("gateway %s listening on %s", cfg.Gateway.SrvID, ln.Addr())
	// On SIGINT / SIGTERM close every session properly, so each one runs its disconnect
	// hooks (logout time, offline notices, online-time accounting) before the process ends.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	closed := make(chan struct{})
	go func() {
		s := <-sig
		logger.Printf("%v: closing all sessions", s)
		gw.Close() // returns once every session has ended
		close(closed)
	}()
	if err := gw.Serve(ln); err != nil {
		log.Fatal(err)
	}
	<-closed // Serve returns as soon as the listener is closed; wait for the sessions
	logger.Printf("gateway stopped")
}

// reportConfig logs the settings that contradict each other (see config.Check).
func reportConfig(cfg *config.Config, st *store.Store) {
	var row *store.Server
	if srv, err := st.Server(cfg.Gateway.SrvID); err == nil {
		row = srv
	}
	for _, f := range cfg.Check(row) {
		log.Printf("config: %s", f)
	}
}
