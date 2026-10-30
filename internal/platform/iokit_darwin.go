//go:build darwin

package platform

import (
	"errors"
	"fmt"
	"github.com/ebitengine/purego"
	"math"
	"sync"
)

// Read-only IOKit/CoreFoundation bindings. Framework handles live for the
// process; every iterator, registry object and copied CF value is released.
type ioKit struct {
	matching       func(string) uintptr
	services       func(uint32, uintptr, *uint32) int32
	next           func(uint32) uint32
	releaseObject  func(uint32) int32
	entryID        func(uint32, *uint64) int32
	property       func(uint32, uintptr, uintptr, uint32) uintptr
	makeString     func(uintptr, string, uint32) uintptr
	release        func(uintptr)
	typeID         func(uintptr) uintptr
	dictionaryType func() uintptr
	numberType     func() uintptr
	stringType     func() uintptr
	dictValue      func(uintptr, uintptr) uintptr
	numberValue    func(uintptr, int32, *float64) bool
	stringValue    func(uintptr, *byte, int64, uint32) bool
}

var ioOnce sync.Once
var ioAPI *ioKit
var ioError error

func nativeIOKit() (*ioKit, error) {
	ioOnce.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				ioError = fmt.Errorf("IOKit bindings: %v", r)
			}
		}()
		lib, e := purego.Dlopen("/System/Library/Frameworks/IOKit.framework/IOKit", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if e != nil {
			ioError = e
			return
		}
		cf, e := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if e != nil {
			ioError = e
			return
		}
		a := &ioKit{}
		for name, target := range map[string]any{"IOServiceMatching": &a.matching, "IOServiceGetMatchingServices": &a.services, "IOIteratorNext": &a.next, "IOObjectRelease": &a.releaseObject, "IORegistryEntryGetRegistryEntryID": &a.entryID, "IORegistryEntryCreateCFProperty": &a.property} {
			purego.RegisterLibFunc(target, lib, name)
		}
		for name, target := range map[string]any{"CFStringCreateWithCString": &a.makeString, "CFRelease": &a.release, "CFGetTypeID": &a.typeID, "CFDictionaryGetTypeID": &a.dictionaryType, "CFNumberGetTypeID": &a.numberType, "CFStringGetTypeID": &a.stringType, "CFDictionaryGetValue": &a.dictValue, "CFNumberGetValue": &a.numberValue, "CFStringGetCString": &a.stringValue} {
			purego.RegisterLibFunc(target, cf, name)
		}
		ioAPI = a
	})
	return ioAPI, ioError
}

const utf8Encoding = 0x08000100

func (a *ioKit) objects(class string, visit func(uint32)) error {
	match := a.matching(class)
	if match == 0 {
		return errors.New("IOKit matching unavailable")
	}
	var iter uint32
	if code := a.services(0, match, &iter); code != 0 {
		return fmt.Errorf("IOKit service lookup: %d", code)
	}
	defer a.releaseObject(iter)
	for {
		entry := a.next(iter)
		if entry == 0 {
			break
		}
		func() { defer a.releaseObject(entry); visit(entry) }()
	}
	return nil
}
func (a *ioKit) prop(entry uint32, key string) uintptr {
	k := a.makeString(0, key, utf8Encoding)
	if k == 0 {
		return 0
	}
	defer a.release(k)
	return a.property(entry, k, 0, 0)
}
func (a *ioKit) value(dict uintptr, key string) uintptr {
	if dict == 0 || a.typeID(dict) != a.dictionaryType() {
		return 0
	}
	k := a.makeString(0, key, utf8Encoding)
	if k == 0 {
		return 0
	}
	defer a.release(k)
	return a.dictValue(dict, k)
}
func (a *ioKit) num(value uintptr) float64 {
	v := math.NaN()
	if value != 0 && a.typeID(value) == a.numberType() {
		if !a.numberValue(value, 13, &v) {
			return math.NaN()
		}
	}
	return v
}
func (a *ioKit) text(value uintptr) string {
	if value == 0 || a.typeID(value) != a.stringType() {
		return ""
	}
	var b [512]byte
	if !a.stringValue(value, &b[0], int64(len(b)), utf8Encoding) {
		return ""
	}
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return ""
}
func (a *ioKit) id(entry uint32, prefix string) string {
	var id uint64
	a.entryID(entry, &id)
	return fmt.Sprintf("%s:%d", prefix, id)
}
