package main

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// validateMAC checks if the MAC address is in valid format XX:XX:XX:XX:XX:XX
func validateMAC(mac string) bool {
	_, err := net.ParseMAC(mac)
	return err == nil
}

// validateIP checks if the IP is valid IPv4 format (empty is allowed)
func validateIP(ip string) bool {
	if ip == "" {
		return true
	}
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.To4() != nil
}

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

func jsonError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// checkDeviceStatus probes common TCP ports to determine if a device is reachable
func checkDeviceStatus(ip string) string {
	if ip == "" {
		return "no_ip"
	}
	ports := []string{"80", "443", "22", "445", "3389"}
	hasTimeout := false
	for _, port := range ports {
		conn, err := net.DialTimeout("tcp", ip+":"+port, 800*time.Millisecond)
		if err == nil {
			conn.Close()
			return "online"
		}
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			hasTimeout = true
		}
	}
	if hasTimeout {
		return "unknown"
	}
	return "offline"
}

var store = NewDeviceStore("devices.json")

// ---------------------------------------------------------------------------
// Xiaomi Router integration
// ---------------------------------------------------------------------------

// RouterConfig holds the connection settings for the Xiaomi router
type RouterConfig struct {
	Address  string `json:"address"`
	Password string `json:"password"`
}

// RouterStore persists router configuration
type RouterStore struct {
	mu     sync.RWMutex
	config RouterConfig
	file   string
}

func NewRouterStore(filepath string) *RouterStore {
	s := &RouterStore{
		config: RouterConfig{Address: "192.168.31.1"},
		file:   filepath,
	}
	s.load()
	return s
}

func (s *RouterStore) load() {
	data, err := os.ReadFile(s.file)
	if err != nil {
		return
	}
	json.Unmarshal(data, &s.config)
}

func (s *RouterStore) Get() RouterConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

func (s *RouterStore) Set(cfg RouterConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = cfg
	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(s.file, data, 0644)
}

// routerLogin authenticates with the Xiaomi router and returns a stok token.
func routerLogin(address, password string) (string, error) {
	// Step 1: fetch the login page to extract key and deviceId
	baseURL := "http://" + address + "/cgi-bin/luci/web"
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(baseURL)
	if err != nil {
		return "", fmt.Errorf("无法连接路由器 (%s): %v", address, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	keyRe := regexp.MustCompile(`key:\s*'([^']+)'`)
	keyMatch := keyRe.FindStringSubmatch(bodyStr)
	if keyMatch == nil {
		return "", fmt.Errorf("无法从路由器页面提取登录 key")
	}
	key := keyMatch[1]

	deviceIDRe := regexp.MustCompile(`deviceId\s*=\s*"([^"]+)"`)
	deviceIDMatch := deviceIDRe.FindStringSubmatch(bodyStr)
	deviceID := ""
	if deviceIDMatch != nil {
		deviceID = deviceIDMatch[1]
	}

	// Step 2: detect encryption mode and encrypt password
	newEncryptMode := strings.Contains(bodyStr, "newEncryptMode")

	nonce := fmt.Sprintf("%d_%s_%d_%d",
		0, deviceID,
		time.Now().UnixMilli(), rand.Intn(10000))

	var encryptedPwd string
	if newEncryptMode {
		inner := sha256.Sum256([]byte(password + key))
		outer := sha256.Sum256([]byte(nonce + hex.EncodeToString(inner[:])))
		encryptedPwd = hex.EncodeToString(outer[:])
	} else {
		h := sha1.Sum([]byte(password + key))
		encryptedPwd = hex.EncodeToString(h[:])
	}

	// Step 3: POST login
	loginURL := "http://" + address + "/cgi-bin/luci/api/xqsystem/login"
	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", encryptedPwd)
	form.Set("logtype", "2")
	form.Set("nonce", nonce)

	loginResp, err := client.PostForm(loginURL, form)
	if err != nil {
		return "", fmt.Errorf("登录请求失败: %v", err)
	}
	defer loginResp.Body.Close()

	var loginResult map[string]interface{}
	json.NewDecoder(loginResp.Body).Decode(&loginResult)

	if code, ok := loginResult["code"].(float64); !ok || code != 0 {
		msg := "登录失败"
		if m, ok := loginResult["msg"].(string); ok {
			msg = m
		}
		return "", fmt.Errorf("路由器拒绝登录: %s", msg)
	}

	stok, _ := loginResult["token"].(string)
	if stok == "" {
		return "", fmt.Errorf("登录成功但未返回 stok")
	}
	return stok, nil
}

// routerGetDeviceList fetches the list of connected devices from the router.
func routerGetDeviceList(address, stok string) ([]Device, error) {
	listURL := fmt.Sprintf("http://%s/cgi-bin/luci/;stok=%s/api/misystem/devicelist", address, stok)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(listURL)
	if err != nil {
		return nil, fmt.Errorf("获取设备列表失败: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析设备列表响应失败: %v", err)
	}

	listRaw, ok := result["list"]
	if !ok {
		return nil, fmt.Errorf("响应中无 list 字段")
	}
	listArr, ok := listRaw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("list 字段不是数组")
	}

	var devices []Device
	for _, item := range listArr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		mac, _ := m["mac"].(string)
		name, _ := m["name"].(string)
		if name == "" {
			name, _ = m["hostname"].(string)
		}
		ip := ""
		if ipList, ok := m["ip"].([]interface{}); ok && len(ipList) > 0 {
			if ipMap, ok := ipList[0].(map[string]interface{}); ok {
				ip, _ = ipMap["ip"].(string)
			}
		}
		if mac == "" {
			continue
		}
		mac = strings.ToUpper(mac)
		devices = append(devices, Device{Name: name, MAC: mac, IP: ip})
	}
	return devices, nil
}

