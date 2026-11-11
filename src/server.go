package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Device represents a network device that can be woken up
type Device struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	MAC  string `json:"mac"`
	IP   string `json:"ip"`
}

// DeviceStore provides thread-safe CRUD operations with JSON persistence
type DeviceStore struct {
	mu      sync.RWMutex
	devices []Device
	nextID  int
	file    string
}

func NewDeviceStore(filepath string) *DeviceStore {
	store := &DeviceStore{
		devices: make([]Device, 0),
		nextID:  1,
		file:    filepath,
	}
	store.load()
	return store
}

func (s *DeviceStore) load() {
	data, err := os.ReadFile(s.file)
	if err != nil {
		return // file doesn't exist yet
	}
	json.Unmarshal(data, &s.devices)
	for _, d := range s.devices {
		if d.ID >= s.nextID {
			s.nextID = d.ID + 1
		}
	}
}

func (s *DeviceStore) save() {
	data, _ := json.MarshalIndent(s.devices, "", "  ")
	os.WriteFile(s.file, data, 0644)
}

func (s *DeviceStore) List() []Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Device, len(s.devices))
	copy(result, s.devices)
	return result
}

func (s *DeviceStore) Get(id int) (Device, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, d := range s.devices {
		if d.ID == id {
			return d, true
		}
	}
	return Device{}, false
}

func (s *DeviceStore) Add(name, mac, ip string) Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := Device{ID: s.nextID, Name: name, MAC: mac, IP: ip}
	s.nextID++
	s.devices = append(s.devices, d)
	s.save()
	return d
}

func (s *DeviceStore) Update(id int, name, mac, ip string) (Device, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, d := range s.devices {
		if d.ID == id {
			s.devices[i].Name = name
			s.devices[i].MAC = mac
			s.devices[i].IP = ip
			s.save()
			return s.devices[i], true
		}
	}
	return Device{}, false
}

func (s *DeviceStore) Delete(id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, d := range s.devices {
		if d.ID == id {
			s.devices = append(s.devices[:i], s.devices[i+1:]...)
			s.save()
			return true
		}
	}
	return false
}

// sendWOL sends a Wake-on-LAN magic packet to the given MAC address
func sendWOL(mac, ip string) error {
	// Parse MAC address
	hwAddr, err := net.ParseMAC(mac)
	if err != nil {
		return fmt.Errorf("invalid MAC address: %v", err)
	}

	// Build magic packet: 6 bytes of 0xFF + 16 repetitions of MAC
	packet := make([]byte, 102)
	for i := 0; i < 6; i++ {
		packet[i] = 0xFF
	}
	for i := 0; i < 16; i++ {
		copy(packet[6+i*6:], hwAddr)
	}

	// Send via UDP broadcast
	addr := ip + ":9"
	if ip == "" {
		addr = "255.255.255.255:9"
	}
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return fmt.Errorf("dial error: %v", err)
	}
	defer conn.Close()

	_, err = conn.Write(packet)
	if err != nil {
		return fmt.Errorf("write error: %v", err)
	}
	return nil
}

var store = NewDeviceStore("devices.json")

func main() {
	mux := http.NewServeMux()

	// API: List all devices
	mux.HandleFunc("/api/devices", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(store.List())
			return
		}
		if r.Method == http.MethodPost {
			var req struct {
				Name string `json:"name"`
				MAC  string `json:"mac"`
				IP   string `json:"ip"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			device := store.Add(req.Name, req.MAC, req.IP)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(device)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	// API: Get/Update/Delete a specific device
	mux.HandleFunc("/api/devices/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Extract ID from URL path
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/devices/"), "/")
		id, err := strconv.Atoi(parts[0])
		if err != nil {
			http.Error(w, "invalid device ID", http.StatusBadRequest)
			return
		}

		switch r.Method {
		case http.MethodGet:
			device, ok := store.Get(id)
			if !ok {
				http.Error(w, "device not found", http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(device)

		case http.MethodPut:
			var req struct {
				Name string `json:"name"`
				MAC  string `json:"mac"`
				IP   string `json:"ip"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			device, ok := store.Update(id, req.Name, req.MAC, req.IP)
			if !ok {
				http.Error(w, "device not found", http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(device)

		case http.MethodDelete:
			if ok := store.Delete(id); !ok {
				http.Error(w, "device not found", http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// API: Wake a device by ID
	mux.HandleFunc("/api/wake", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID int `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		device, ok := store.Get(req.ID)
		if !ok {
			http.Error(w, "device not found", http.StatusNotFound)
			return
		}
		if err := sendWOL(device.MAC, device.IP); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "WOL packet sent to " + device.Name})
	})

	// Serve web UI
	mux.HandleFunc("/wol", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wol" {
			http.Redirect(w, r, "/wol", http.StatusFound)
			return
		}
		http.ServeFile(w, r, "index.html")
	})

	log.Println("WOL Server starting on :8091")
	log.Fatal(http.ListenAndServe(":8091", mux))
}
