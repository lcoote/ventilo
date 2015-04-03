package main

import (
    "flag"
    "log"
    "net/http"
    "strings"
    "sync"
    "time"

    "github.com/gorilla/websocket"
)

var httpAddr = flag.String("http", ":8080", "HTTP listen address")

func main() {
    flag.Parse()
    http.Handle("/", NewServer())
    log.Fatal(http.ListenAndServe(*httpAddr, nil))
}

type Server struct {
    mu  sync.Mutex
    m   map[string][]chan string
}

func NewServer() *Server {
    return &Server{m: make(map[string][]chan string)}
}

var upgrader = websocket.Upgrader{
    ReadBufferSize:  1024,
    WriteBufferSize: 1024,
    CheckOrigin:     func(r *http.Request) bool { return true },
}

const (
    broadcast = "/broadcast/"
    listen    = "/listen/"
)

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    p := r.URL.Path
    switch {
    default:
        http.NotFound(w, r)

    case strings.HasPrefix(p, broadcast):
        p = strings.TrimPrefix(p, broadcast)
        s.broadcast(p, r.FormValue("message"))

    case strings.HasPrefix(p, listen):
        conn, err := upgrader.Upgrade(w, r, nil)
        if err != nil {
            log.Print(err)
            return
        }

        p = strings.TrimPrefix(r.URL.Path, listen)
        c := s.listen(p)
        defer s.hangup(p, c)

        for m := range c {
            err := conn.WriteMessage(websocket.TextMessage, []byte(m))
            if err != nil {
                log.Print(err)
                return
            }
        }
    }
}

func (s *Server) listen(p string) <-chan string {
    c := make(chan string)
    s.mu.Lock()
    s.m[p] = append(s.m[p], c)
    s.mu.Unlock()
    return c
}

func (s *Server) hangup(p string, c <-chan string) {
    // Remove channel from listener map.
    s.mu.Lock()
    ls := s.m[p]
    for i := range ls {
        if ls[i] == c {
            ls = append(ls[:i], ls[i+1:]...)
            break
        }
    }
    s.m[p] = ls
    s.mu.Unlock()

    // Drain channel for a minute, to unblock any in-flight senders.
    go func() {
        timeout := time.After(1 * time.Minute)
        for {
            select {
            case <-c:
            case <-timeout:
                return
            }
        }
    }()
}

func (s *Server) broadcast(p, m string) {
    s.mu.Lock()
    ls := append([]chan string{}, s.m[p]...) // copy
    s.mu.Unlock()
    for _, c := range ls {
        c <- m
    }
}
