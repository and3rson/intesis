// Interface to control and monitor Intesis adapters (Mitsubishi Air Conditioning systems).
//
// Usage example:
//
//	package main
//
//	import (
//		"context"
//		"github.com/and3rson/intesis"
//	)
//
//	func main() {
//		intesisManager := intesis.NewManager()
//		intesisManager.AddDevice(intesis.NewDevice("ac_one", "192.168.1.101"))
//		intesisManager.AddDevice(intesis.NewDevice("ac_two", "192.168.1.102"))
//		intesisManager.AddDevice(intesis.NewDevice("ac_three", "192.168.1.103"))
//		intesisManager.Run()
//
//		// Set a datapoint
//		intesisManager.Devices[0].Set(intesis.UserSetpoint, 250) // Set temperature to 25.0°C
//		intesisManager.Devices[1].Set(intesis.OnOff, 1) // Turn on
//
//		for {
//			select {
//			case event := <-intesisManager.Events:
//				// Handle event
//				fmt.Printf("Got event: device=%s, datapoint=%s\n", event.Device.Name, event.Datapoint)
//			}
//		}
//	}
package intesis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/mitchellh/mapstructure"
	log "github.com/sirupsen/logrus"
)

// Enum of datapoint UIDs
type DatapointUID int

const (
	OnOff                   DatapointUID = 1
	UserMode                DatapointUID = 2
	FanSpeed                DatapointUID = 4
	VaneUpDown              DatapointUID = 5
	UserSetpoint            DatapointUID = 9
	ReturnPathTemperture    DatapointUID = 10
	RemoteDisable           DatapointUID = 12
	OnTime                  DatapointUID = 13
	AlarmStatus             DatapointUID = 14
	ErrorCode               DatapointUID = 15
	MinTemperatureSetpoint  DatapointUID = 35
	MaxTemperatureSetpoint  DatapointUID = 36
	OutoorTemperature       DatapointUID = 37
	MaintenanceTime         DatapointUID = 181
	MaintenanceConfig       DatapointUID = 182
	MaintenanceFilterTime   DatapointUID = 183
	MaintenanceFilterConfig DatapointUID = 184
)

// Datapoint names
var datapointNames = map[DatapointUID]string{
	OnOff:                   "On/Off",
	UserMode:                "User Mode",
	FanSpeed:                "Fan Speed",
	VaneUpDown:              "Vane Up/Down",
	UserSetpoint:            "User Setpoint",
	ReturnPathTemperture:    "Return Path Temperture",
	RemoteDisable:           "Remote Disable",
	OnTime:                  "On Time",
	AlarmStatus:             "Alarm Status",
	ErrorCode:               "Error Code",
	MinTemperatureSetpoint:  "Min Temperature Setpoint",
	MaxTemperatureSetpoint:  "Max Temperature Setpoint",
	OutoorTemperature:       "Outoor Temperature",
	MaintenanceTime:         "Maintenance Time",
	MaintenanceConfig:       "Maintenance Config",
	MaintenanceFilterTime:   "Maintenance Filter Time",
	MaintenanceFilterConfig: "Maintenance Filter Config",
}

// Datapoint struct
type Datapoint struct {
	// UID of the datapoint (see DatapointUID)
	UID DatapointUID `mapstructure:"uid"`
	// Value of the datapoint
	Value int `mapstructure:"value"`
	// Status of the datapoint (?) (not used)
	Status int `mapstructure:"status"`
}

// Name returns the name of the datapoint
func (dp Datapoint) Name() string {
	return datapointNames[dp.UID]
}

// String returns a string representation of the datapoint
func (dp Datapoint) String() string {
	return fmt.Sprintf("Datapoint{%s: %d, %d}", dp.Name(), dp.Value, dp.Status)
}

// Compare compares two datapoints
func (dp Datapoint) Compare(other Datapoint) bool {
	return dp.UID == other.UID && dp.Value == other.Value
}

// Response struct represents the payload of the API response
type Response struct {
	Success bool `json:"success"`
	Error   struct {
		Message string `json:"message"`
	} `json:"error"`
	Data map[string]interface{} `json:"data"`
}

// Event struct represents an event produced by the device
type Event struct {
	// Device that produced the event
	Device *Device
	// New datapoint value
	Datapoint Datapoint
}

// Device struct represents an Intesis device
type Device struct {
	// Name of the device
	Name string
	// Session ID (fetched during login)
	sessionID string
	// API URL
	apiURL string
	// Username (defaults to "admin")
	username string
	// Password (defaults to "admin")
	password string
	// Datapoints
	Datapoints map[DatapointUID]Datapoint
	// Events channel: receives events when a datapoint changes
	Events chan Event
	// Mutex for synchronizing concurrent API calls
	mutex sync.Mutex
}

// NewDevice creates a new Device
func NewDevice(name string, host string) *Device {
	apiURL := fmt.Sprintf("http://%s/api.cgi", host)
	log.Debugf("Creating new client: %s", apiURL)

	return &Device{
		Name:       name,
		sessionID:  "",
		apiURL:     apiURL,
		username:   "",
		password:   "",
		Datapoints: make(map[DatapointUID]Datapoint),
	}
}

