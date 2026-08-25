package windows

/*
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lole32 -lshell32 -luuid
#include <stdlib.h>
int foldersnap_recycle(const wchar_t* path, wchar_t* message, unsigned int messageLength);
*/
import "C"

import (
	"context"
	"errors"
	"syscall"
	"unsafe"

	"foldersnap/internal/cleanup"
)

type RecycleBin struct{}

func (RecycleBin) Move(ctx context.Context, targets []string) []cleanup.MoveResult {
	results := make([]cleanup.MoveResult, 0, len(targets))
	for _, target := range targets {
		result := cleanup.MoveResult{Target: target}
		if err := ctx.Err(); err != nil {
			result.Error = err
			results = append(results, result)
			continue
		}
		wide, err := syscall.UTF16FromString(target)
		if err != nil {
			result.Error = err
			results = append(results, result)
			continue
		}
		message := make([]uint16, 512)
		hresult := C.foldersnap_recycle(
			(*C.wchar_t)(unsafe.Pointer(&wide[0])),
			(*C.wchar_t)(unsafe.Pointer(&message[0])),
			C.uint(len(message)),
		)
		if hresult != 0 {
			text := syscall.UTF16ToString(message)
			if text == "" {
				text = "Windows Shell rejected the Recycle Bin operation"
			}
			result.Error = errors.New(text)
		}
		results = append(results, result)
	}
	return results
}
