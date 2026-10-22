// bot plays the main line against a running server, like a scripted client, and
// prints one line per step. It checks the real processes, configuration and
// database (the unit tests only check the code), without a phone.
//
//	go run ./cmd/bot -http http://127.0.0.1:8081 -tcp 127.0.0.1:8800 -user bot12345 -pass secret123
//
// The account is created on first use when the login service allows auto_register;
// a role is created if the account has none. Exit status 1 when a step fails.
package main

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"2droundserver/internal/proto"
)

type bot struct {
	c      net.Conn
	br     *bufio.Reader
	schema *proto.Schema
	idx    int64
	seen   map[uint16]int // frames received by command, for the summary
	failed bool
}

// exempt mirrors the client: these commands are sent without a signed 1197 before them.
func exempt(cmd uint16) bool {
	return cmd == 10300 || (cmd >= 16601 && cmd <= 16603) || cmd/100 == 111 || cmd/100 == 200
}

func (b *bot) send(cmd uint16, msg proto.Message) error {
	o := proto.DefaultOptions()
	var out []byte
	if !exempt(cmd) {
		b.idx++
		now := time.Now().Unix()
		sum := md5.Sum([]byte(fmt.Sprintf("%d%d%d__sszg666__", b.idx, now, cmd)))
		p, err := proto.Encode(o, b.schema.Send[1197], proto.Message{"sign": hex.EncodeToString(sum[:]), "idx": b.idx, "code": int64(cmd) + (b.idx+5)%16, "time": now})
		if err != nil {
			return err
		}
		f, _ := proto.Frame{Command: 1197, Payload: p}.Encode()
		out = append(out, f...)
	}
	p, err := proto.Encode(o, b.schema.Send[cmd], msg)
	if err != nil {
		return fmt.Errorf("encode %d: %w", cmd, err)
	}
	f, _ := proto.Frame{Command: cmd, Payload: p}.Encode()
	_, err = b.c.Write(append(out, f...))
	return err
}

// recv reads frames until one is cmd; the others are pushes and are counted and skipped.
func (b *bot) recv(cmd uint16) (proto.Message, error) { return b.recvWithin(cmd, 5*time.Second) }

func (b *bot) recvWithin(cmd uint16, wait time.Duration) (proto.Message, error) {
	deadline := time.Now().Add(wait)
	for {
		b.c.SetReadDeadline(deadline)
		fr, err := proto.ReadFrame(b.br)
		if err != nil {
			return nil, fmt.Errorf("waiting for %d: %w", cmd, err)
		}
		b.seen[fr.Command]++
		if fr.Command != cmd {
			continue
		}
		return proto.Decode(proto.DefaultOptions(), b.schema.Recv[cmd], fr.Payload)
	}
}

// step sends a command, waits for its answer and runs the check on it.
func (b *bot) step(name string, cmd uint16, msg proto.Message, check func(proto.Message) error) proto.Message {
	if err := b.send(cmd, msg); err != nil {
		return b.fail(name, err)
	}
	r, err := b.recv(cmd)
	if err != nil {
		return b.fail(name, err)
	}
	if check != nil {
		if err := check(r); err != nil {
			return b.fail(name, fmt.Errorf("%w (reply %v)", err, r))
		}
	}
	fmt.Printf("ok    %s\n", name)
	return r
}

func (b *bot) fail(name string, err error) proto.Message {
	fmt.Printf("FAIL  %s: %v\n", name, err)
	b.failed = true
	return proto.Message{}
}

func want(cond bool, what string) error {
	if !cond {
		return fmt.Errorf("expected %s", what)
	}
	return nil
}