// callAPI calls the API
func (dev *Device) callAPI(ctx context.Context, command string, data interface{}) (Response, error) {
	var payload struct {
		Command string                 `json:"command"`
		Data    map[string]interface{} `json:"data"`
	}

	var err error

	// Initialize payload
	err = mapstructure.Decode(data, &payload.Data)
	if err != nil {
		return Response{}, fmt.Errorf("intesis: callAPI: failed to convert data to map[string]interface{}: %w", err)
	}

	payload.Command = command
	payload.Data["SessionId"] = dev.sessionID

	// JSON-encode the payload
	var requestBody []byte
	requestBody, err = json.Marshal(payload)

	if err != nil {
		return Response{}, fmt.Errorf("intesis: callAPI: failed to marshal data: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", dev.apiURL, bytes.NewBuffer(requestBody))
	if err != nil {
		return Response{}, fmt.Errorf("intesis: callAPI: failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")

	if dev.sessionID != "" {
		req.AddCookie(&http.Cookie{
			Name:  "Intesis-Webserver",
			Value: fmt.Sprintf("{%%22sessionID%%22:%%22%s%%22}", dev.sessionID),
		})
	}

	// Call the API
	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		return Response{}, fmt.Errorf("intesis: callAPI: failed to call API: %w", err)
	}

	defer resp.Body.Close()

	// Check the status code
	if resp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("intesis: callAPI: server returned status code %d", resp.StatusCode)
	}

	// Decode the response
	var response Response
	err = json.NewDecoder(resp.Body).Decode(&response)

	if err != nil {
		return Response{}, fmt.Errorf("intesis: callAPI: failed to decode response: %w", err)
	}

	if !response.Success {
		return Response{}, fmt.Errorf("intesis: callAPI: server returned error: %s", response.Error.Message)
	}

	return response, nil
}

// Log in to the device
func (dev *Device) login(ctx context.Context) error {
	var data struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	data.Username = dev.username
	data.Password = dev.password

	var err error

	response, err := dev.callAPI(ctx, "login", data)
	if err != nil {
		return fmt.Errorf("intesis: Login: %w", err)
	}

	id := response.Data["id"]
	sessionID, ok := id.(map[string]interface{})["sessionID"].(string)

	if !ok {
		return fmt.Errorf("intesis: Login: failed to cast sessionID in response to string")
	}

	dev.sessionID = sessionID

	return nil
}

// Run starts polling the device
// When a datapoint changes, it sends an event to the Events channel.
func (dev *Device) Run(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)

	dev.Events = make(chan Event, 32)

	go func() {
		defer wg.Done()
		defer close(dev.Events)

		for {
			var err error

			func() {
				dev.mutex.Lock()
				defer dev.mutex.Unlock()

				if err = dev.login(ctx); err != nil {
					log.Errorf("intesis: Run: %v", err)

					return
				}

				response, err := dev.callAPI(ctx, "getdatapointvalue", map[string]string{"uid": "all"})
				if err != nil {
					log.Errorf("intesis: Run: %v", err)

					return
				}

				newDatapoints, ok := (&response).Data["dpval"].([]interface{})
				if !ok {
					log.Errorf("intesis: Run: failed to cast dpval to list of interfaces")

					return
				}

				for _, entry := range newDatapoints {
					var newDatapoint Datapoint

					if err = mapstructure.Decode(entry, &newDatapoint); err != nil {
						log.Errorf("intesis: Run: failed to decode datapoints: %v", err)

						return
					}

					if oldDatapoint, ok := dev.Datapoints[newDatapoint.UID]; ok {
						if !oldDatapoint.Compare(newDatapoint) {
							dev.Datapoints[newDatapoint.UID] = newDatapoint
							dev.Events <- Event{Device: dev, Datapoint: newDatapoint}

							log.Debugf("intesis: datapoint event: %s: %s -> %s", newDatapoint.Name(), oldDatapoint, newDatapoint)
						}
					} else {
						dev.Datapoints[newDatapoint.UID] = newDatapoint
						dev.Events <- Event{Device: dev, Datapoint: newDatapoint}

						log.Debugf("intesis: datapoint event: %s: %s", newDatapoint.Name(), oldDatapoint)
					}
				}
			}()

			// Wait for 1 second
			select {
			case <-time.After(1 * time.Second):
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Set sets a datapoint value
func (dev *Device) Set(ctx context.Context, uid DatapointUID, value int) error {
	dev.mutex.Lock()
	defer dev.mutex.Unlock()

	var data struct {
		UID   int `json:"uid"`
		Value int `json:"value"`
	}

	data.UID = int(uid)
	data.Value = value

	newDatapoint := Datapoint{UID: uid, Value: value}

	if current, ok := dev.Datapoints[uid]; ok && current.Value == value {
		log.Debugf("intesis: Set: set datapoint: %s = %d (no change)", newDatapoint.Name(), value)

		return nil
	}

	log.Debugf("intesis: Set: set datapoint: %s = %d", newDatapoint.Name(), value)

	// Call auth
	if err := dev.login(ctx); err != nil {
		return fmt.Errorf("intesis: Set: %w", err)
	}

	dev.Datapoints[uid] = newDatapoint

	_, err := dev.callAPI(ctx, "setdatapointvalue", data)
	if err != nil {
		return fmt.Errorf("intesis: Set: %w", err)
	}

	return nil
}

// Manager struct is a convenience manager for multiple Intesis devices that collects events from all devices
type Manager struct {
	// Array of devices
	Devices []*Device
	// Events channel: receives events from all devices
	Events chan Event
}

// NewManager creates a new Manager
func NewManager() *Manager {
	return &Manager{
		Devices: make([]*Device, 0),
		Events:  make(chan Event, 32),
	}
}

// AddDevice adds a device to the manager
func (man *Manager) AddDevice(device *Device) {
	man.Devices = append(man.Devices, device)
}

// Run starts polling all devices
// When a datapoint changes, it sends an event to the Events channel.
func (man *Manager) Run(ctx context.Context, wg *sync.WaitGroup) {
	for _, device := range man.Devices {
		go func(device *Device) {
			device.Run(ctx, wg)

			for {
				select {
				case event := <-device.Events:
					man.Events <- event
				}
			}
		}(device)
	}
}
