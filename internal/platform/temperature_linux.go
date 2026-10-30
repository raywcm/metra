//go:build linux

package platform

import "context"

func nativeCPUTemperatures(context.Context) []Temperature { return nil }
