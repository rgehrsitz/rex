// pkg\runtime\dashboard.go

package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Dashboard struct {
	engine         *Engine
	port           int
	clients        map[*websocket.Conn]bool
	clientsMutex   sync.Mutex
	updateInterval time.Duration
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for now. In production, this should be more restrictive.
	},
}

func NewDashboard(engine *Engine, port int, updateInterval time.Duration) *Dashboard {
	return &Dashboard{
		engine:         engine,
		port:           port,
		clients:        make(map[*websocket.Conn]bool),
		updateInterval: updateInterval,
	}
}

func (d *Dashboard) Start() {
	// Serve static files
	buildPath := "../../pkg/runtime/dashboard/build"
	fs := http.FileServer(http.Dir(buildPath))
	http.Handle("/dashboard/", http.StripPrefix("/dashboard/", fs))

	// Add a check to see if the directory exists
	if _, err := os.Stat(buildPath); os.IsNotExist(err) {
		fmt.Printf("Error: Build directory does not exist: %s\n", buildPath)
		return
	}

	// Add a simple handler to check if the server is running
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Server is running")
	})

	// WebSocket handler
	http.HandleFunc("/events", d.handleWebSocket)

	go d.broadcastUpdates()

	addr := fmt.Sprintf(":%d", d.port)
	fmt.Printf("Dashboard starting on http://localhost%s/dashboard/\n", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		fmt.Printf("Dashboard error: %v\n", err)
	}
}

func (d *Dashboard) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Printf("Error upgrading to WebSocket: %v\n", err)
		return
	}
	defer conn.Close()

	fmt.Printf("Client connected: %v\n", conn.RemoteAddr()) // Add this line for debugging

	d.clientsMutex.Lock()
	d.clients[conn] = true
	d.clientsMutex.Unlock()

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}

	d.clientsMutex.Lock()
	delete(d.clients, conn)
	d.clientsMutex.Unlock()

	fmt.Printf("Client disconnected: %v\n", conn.RemoteAddr()) // Add this line for debugging
}

func (d *Dashboard) broadcastUpdates() {
	ticker := time.NewTicker(d.updateInterval)
	defer ticker.Stop()

	for range ticker.C {
		stats := d.engine.GetStats()
		message, err := json.Marshal(stats)
		if err != nil {
			fmt.Printf("Error marshaling stats: %v\n", err)
			continue
		}

		fmt.Printf("Broadcasting update: %s\n", string(message)) // Add this log

		d.clientsMutex.Lock()
		for client := range d.clients {
			err := client.WriteMessage(websocket.TextMessage, message)
			if err != nil {
				fmt.Printf("Error sending message to client: %v\n", err)
				client.Close()
				delete(d.clients, client)
			}
		}
		d.clientsMutex.Unlock()
	}
}
