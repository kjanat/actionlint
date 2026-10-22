package core

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero/api"
)

type responseModule struct {
	api.Module
	pointer uint32
	length  uint32
	read    *bool
}

func (m responseModule) ExportedFunction(name string) api.Function {
	function := m.Module.ExportedFunction(name)
	if name == "check" || name == "metadata" || name == "validate_config" {
		return responseFunction{Function: function, result: uint64(m.pointer)<<32 | uint64(m.length)}
	}
	return function
}

func (m responseModule) Memory() api.Memory {
	return responseMemory{Memory: m.Module.Memory(), read: m.read}
}

type responseFunction struct {
	api.Function
	result uint64
}

func (f responseFunction) Call(context.Context, ...uint64) ([]uint64, error) {
	return []uint64{f.result}, nil
}

type responseMemory struct {
	api.Memory
	read *bool
}

func (m responseMemory) Read(offset, length uint32) ([]byte, bool) {
	*m.read = true
	return m.Memory.Read(offset, length)
}

func TestResponseLimitPrecedesGuestMemoryRead(t *testing.T) {
	engine := pooledEngine(t)
	module, err := engine.instantiate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close(t.Context())
	for _, operation := range []string{"check", "metadata", "validate_config"} {
		for _, length := range []uint32{8*1024*1024 + 1, ^uint32(0)} {
			read := false
			_, err := engine.invoke(t.Context(), responseModule{Module: module, pointer: 1024, length: length, read: &read}, operation, nil)
			if err == nil || !strings.Contains(err.Error(), "response limit of 8388608 bytes") || read {
				t.Fatalf("%s length=%d: read=%v err=%v", operation, length, read, err)
			}
		}
	}
}

func TestResponseAtHostLimitRemainsUsable(t *testing.T) {
	for _, length := range []uint32{8*1024*1024 - 1, 8 * 1024 * 1024} {
		engine := pooledEngine(t)
		module, err := engine.instantiate(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		allocation, err := module.ExportedFunction("alloc").Call(t.Context(), uint64(length))
		if err != nil {
			t.Fatal(err)
		}
		pointer := uint32(allocation[0])
		payload := bytes.Repeat([]byte{' '}, int(length))
		copy(payload, `{"ok":true}`)
		if !module.Memory().Write(pointer, payload) {
			t.Fatal("cannot write response fixture")
		}
		read := false
		engine.idle = []api.Module{responseModule{Module: module, pointer: pointer, length: length, read: &read}}
		result, err := engine.call(t.Context(), "check", nil)
		if err != nil || !read || result == nil || !result.OK {
			t.Fatalf("length=%d read=%v result=%+v err=%v", length, read, result, err)
		}
	}
}
