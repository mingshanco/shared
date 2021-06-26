package wsserver

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/tal-tech/go-zero/core/logx"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024 * 1024,
	WriteBufferSize: 1024 * 1024,
}

func Handler(s *Server) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		uid, err := r.Context().Value("uid").(json.Number).Int64()
		if err != nil {
			logx.Errorf("bad token")
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		s.addConn(uid, conn)

	}
}

type Server struct {
	// Registered clients.
	clients map[*Client]bool

	// Inbound messages from the clients.
	broadcast chan []byte

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client

	producers chan *Client
}

func NewServer() *Server {

	s := &Server{
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
		producers:  make(chan *Client, 20480),
	}
	go s.run()
	return s
}

func (s *Server) addConn(uid int64, conn *websocket.Conn) {
	client := newClient(uid, s, conn)
	s.register <- client
}

func (s *Server) Broadcast(message []byte) {
	s.broadcast <- message
}

func (s *Server) GetProduce() *Client {
	return <-s.producers
}

func (s *Server) run() {

	for {
		select {
		case client := <-s.register:
			s.clients[client] = true
		case client := <-s.unregister:
			if _, ok := s.clients[client]; ok {
				delete(s.clients, client)
				close(client.read)
				close(client.send)
			}
		case message := <-s.broadcast:
			for client := range s.clients {
				select {
				case client.send <- message:
				default:
					close(client.read)
					close(client.send)
					delete(s.clients, client)
				}
			}
		}
	}
}
