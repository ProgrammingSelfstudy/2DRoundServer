package main

import (
	"io"
	"log"
	"testing"

	"2droundserver/internal/config"
	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestProductionGameRegistration(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.SrvID = "symlf_1"
	cfg.Gateway.EmptyReplies = true
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	gw := &gateway.Server{Hub: gateway.NewHub(), Router: gateway.NewStrictRouter(), Log: log.New(io.Discard, "", 0)}
	registerGame(gw, st, cfg)
	for _, cmd := range []uint16{1164, 11000, 13402, proto.H5ShopCatalog, proto.H5ShopBuy} {
		if !gw.Router.Has(cmd) {
			t.Errorf("command %d missing from production gateway", cmd)
		}
	}
}
