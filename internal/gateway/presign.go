package gateway

import (
	"crypto/md5"
	"encoding/hex"
	"strconv"
)

// signKey is the constant the client mixes into every 1197 signature
// (gamenet.lua SendPrev). It is a client-side artefact, not a secret and not a
// security mechanism; the gateway only uses it to log suspicious frames.
const signKey = "__sszg666__"

// signExempt reports whether the client sends a command without a 1197 in
// front of it (gamenet.lua SendPrev): 10300, 16601-16603, 111xx and 200xx.
func signExempt(cmd uint16) bool {
	switch cmd {
	case 10300, 16601, 16602, 16603:
		return true
	}
	g := cmd / 100
	return g == 111 || g == 200
}

// expectedSign = md5(idx .. time .. cmd .. signKey), confirmed against a real
// capture (testdata/captures/real-login-2026-10-18).
func expectedSign(idx, time int64, cmd uint16) string {
	sum := md5.Sum([]byte(strconv.FormatInt(idx, 10) + strconv.FormatInt(time, 10) + strconv.Itoa(int(cmd)) + signKey))
	return hex.EncodeToString(sum[:])
}

// expectedCode is the 1197 "code" field for a command at sequence number idx.
func expectedCode(cmd uint16, idx int64) int64 { return int64(cmd) + (idx+5)%16 }