func token(base, user, pass string) (string, error) {
	u := base + "/userlogin/userLogins2?username=" + url.QueryEscape(user) + "&password=" + url.QueryEscape(pass)
	resp, err := http.Get(u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	m := regexp.MustCompile(`msg="([^"]*)"`).FindSubmatch(body)
	if m == nil || !strings.Contains(string(body), "code=1") {
		return "", fmt.Errorf("login refused: %s", body)
	}
	return string(m[1]), nil
}

func main() {
	httpBase := flag.String("http", "http://127.0.0.1:8081", "login service base URL")
	tcp := flag.String("tcp", "127.0.0.1:8800", "gateway address")
	user := flag.String("user", "", "account name (6-12 letters or digits)")
	pass := flag.String("pass", "secret123", "account password")
	srv := flag.String("srv", "symlf_1", "server id")
	fights := flag.Int("fights", 3, "campaign challenges to play")
	flag.Parse()
	if *user == "" {
		*user = fmt.Sprintf("bot%d", time.Now().Unix()%1000000)
	}
	schema, err := proto.LoadSchema()
	if err != nil {
		fmt.Println(err)
		os.Exit(2)
	}
	tok, err := token(*httpBase, *user, *pass)
	if err != nil {
		fmt.Println("FAIL  http login:", err)
		os.Exit(1)
	}
	fmt.Printf("ok    http login (account %s)\n", *user)
	c, err := net.Dial("tcp", *tcp)
	if err != nil {
		fmt.Println("FAIL  connect:", err)
		os.Exit(1)
	}
	defer c.Close()
	c.Write([]byte(proto.Handshake))
	b := &bot{c: c, br: bufio.NewReader(c), schema: schema, seen: map[uint16]int{}}
	kv := func(k, v string) proto.Message { return proto.Message{"key": k, "val": v} }

	list := b.step("1164 role list", 1164, proto.Message{"args": []proto.Message{kv("account", *user), kv("token", tok), kv("enter_srv_id", *srv), kv("platform", "symlf")}},
		func(r proto.Message) error { return want(r.Int("code") == 1, "code 1") })
	rid := int64(0)
	if roles := list.Arr("roles"); len(roles) > 0 {
		rid = roles[0].Int("rid")
	} else {
		cr := b.step("10191 create role", 10191, proto.Message{"sex": 1, "name": "", "career": 2, "playform": "symlf"},
			func(r proto.Message) error { return want(r.Int("code") == 1 && r.Int("rid") != 0, "a new role") })
		rid = cr.Int("rid")
	}
	b.step("10192 enter", 10192, proto.Message{"rid": rid, "srv_id": *srv}, func(r proto.Message) error { return want(r.Int("code") == 1, "code 1") })
	if err := b.send(10300, proto.Message{}); err == nil {
		for _, cmd := range []uint16{10301, 10302, 13000, 10304} {
			if _, err := b.recv(cmd); err != nil {
				b.fail("10300 role data", err)
				break
			}
		}
		fmt.Println("ok    10300 role data and main city")
	}

	heroes := b.step("11025 hero list", 11025, proto.Message{}, func(r proto.Message) error { return want(len(r.Arr("partners")) >= 1, "at least one hero") })
	b.step("11211 campaign formation", 11211, proto.Message{"type": 0}, func(r proto.Message) error { return want(len(r.Arr("pos_info")) >= 1, "heroes in the formation") })
	_ = heroes
	for i := 1; i <= *fights; i++ {
		b.step(fmt.Sprintf("13003 challenge %d", i), 13003, proto.Message{"is_auto": 0}, func(r proto.Message) error { return want(r.Int("code") == 1, "code 1") })
		b.playFight()
	}
	// the free summon (once a day): a second hero, which then goes into the formation
	// (a refused summon is not answered, so a short wait tells that it was used already)
	var free proto.Message
	if err := b.send(23201, proto.Message{"group_id": 100, "times": 1, "recruit_type": 1}); err != nil {
		b.fail("23201 free summon", err)
	} else if r, err := b.recvWithin(23201, 800*time.Millisecond); err == nil {
		free = r
		fmt.Println("ok    23201 free summon")
	}
	if len(free.Arr("partner_bids")) == 1 {
		if _, err := b.recv(11001); err != nil {
			b.fail("11001 new hero", err)
		}
		now := b.step("11025 hero list after the summon", 11025, proto.Message{}, func(r proto.Message) error { return want(len(r.Arr("partners")) >= 2, "two heroes") })
		var pos []proto.Message
		for i, h := range now.Arr("partners") {
			if i < 2 {
				pos = append(pos, proto.Message{"pos": i + 1, "id": h.Int("partner_id")})
			}
		}
		b.step("11212 save formation", 11212, proto.Message{"type": 0, "formation_type": 1, "pos_info": pos, "hallows_id": 0}, func(r proto.Message) error { return want(r.Int("code") == 1, "code 1") })
		b.step("11211 formation again", 11211, proto.Message{"type": 0}, func(r proto.Message) error { return want(len(r.Arr("pos_info")) == 2, "the saved two heroes") })
	} else {
		fmt.Println("skip  free summon already used today")
	}
	b.step("10406 take the login quest", 10406, proto.Message{"id": 90000}, func(r proto.Message) error {
		return want(r.Int("flag") == 1 || strings.Contains(r.Str("msg"), "已领取") || strings.Contains(r.Str("msg"), "未完成"), "a claim or a clear refusal")
	})
	b.step("13017 hook income", 13017, proto.Message{}, nil)
	b.step("13006 quick battle state", 13006, proto.Message{}, nil)
	b.step("10400 daily quests", 10400, proto.Message{}, func(r proto.Message) error { return want(len(r.Arr("quest_list")) == 12, "12 quests") })
	b.step("16400 achievements", 16400, proto.Message{}, func(r proto.Message) error { return want(len(r.Arr("feat_list")) > 100, "the achievement list") })
	b.step("23200 summon state", 23200, proto.Message{}, func(r proto.Message) error { return want(len(r.Arr("recruit_list")) == 4, "four summon groups") })
	b.step("10501 equipment bag", 10501, proto.Message{}, nil)
	b.step("12900 campaign ranking", 12900, proto.Message{"type": 2, "start": 1, "num": 10}, func(r proto.Message) error { return want(r.Int("my_idx") >= 1, "the role in the ranking") })
	b.step("1169 heartbeat", 1169, proto.Message{}, nil)

	cmds := make([]int, 0, len(b.seen))
	for cmd := range b.seen {
		cmds = append(cmds, int(cmd))
	}
	sort.Ints(cmds)
	fmt.Printf("\nframes received (command:count):")
	for _, cmd := range cmds {
		fmt.Printf(" %d:%d", cmd, b.seen[uint16(cmd)])
	}
	fmt.Println()
	if b.failed {
		os.Exit(1)
	}
	fmt.Println("all steps passed")
}

// playFight plays the fight that follows a 13003 the way the client does: it
// confirms the loading (20001), each round start (20019) and each round's attacks
// (20005), and ends with the result (13016).
func (b *bot) playFight() {
	if _, err := b.recv(20027); err != nil {
		b.fail("20027 fight start", err)
		return
	}
	if err := b.send(20001, proto.Message{}); err != nil {
		b.fail("20001 ready", err)
		return
	}
	if _, err := b.recv(20002); err != nil {
		b.fail("20002 round start", err)
		return
	}
	for round := 1; round <= 20; round++ {
		if err := b.send(20019, proto.Message{}); err != nil {
			b.fail("20019 round over", err)
			return
		}
		if _, err := b.recv(20004); err != nil {
			b.fail(fmt.Sprintf("20004 attacks of round %d", round), err)
			return
		}
		if err := b.send(20005, proto.Message{}); err != nil {
			b.fail("20005 played", err)
			return
		}
		// the last round is followed by the result, the others by the next round start
		if _, err := b.recvWithin(13016, 700*time.Millisecond); err == nil {
			fmt.Printf("ok    fight won after %d rounds, 13016 result\n", round)
			return
		}
	}
	b.fail("13016 result", fmt.Errorf("the fight never ended"))
}
