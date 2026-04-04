// internal/api/websocket.go
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/stahnma/therm-pro/internal/cook"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type wsClient struct {
	conn *websocket.Conn
	send chan []byte
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}

	client := &wsClient{
		conn: conn,
		send: make(chan []byte, 64),
	}

	s.wsMu.Lock()
	s.wsClients[client] = true
	s.wsMu.Unlock()

	go client.writePump(s)
}

func (c *wsClient) writePump(s *Server) {
	defer func() {
		s.wsMu.Lock()
		delete(s.wsClients, c)
		s.wsMu.Unlock()
		c.conn.Close()
	}()

	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}

func (s *Server) broadcast(reading cook.Reading) {
	data, err := json.Marshal(reading)
	if err != nil {
		return
	}

	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	for client := range s.wsClients {
		select {
		case client.send <- data:
		default:
			close(client.send)
			delete(s.wsClients, client)
		}
	}
}
