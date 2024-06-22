// pkg/runtime/dashboard.go

package runtime

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sync"
	"time"
)

type Dashboard struct {
	engine         *Engine
	port           int
	clients        map[chan string]bool
	clientsMutex   sync.Mutex
	updateInterval time.Duration
}

// NewDashboard creates a new instance of the Dashboard struct.
// It takes an `engine` pointer and a `port` integer as parameters.
// It returns a pointer to the created Dashboard.
func NewDashboard(engine *Engine, port int, updateInterval time.Duration) *Dashboard {
	return &Dashboard{
		engine:         engine,
		port:           port,
		clients:        make(map[chan string]bool),
		updateInterval: updateInterval,
	}
}

// Start starts the dashboard server and initializes the necessary HTTP handlers.
func (d *Dashboard) Start() {
	http.HandleFunc("/", d.handleHome)
	http.HandleFunc("/api/stats", d.handleStats)
	http.HandleFunc("/events", d.handleSSE)

	go func() {
		addr := fmt.Sprintf(":%d", d.port)
		fmt.Printf("Dashboard starting on http://localhost%s\n", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			fmt.Printf("Dashboard error: %v\n", err)
		}
	}()

	go d.broadcastUpdates()
}

// handleHome handles the HTTP request for the home page of the dashboard.
// It renders the "home" template and writes the response to the given http.ResponseWriter.
func (d *Dashboard) handleHome(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.New("home").Parse(homeHTML))
	tmpl.Execute(w, nil)
}

// handleStats handles the HTTP request for retrieving and encoding the statistics of the dashboard.
func (d *Dashboard) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := d.engine.GetStats() // Implement this method in the Engine struct
	json.NewEncoder(w).Encode(stats)
}

// handleSSE handles the server-sent events (SSE) for the Dashboard.
// It sets the appropriate headers for SSE, adds the client to the list of active clients,
// and continuously sends messages to the client until the request is canceled.
// The function takes a http.ResponseWriter and a http.Request as parameters.
func (d *Dashboard) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	client := make(chan string)
	d.clientsMutex.Lock()
	d.clients[client] = true
	d.clientsMutex.Unlock()

	defer func() {
		d.clientsMutex.Lock()
		delete(d.clients, client)
		d.clientsMutex.Unlock()
	}()

	for {
		select {
		case message := <-client:
			fmt.Fprintf(w, "data: %s\n\n", message)
			w.(http.Flusher).Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// broadcastUpdates sends periodic updates to all connected clients.
func (d *Dashboard) broadcastUpdates() {
	ticker := time.NewTicker(d.updateInterval)
	defer ticker.Stop()

	for range ticker.C {
		stats := d.engine.GetStats()
		message, _ := json.Marshal(stats)

		d.clientsMutex.Lock()
		for client := range d.clients {
			select {
			case client <- string(message):
			default:
				// Client is not ready, skip this update
			}
		}
		d.clientsMutex.Unlock()
	}
}

const homeHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>REX Dashboard
	<img src="rex_logo_128.png" alt="Rex Logo" height="1.5em">
	</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 0; padding: 20px; background-color: #f0f0f0; }
        .container { max-width: 800px; margin: 0 auto; background-color: white; padding: 20px; border-radius: 5px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
        h1 { color: #333; }
        #stats { margin-top: 20px; }
        .stat-item { margin-bottom: 10px; }
        .stat-label { font-weight: bold; }
    </style>
</head>
<body>
    <div class="container">
        <h1>REX Dashboard</h1>
        <div id="stats"></div>
    </div>
    <script>
        const evtSource = new EventSource("/events");
        evtSource.onmessage = function(event) {
            const stats = JSON.parse(event.data);
            const statsDiv = document.getElementById("stats");
            statsDiv.innerHTML = "";
            for (const [key, value] of Object.entries(stats)) {
                statsDiv.innerHTML += '<div class="stat-item"><span class="stat-label">' + key + ':</span> ' + value + '</div>';
            }
        };
    </script>
</body>
</html>`
