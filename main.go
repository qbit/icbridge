package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"suah.dev/icb"
	"suah.dev/protect"
)

//go:embed _conf.yaml
var yamlTemplate string

type (
	config map[string]string

	// Content maps our content for an event
	Content map[string]interface{}
)

func (c config) get(key string) string {
	if value, ok := c[key]; ok {
		return value
	}
	return ""
}

func (c config) load(file string) error {
	cf, err := os.Open(file)
	if err != nil {
		return err
	}
	defer cf.Close()

	scanner := bufio.NewScanner(cf)

	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "=", 2)
		if len(parts) != 2 {
			continue
		}

		k := strings.Trim(parts[0], " ")
		v := strings.Trim(parts[1], " ")

		c[k] = v
	}

	return nil
}

// Event represents a matrix event
type Event struct {
	EventID   string  `json:"event_id"`
	EventType string  `json:"type"`
	Content   Content `json:"content"`
	RoomID    string  `json:"room_id"`
	UserID    string  `json:"user_id"`
}

// Events are a set of matrix events
type Events struct {
	Events []Event `json:"events"`
}

// HSToken is a token given to us by the homeserver after we register as an
// application service.
type HSToken struct {
	Token string `json:"hs_token"`
}

func makeYaml(c config) {
	fmt.Printf(yamlTemplate,
		c.get("bridge.id"),
		c.get("bridge.url"),
		c.get("bridge.as_token"),
		c.get("bridge.hs_token"))

}

func main() {
	confPath := flag.String("conf", "./config.maml", "Path to configuration file")
	genYaml := flag.Bool("genyaml", false, "Generate yaml file for homeserver consumption")
	flag.Parse()

	_ = protect.Unveil(*confPath, "r")
	_ = protect.Unveil("/etc/resolv.conf", "r")
	_ = protect.UnveilBlock()

	var c icb.Client
	var conf config
	var err error

	conf = make(map[string]string)

	conf.load(*confPath)
	server := conf.get("icb.server")
	nick := conf.get("icb.nick")
	group := conf.get("icb.group")
	port := conf.get("icb.port")

	if *genYaml {
		makeYaml(conf)
		os.Exit(0)
	}

	fmt.Println(server)
	err = c.Connect(fmt.Sprintf("%s:%s", server, port))
	if err != nil {
		log.Fatal(err)
	}

	defer c.Conn.Close()

	c.Handlers = map[string]interface{}{
		"a": func(s []string, c *icb.Client) {
		},
		"b": func(s []string, c *icb.Client) {
			// Message received
			log.Printf("%s> %s", s[1], strings.Join(s[2:], " "))
		},
		"c": func(s []string, c *icb.Client) {
			// Personal message received
			fmt.Printf("private msg from: %s> %s\n", s[1], strings.Join(s[2:], " "))
		},
		"d": func(s []string, c *icb.Client) {
			// Status message
			fmt.Println("->", strings.Join(s[1:], " "))
		},
		"e": func(s []string, c *icb.Client) {
			// Error message
			fmt.Println("ERROR>", strings.Join(s[1:], " "))
		},
		"j": func(s []string, c *icb.Client) {
			fmt.Printf("-> Connected to %s (%s)\n", s[2], s[3])
		},
		"k": func(s []string, c *icb.Client) {
			fmt.Println("-> BEEP")
		},
		"l": func(s []string, c *icb.Client) {
			c.Write([]string{"m"})
		},
		"n": func(s []string, c *icb.Client) {
		},
	}

	go func() {
		for {
			p, err := c.Read()
			if err != nil {
				log.Fatal("reading: ", err)
			}

			a, err := p.Decode()
			if err != nil {
				log.Printf("error decoding: %q", p.Buffer)
				continue
			}

			c.RunHandlers(*a)
		}
	}()

	log.Printf("joining: %q\n", group)
	c.Write([]string{"a", nick, nick, group, "login"})
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/transactions/", func(w http.ResponseWriter, r *http.Request) {
		buf, _ := ioutil.ReadAll(r.Body)
		var events Events
		json.Unmarshal(buf, &events)
		for _, e := range events.Events {
			if strings.HasPrefix(e.UserID, "@icb.") {
				return
			}
			switch e.EventType {
			case "m.room.message":
				err = c.Write([]string{"b", e.Content["body"].(string)})
				if err != nil {
					log.Println(err)
				}
			default:
				fmt.Println(e.EventType, "unknown event type")
			}
		}
	})

	log.Fatal(http.ListenAndServe(":9000", nil))
}
