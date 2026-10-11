package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	server := flag.String("server", "http://127.0.0.1:8080", "server base URL")
	timeout := flag.Duration("timeout", 10*time.Second, "HTTP timeout")

	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}

	c := &http.Client{Timeout: *timeout}
	base := strings.TrimRight(*server, "/")
	var err error
	switch args[0] {
	case "create":
		err = create(c, base, args[1:])
	case "fact":
		err = fact(c, base, args[1:])
	case "relate":
		err = relate(c, base, args[1:])
	case "get":
		err = get(c, base, args[1:])
	case "query-ip":
		err = queryIP(c, base, args[1:])
	case "relationships":
		err = relationships(c, base, args[1:])
	case "health":
		err = request(c, "GET", base+"/health", nil)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `factdb-client [global flags] COMMAND
Global flags: -server URL -timeout duration
Commands:
  create --type TYPE --id ID
  fact ENTITY_ID KEY JSON_VALUE --source NAME [--verified]
  relate FROM_ID RELATIONSHIP TO_ID --source NAME [--id RELATIONSHIP_ID]
  relationships ENTITY_ID [--type TYPE] [--direction outgoing|incoming|both]
  get ENTITY_ID
  query-ip CIDR [--type TYPE] [--verified]
  health
Examples:
  factdb-client create --type VirtualMachine --id vm-01
  factdb-client fact vm-01 ipAddresses '["192.168.2.15"]' --source scanner --verified
  factdb-client relate vm-01 ownedBy team-platform --source inventory
  factdb-client relationships team-platform --type ownedBy --direction incoming`)
}

func create(c *http.Client, b string, a []string) error {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	typ := fs.String("type", "", "entity type")
	id := fs.String("id", "", "stable ID")
	if err := fs.Parse(a); err != nil {
		return err
	}
	if *typ == "" || *id == "" {
		return fmt.Errorf("create requires --type and --id")
	}
	return request(c, "POST", b+"/v1/entities", map[string]any{"id": *id, "type": *typ})
}

func fact(c *http.Client, b string, a []string) error {
	if len(a) < 3 {
		return fmt.Errorf("usage: fact ENTITY_ID KEY JSON_VALUE --source NAME [--verified]")
	}
	id, key, raw := a[0], a[1], a[2]
	fs := flag.NewFlagSet("fact", flag.ContinueOnError)
	source := fs.String("source", "cli", "fact source")
	verified := fs.Bool("verified", false, "mark fact verified")
	if err := fs.Parse(a[3:]); err != nil {
		return err
	}
	var v any
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		return fmt.Errorf("value must be valid JSON: %w", err)
	}
	return request(c, "PUT", b+"/v1/entities/"+url.PathEscape(id)+"/facts/"+url.PathEscape(key), map[string]any{"value": v, "source": *source, "verified": *verified})
}

func relate(c *http.Client, b string, a []string) error {
	if len(a) < 3 {
		return fmt.Errorf("usage: relate FROM_ID RELATIONSHIP TO_ID --source NAME [--id RELATIONSHIP_ID]")
	}
	from, typ, to := a[0], a[1], a[2]
	fs := flag.NewFlagSet("relate", flag.ContinueOnError)
	source := fs.String("source", "cli", "relationship source")
	id := fs.String("id", "", "relationship ID (generated if omitted)")
	if err := fs.Parse(a[3:]); err != nil {
		return err
	}
	return request(c, "POST", b+"/v1/relationships", map[string]any{"id": *id, "fromId": from, "type": typ, "toId": to, "source": *source})
}

func relationships(c *http.Client, b string, a []string) error {
	if len(a) < 1 {
		return fmt.Errorf("usage: relationships ENTITY_ID [--type TYPE] [--direction outgoing|incoming|both]")
	}

	id := a[0]
	fs := flag.NewFlagSet("relationships", flag.ContinueOnError)
	typ := fs.String("type", "", "relationship type filter")
	source := fs.String("source", "", "relationship source filter")
	direction := fs.String("direction", "both", "outgoing, incoming, or both")

	if err := fs.Parse(a[1:]); err != nil {
		return err
	}

	endpoint := b + "/v1/entities/" + url.PathEscape(id) + "/relationships?"
	q := url.Values{}

	if *typ != "" {
		q.Set("type", *typ)
	}

	if *source != "" {
		q.Set("source", *source)
	}

	if *direction != "" {
		q.Set("direction", *direction)
	}
	return request(c, "GET", endpoint+q.Encode(), nil)
}

func get(c *http.Client, b string, a []string) error {
	if len(a) != 1 {
		return fmt.Errorf("usage: get ENTITY_ID")
	}
	return request(c, "GET", b+"/v1/entities/"+url.PathEscape(a[0]), nil)
}

func queryIP(c *http.Client, b string, a []string) error {
	if len(a) < 1 {
		return fmt.Errorf("usage: query-ip CIDR [--type TYPE] [--verified]")
	}
	cidr := a[0]
	fs := flag.NewFlagSet("query-ip", flag.ContinueOnError)
	typ := fs.String("type", "", "entity type filter")
	verified := fs.Bool("verified", false, "require verified IP fact")
	if err := fs.Parse(a[1:]); err != nil {
		return err
	}
	return request(c, "POST", b+"/v1/query/ip-in-subnet", map[string]any{"cidr": cidr, "type": *typ, "requireVerified": *verified})
}

func request(c *http.Client, method, endpoint string, body any) error {
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, endpoint, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, data, "", "  ") == nil {
		fmt.Println(pretty.String())
	} else {
		fmt.Println(string(data))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server returned %s", resp.Status)
	}
	return nil
}
