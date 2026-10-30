package platform

type CPUFrequency struct {
	MHz, MinMHz, MaxMHz float64
	Reported            bool
}
