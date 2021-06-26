package wsserver

import (
	"bytes"
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
	maxMessageSize = 1024 * 1024

	// send buffer size
	bufSize = 256
)

var (
	newline = []byte{'\n'}
	//space   = []byte{' '}
)

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	uid int64

	s *Server

	// The websocket connection.
	conn *websocket.Conn

	read chan []byte

	// Buffered channel of outbound messages.
	send chan []byte
}

func newClient(uid int64, s *Server, conn *websocket.Conn) *Client {
	c := &Client{
		uid:  uid,
		s:    s,
		conn: conn,
		read: make(chan []byte, bufSize),
		send: make(chan []byte, bufSize),
	}

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go c.writePump()
	go c.readPump()
	return c
}

func (c *Client) UID() int64 {
	return c.uid
}

func (c *Client) Read() []byte {
	select {
	case buf := <-c.read:
		return buf
	default:
		return nil
	}
}

func (c *Client) Write(buf []byte) {
	select {
	case c.send <- buf:
	default:
		logx.Slowf("%d's send buf is full", c.uid)
	}
}

// readPump pumps messages from the websocket connection to the hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Client) readPump() {
	defer func() {
		c.s.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logx.Errorf("error: %v", err)
			}
			break
		}

		messages := bytes.Split(message, newline)
		//message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
		for i := range messages {
			select {
			case c.read <- messages[i]:
				select {
				case c.s.producers <- c:
				default:
					logx.Error("producers list is full")
				}
			default:
				logx.Error("read buf is full")
			}
		}

	}
}

// writePump pumps messages from the hub to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued chat messages to the current websocket message.
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write(newline)
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
