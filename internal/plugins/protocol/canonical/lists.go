// Package canonical shares component ABI operations across generated interfaces.
package canonical

import (
	"context"
	"fmt"

	"github.com/kjanat/wacogo/host"
)

type Memory interface {
	ReadUint32Le(uint32) (uint32, bool)
	WriteUint32Le(uint32, uint32) bool
}

func ReadFlat[T any](stack []uint64, stride uint32, read func(uint32) (T, error)) ([]T, error) {
	return readList(uint32(stack[0]), uint32(stack[1]), stride, read)
}

func ReadMemory[T any](memory Memory, address, stride uint32, operation string, read func(uint32) (T, error)) ([]T, error) {
	pointer, ok := memory.ReadUint32Le(address)
	if !ok {
		return nil, fmt.Errorf("wacogo/witgen: %s: list ptr: bad memory read", operation)
	}
	length, ok := memory.ReadUint32Le(address + 4)
	if !ok {
		return nil, fmt.Errorf("wacogo/witgen: %s: list len: bad memory read", operation)
	}
	return readList(pointer, length, stride, read)
}

func readList[T any](pointer, length, stride uint32, read func(uint32) (T, error)) ([]T, error) {
	values := make([]T, length)
	for index := range length {
		value, err := read(pointer + index*stride)
		if err != nil {
			return nil, err
		}
		values[index] = value
	}
	return values, nil
}

func WriteFlat[T any](ctx context.Context, call *host.CallContext, stack []uint64, values []T, alignment, stride uint32, operation string, write func(uint32, T) error) error {
	pointer, err := writeList(ctx, call, values, alignment, stride, operation, write)
	if err != nil {
		return err
	}
	stack[0], stack[1] = uint64(pointer), uint64(uint32(len(values)))
	return nil
}

func WriteMemory[T any](ctx context.Context, call *host.CallContext, address uint32, values []T, alignment, stride uint32, operation string, write func(uint32, T) error) error {
	pointer, err := writeList(ctx, call, values, alignment, stride, operation, write)
	if err != nil {
		return err
	}
	if !call.Memory().WriteUint32Le(address, pointer) {
		return fmt.Errorf("wacogo/witgen: %s: list ptr: bad memory write", operation)
	}
	if !call.Memory().WriteUint32Le(address+4, uint32(len(values))) {
		return fmt.Errorf("wacogo/witgen: %s: list len: bad memory write", operation)
	}
	return nil
}

func writeList[T any](ctx context.Context, call *host.CallContext, values []T, alignment, stride uint32, operation string, write func(uint32, T) error) (uint32, error) {
	pointer, err := call.Realloc(ctx, 0, 0, alignment, uint32(len(values))*stride)
	if err != nil {
		return 0, fmt.Errorf("wacogo/witgen: %s: realloc failed: %w", operation, err)
	}
	for index, value := range values {
		if err := write(pointer+uint32(index)*stride, value); err != nil {
			return 0, err
		}
	}
	return pointer, nil
}
