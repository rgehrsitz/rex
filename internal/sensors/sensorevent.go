package sensors

// SensorEvent represents an event containing a sensor name and its new value.
type SensorEvent struct {
	SensorName string
	Value      interface{}
}
