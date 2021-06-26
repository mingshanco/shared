package wsclient

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tal-tech/go-zero/core/logx"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 5120

	bufSize = 512
)

var (
	newline = []byte{'\n'}
	//space   = []byte{' '}
)

type WSClient struct {
	sync.WaitGroup
	logx.Logger
	addr      string
	token     string
	read      chan []byte
	send      chan []byte
	conn      *websocket.Conn
	connected bool
}

func NewWSClient(addr, token string) *WSClient {
	client := &WSClient{
		addr:   addr,
		token:  token,
		read:   make(chan []byte, bufSize),
		send:   make(chan []byte, bufSize),
		Logger: logx.WithContext(context.Background()),
	}

	go client.run()

	return client
}

func (w *WSClient) Read() []byte {
	return <-w.read
}

func (w *WSClient) Write(buf []byte) {
	if !w.connected {
		return
	}
	select {
	case w.send <- buf:
	default:
		w.Slow("send buf is full")
	}

}

func (w *WSClient) run() {
	header := make(http.Header)
	if w.token != "" {
		header.Add("authorization", w.token)
	}

	for {
		conn, _, err := websocket.DefaultDialer.Dial(w.addr, header)
		if err != nil {
			time.Sleep(3 * time.Second)
			w.Errorf("connect to ws server failed: %s", err)
			continue
		}
		w.conn = conn
		w.connected = true
		w.Add(2)
		go w.readPump()
		go w.writePump()
		w.Wait()
		w.connected = false
	}
}

func (w *WSClient) readPump() {
	defer func() {
		w.conn.Close()
		w.Done()
	}()
	w.conn.SetReadLimit(maxMessageSize)
	w.conn.SetReadDeadline(time.Now().Add(pongWait))
	w.conn.SetPongHandler(func(string) error { w.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, message, err := w.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
		messages := bytes.Split(message, newline)
		//message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
		for i := range messages {
			select {
			case w.read <- messages[i]:
			default:
				w.Slow("read buf is full")
			}
		}

	}
}

func (w *WSClient) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		w.conn.Close()
		w.Done()
	}()

	for {
		select {
		case message, ok := <-w.send:
			w.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				w.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			nw, err := w.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			nw.Write(message)

			// Add queued chat messages to the current websocket message.
			n := len(w.send)
			for i := 0; i < n; i++ {
				nw.Write(newline)
				nw.Write(<-w.send)
			}

			if err := nw.Close(); err != nil {
				return
			}
		case <-ticker.C:
			w.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := w.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