// DiscoverDevices logs into the router and returns all connected devices.
func DiscoverDevices(address, password string) ([]Device, error) {
	stok, err := routerLogin(address, password)
	if err != nil {
		return nil, err
	}
	return routerGetDeviceList(address, stok)
}

func main() {
	mux := http.NewServeMux()
	routerStore := NewRouterStore("router.json")

	// API: Get/Set router configuration
	mux.HandleFunc("/api/router/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(routerStore.Get())
		case http.MethodPost:
			var cfg RouterConfig
			if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
				jsonError(w, err.Error(), http.StatusBadRequest)
				return
			}
			if cfg.Address == "" {
				cfg.Address = "192.168.31.1"
			}
			routerStore.Set(cfg)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		default:
			jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// API: Discover devices from Xiaomi router
	mux.HandleFunc("/api/router/discover", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		cfg := routerStore.Get()
		if cfg.Password == "" {
			jsonError(w, "请先配置路由器密码", http.StatusBadRequest)
			return
		}
		devices, err := DiscoverDevices(cfg.Address, cfg.Password)
		if err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(devices)
	})

	// API: Batch import discovered devices into the WOL store
	mux.HandleFunc("/api/router/import", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Devices []struct {
				Name string `json:"name"`
				MAC  string `json:"mac"`
				IP   string `json:"ip"`
			} `json:"devices"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		imported := 0
		skipped := 0
		for _, d := range req.Devices {
			if d.Name == "" || d.MAC == "" {
				skipped++
				continue
			}
			if !validateMAC(d.MAC) {
				skipped++
				continue
			}
			// Check if MAC already exists
			exists := false
			for _, existing := range store.List() {
				if strings.EqualFold(existing.MAC, d.MAC) {
					exists = true
					break
				}
			}
			if exists {
				skipped++
				continue
			}
			store.Add(d.Name, d.MAC, d.IP)
			imported++
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"imported": imported,
			"skipped":  skipped,
			"message":  fmt.Sprintf("导入 %d 台设备，跳过 %d 台（已存在或无效）", imported, skipped),
		})
	})

	// API: List all devices
	mux.HandleFunc("/api/devices", func(w http.ResponseWriter, r *http.Request) {
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
				jsonError(w, err.Error(), http.StatusBadRequest)
				return
			}
			if req.Name == "" || req.MAC == "" {
				jsonError(w, "名称和MAC地址不能为空", http.StatusBadRequest)
				return
			}
			if !validateMAC(req.MAC) {
				jsonError(w, "MAC地址格式错误", http.StatusBadRequest)
				return
			}
			if !validateIP(req.IP) {
				jsonError(w, "IP地址格式错误", http.StatusBadRequest)
				return
			}
			device := store.Add(req.Name, req.MAC, req.IP)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(device)
			return
		}
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	// API: Get/Update/Delete a specific device
	mux.HandleFunc("/api/devices/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Extract ID from URL path
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/devices/"), "/")
		id, err := strconv.Atoi(parts[0])
		if err != nil {
			jsonError(w, "invalid device ID", http.StatusBadRequest)
			return
		}

		switch r.Method {
		case http.MethodGet:
			device, ok := store.Get(id)
			if !ok {
				jsonError(w, "device not found", http.StatusNotFound)
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
				jsonError(w, err.Error(), http.StatusBadRequest)
				return
			}
			if req.Name == "" || req.MAC == "" {
				jsonError(w, "名称和MAC地址不能为空", http.StatusBadRequest)
				return
			}
			if !validateMAC(req.MAC) {
				jsonError(w, "MAC地址格式错误", http.StatusBadRequest)
				return
			}
			if !validateIP(req.IP) {
				jsonError(w, "IP地址格式错误", http.StatusBadRequest)
				return
			}
			device, ok := store.Update(id, req.Name, req.MAC, req.IP)
			if !ok {
				jsonError(w, "device not found", http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(device)

		case http.MethodDelete:
			if ok := store.Delete(id); !ok {
				jsonError(w, "device not found", http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		default:
			jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// API: Wake a device by ID
	mux.HandleFunc("/api/wake", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID int `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		device, ok := store.Get(req.ID)
		if !ok {
			jsonError(w, "device not found", http.StatusNotFound)
			return
		}
		if err := sendWOL(device.MAC, device.IP); err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "WOL packet sent to " + device.Name})
	})

	// API: Check status of all devices
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		devices := store.List()
		result := make([]map[string]string, len(devices))
		var wg sync.WaitGroup
		for i, d := range devices {
			wg.Add(1)
			go func(idx int, dev Device) {
				defer wg.Done()
				result[idx] = map[string]string{
					"id":     strconv.Itoa(dev.ID),
					"status": checkDeviceStatus(dev.IP),
				}
			}(i, d)
		}
		wg.Wait()
		json.NewEncoder(w).Encode(result)
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
