package main

import (
	"encoding/base64"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ---------- WebSocket upgrader ----------

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow connections from any origin
	},
}

// ---------- UDP Video Hub ----------

type VideoHub struct {
	clients    map[*VideoClient]bool
	broadcast  chan []byte
	register   chan *VideoClient
	unregister chan *VideoClient
	mu         sync.RWMutex
}

type VideoClient struct {
	conn *websocket.Conn
	send chan []byte
}

var videoHub *VideoHub

func newVideoHub() *VideoHub {
	return &VideoHub{
		clients:    make(map[*VideoClient]bool),
		broadcast:  make(chan []byte, 10),
		register:   make(chan *VideoClient),
		unregister: make(chan *VideoClient),
	}
}

func (vh *VideoHub) run() {
	for {
		select {
		case client := <-vh.register:
			vh.mu.Lock()
			vh.clients[client] = true
			vh.mu.Unlock()
			log.Printf("Video client connected. Total video clients: %d", len(vh.clients))

		case client := <-vh.unregister:
			vh.mu.Lock()
			if _, ok := vh.clients[client]; ok {
				delete(vh.clients, client)
				close(client.send)
				log.Printf("Video client disconnected. Total video clients: %d", len(vh.clients))
			}
			vh.mu.Unlock()

		case message := <-vh.broadcast:
			vh.mu.RLock()
			for client := range vh.clients {
				select {
				case client.send <- message:
				default:
					// Client buffer full, skip this frame
				}
			}
			vh.mu.RUnlock()
		}
	}
}

// UDP video receiver - receives JPEG frames from UDP and broadcasts to WebSocket clients
func startUDPVideoReceiver(port int) {
	addr := net.UDPAddr{
		Port: port,
		IP:   net.ParseIP("0.0.0.0"),
	}
	
	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		log.Printf("Failed to start UDP video receiver on port %d: %v", port, err)
		return
	}
	defer conn.Close()
	
	log.Printf("UDP video receiver listening on port %d", port)
	
	buffer := make([]byte, 65535)
	frameBuffer := make(map[uint32]map[uint16][]byte)
	lastCleanup := time.Now()
	
	for {
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			log.Printf("UDP video read error: %v", err)
			continue
		}
		
		if n < 8 {
			continue
		}
		
		// Parse header: size (4 bytes), chunk_id (2 bytes), total_chunks (2 bytes)
		dataSize := uint32(buffer[0])<<24 | uint32(buffer[1])<<16 | uint32(buffer[2])<<8 | uint32(buffer[3])
		chunkID := uint16(buffer[4])<<8 | uint16(buffer[5])
		totalChunks := uint16(buffer[6])<<8 | uint16(buffer[7])
		
		payload := buffer[8:n]
		
		// Single packet frame
		if totalChunks == 1 {
			// Encode to base64 and broadcast
			encoded := base64.StdEncoding.EncodeToString(payload)
			videoHub.broadcast <- []byte(encoded)
			continue
		}
		
		// Multi-packet frame reassembly
		frameID := dataSize // Use data size as frame ID
		
		if frameBuffer[frameID] == nil {
			frameBuffer[frameID] = make(map[uint16][]byte)
		}
		
		frameBuffer[frameID][chunkID] = make([]byte, len(payload))
		copy(frameBuffer[frameID][chunkID], payload)
		
		// Check if we have all chunks
		if len(frameBuffer[frameID]) == int(totalChunks) {
			// Reassemble frame
			fullFrame := make([]byte, 0, dataSize)
			for i := uint16(1); i <= totalChunks; i++ {
				if chunk, ok := frameBuffer[frameID][i]; ok {
					fullFrame = append(fullFrame, chunk...)
				}
			}
			
			// Clean up buffer
			delete(frameBuffer, frameID)
			
			// Encode and broadcast
			encoded := base64.StdEncoding.EncodeToString(fullFrame)
			videoHub.broadcast <- []byte(encoded)
		}
		
		// Periodic cleanup of old incomplete frames (every 2 seconds)
		if time.Since(lastCleanup) > 2*time.Second {
			if len(frameBuffer) > 10 {
				// Clear all incomplete frames
				frameBuffer = make(map[uint32]map[uint16][]byte)
			}
			lastCleanup = time.Now()
		}
	}
}

// WebSocket video handler
func handleVideoWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Video WebSocket upgrade failed: %v", err)
		return
	}
	
	client := &VideoClient{
		conn: conn,
		send: make(chan []byte, 5), // Small buffer for video frames
	}
	
	videoHub.register <- client
	
	go client.videoWritePump()
	go client.videoReadPump()
}

func (c *VideoClient) videoReadPump() {
	defer func() {
		videoHub.unregister <- c
		c.conn.Close()
	}()
	
	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (c *VideoClient) videoWritePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			
			// Send video frame as text (base64)
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
			
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func newHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			log.Printf("Client connected. Total clients: %d", len(h.clients))

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				log.Printf("Client disconnected. Total clients: %d", len(h.clients))
			}

		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	client := &Client{
		conn: conn,
		send: make(chan []byte, 256),
	}

	hub.register <- client

	go client.writePump()
	go client.readPump()
}

func (c *Client) readPump() {
	defer func() {
		hub.unregister <- c
		c.conn.Close()
		log.Printf("WebSocket client disconnected")
	}()

	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(30 * time.Second)) // 더 짧은 타임아웃
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		return nil
	})

	// 연결 끊김을 즉시 감지하기 위한 클로즈 핸들러
	c.conn.SetCloseHandler(func(code int, text string) error {
		log.Printf("WebSocket close: code=%d, text=%s", code, text)
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure) {
				log.Printf("WebSocket unexpected error: %v", err)
			}
			break
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(25 * time.Second) // 더 빈번한 핑
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second)) // 더 짧은 타임아웃
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// 대기 중인 메시지들 일괄 처리
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
