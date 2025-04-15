package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Constants for simulation
const (
	G           = 0.0001  // Gravitational constant (tuned for simulation)
	StarMass    = 1000000 // Mass of central star
	MinDistance = 10      // Minimum distance from star for initial position
	MaxDistance = 100     // Maximum distance for initial position
	TimeStep    = 0.016   // Simulation step (approx 60 FPS)
)

// Entity represents a client's state
type Entity struct {
	ID        string
	Position  Vector2
	Velocity  Vector2
	Connected bool
}

// Vector2 for 2D coordinates
type Vector2 struct {
	X, Y float64
}

// ClientUpdate is sent to clients
type ClientUpdate struct {
	Entities []Entity `json:"entities"`
}

// Global state
var (
	clients   = make(map[*websocket.Conn]Entity)
	clientsMu sync.Mutex
	upgrader  = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
)

// Generate random position
func randomPosition() Vector2 {
	// Polar coordinates for even distribution
	theta := rand.Float64() * 2 * math.Pi
	r := MinDistance + rand.Float64()*(MaxDistance-MinDistance)
	x := r * math.Cos(theta)
	y := r * math.Sin(theta)
	return Vector2{X: x, Y: y}
}

// Calculate gravitational acceleration
func gravitationalAccel(pos Vector2) Vector2 {
	r := math.Sqrt(pos.X*pos.X + pos.Y*pos.Y)
	if r < 0.1 { // Prevent division by zero
		r = 0.1
	}
	force := -G * StarMass / (r * r)
	unitX, unitY := pos.X/r, pos.Y/r
	return Vector2{
		X: force * unitX,
		Y: force * unitY,
	}
}

// WebSocket handler
func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}

	// Assign random position and unique ID
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	entity := Entity{
		ID:        id,
		Position:  randomPosition(),
		Velocity:  Vector2{}, // Start with zero velocity
		Connected: true,
	}

	// Register client
	clientsMu.Lock()
	clients[conn] = entity
	clientsMu.Unlock()

	defer func() {
		// Unregister client
		clientsMu.Lock()
		entity.Connected = false
		clients[conn] = entity
		delete(clients, conn)
		clientsMu.Unlock()
		conn.Close()
	}()

	// Handle incoming messages (optional)
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			log.Println("Read error:", err)
			break
		}
	}
}

// Broadcast updates to all clients
func broadcastUpdates() {
	ticker := time.NewTicker(time.Second / 60)
	defer ticker.Stop()

	for range ticker.C {
		clientsMu.Lock()

		// Update physics
		for conn, entity := range clients {
			if !entity.Connected {
				continue
			}
			// Calculate acceleration due to gravity
			accel := gravitationalAccel(entity.Position)
			// Update velocity
			entity.Velocity.X += accel.X * TimeStep
			entity.Velocity.Y += accel.Y * TimeStep
			// Update position
			entity.Position.X += entity.Velocity.X * TimeStep
			entity.Position.Y += entity.Velocity.Y * TimeStep
			// Store updated entity
			clients[conn] = entity
		}

		// Prepare update
		var entities []Entity
		for _, entity := range clients {
			entities = append(entities, entity)
		}
		update := ClientUpdate{Entities: entities}
		data, err := json.Marshal(update)
		if err != nil {
			log.Println("JSON error:", err)
			clientsMu.Unlock()
			continue
		}

		// Broadcast to all connected clients
		for conn, entity := range clients {
			if !entity.Connected {
				continue
			}
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Println("Write error:", err)
				entity.Connected = false
				clients[conn] = entity
			}
		}
		clientsMu.Unlock()
	}
}

func main() {
	// Seed random number generator
	rand.Seed(time.Now().UnixNano())

	// Start physics and broadcast loop
	go broadcastUpdates()

	// Set up WebSocket endpoint
	http.HandleFunc("/ws", wsHandler)

	// Serve a simple HTTP page for testing
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `
			<!DOCTYPE html>
			<html>
			<head><title>2D Gravitational Simulation</title></head>
			<body>
				<h1>WebSocket 2D Gravitational Simulation</h1>
				<canvas id="canvas" width="800" height="600"></canvas>
				<script>
					const ws = new WebSocket("ws://" + window.location.host + "/ws");
					const canvas = document.getElementById("canvas");
					const ctx = canvas.getContext("2d");

					ws.onopen = () => console.log("Connected to server");
					ws.onclose = () => console.log("Disconnected");
					ws.onmessage = (e) => {
						const data = JSON.parse(e.data);
						// console.log(data);
						ctx.clearRect(0, 0, canvas.width, canvas.height);
						// Draw star at center
						ctx.fillStyle = "red";
						ctx.beginPath();
						ctx.arc(canvas.width/2, canvas.height/2, 10, 0, 2*Math.PI);
						ctx.fill();
						// Draw entities
						data.entities.forEach(entity => {
							// The connected entity check isn't working as intended, so commented out for now
							// if (entity.connected != true) return;
							const x = canvas.width/2 + entity.Position.X;
							const y = canvas.height/2 + entity.Position.Y;
							ctx.fillStyle = "blue";
							ctx.beginPath();
							ctx.arc(x, y, 5, 0, 2*Math.PI);
							ctx.fill();
						});
					};
				</script>
			</body>
			</html>
		`)
	})

	// Start server
	log.Println("Server starting on :8080...")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}
