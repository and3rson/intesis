# intesis

Interface to control and monitor Intesis adapters (Mitsubishi Air Conditioning systems) with Go.

## Usage example

```go
package main

import (
	"context"
	"github.com/and3rson/intesis"
)

func main() {
	intesisManager := intesis.NewManager()
	intesisManager.AddDevice(intesis.NewDevice("ac_one", "192.168.1.101"))
	intesisManager.AddDevice(intesis.NewDevice("ac_two", "192.168.1.102"))
	intesisManager.AddDevice(intesis.NewDevice("ac_three", "192.168.1.103"))
	intesisManager.Run()

	// Set a datapoint
	intesisManager.Devices[0].Set(intesis.UserSetpoint, 250) // Set temperature to 25.0°C
	intesisManager.Devices[1].Set(intesis.OnOff, 1) // Turn on

	for {
		select {
		case event := <-intesisManager.Events:
			// Handle event
			fmt.Printf("Got event: device=%s, datapoint=%s\n", event.Device.Name, event.Datapoint)
		}
	}
}
```
