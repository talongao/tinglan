//go:build darwin && cgo

// Package webauth signs in through a disposable in-app browser window.
package webauth

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework AuthenticationServices -framework WebKit
#include <stdint.h>
#include <stdlib.h>
void *hm_auth_start(const char *url, uintptr_t handle);
void hm_auth_close(void *session);
void *hm_web_auth_start(const char *url, uintptr_t handle);
void hm_web_auth_close(void *session);
*/
import "C"

import (
	"context"
	"errors"
	"os"
	"runtime/cgo"
	"unsafe"

	"tinglan/native/internal/huawei"
)

type result struct {
	url    string
	status int
}

// Authorize opens the Huawei login page in a disposable in-app window. The
// window has no persistent cookies, follows only the login domains and hands
// the hms:// callback back here for validation.
//
// TINGLAN_AUTH_SYSTEM=1 switches to the OS authorization sheet. That sheet is
// kept as a fallback because some Huawei accounts may refuse embedded webviews.
//
// Never launch an external browser here: doing so can attach to the user's
// running browser and close their tabs when the disposable session ends.
func Authorize(ctx context.Context, attempt *huawei.Attempt) (string, error) {
	if os.Getenv("TINGLAN_AUTH_SYSTEM") == "" {
		if raw, err, handled := webAuthorize(ctx, attempt); handled {
			return raw, err
		}
	}
	return systemAuthorize(ctx, attempt)
}

// webAuthorize returns handled=false when the embedded window cannot start, so
// the caller falls back to the system sheet instead of failing the login.
func webAuthorize(ctx context.Context, attempt *huawei.Attempt) (string, error, bool) {
	if err := ctx.Err(); err != nil {
		return "", errors.New("authorization cancelled"), true
	}
	ch := make(chan result, 1)
	handle := cgo.NewHandle(ch)
	raw := C.CString(attempt.AuthorizeURL())
	session := C.hm_web_auth_start(raw, C.uintptr_t(handle))
	C.free(unsafe.Pointer(raw))
	if session == nil {
		handle.Delete()
		return "", nil, false
	}
	defer C.hm_web_auth_close(session)
	select {
	case r := <-ch:
		if r.status != 0 {
			return "", errors.New("authorization cancelled"), true
		}
		if !huawei.IsCallback(r.url) {
			return "", errors.New("authorization callback rejected"), true
		}
		return r.url, nil, true
	case <-ctx.Done():
		return "", errors.New("authorization cancelled"), true
	}
}

func systemAuthorize(ctx context.Context, attempt *huawei.Attempt) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", errors.New("authorization cancelled")
	}
	ch := make(chan result, 1)
	handle := cgo.NewHandle(ch)
	raw := C.CString(attempt.AuthorizeURL())
	session := C.hm_auth_start(raw, C.uintptr_t(handle))
	C.free(unsafe.Pointer(raw))
	defer C.hm_auth_close(session)
	select {
	case r := <-ch:
		if r.status != 0 {
			return "", errors.New("system browser authorization cancelled or unavailable")
		}
		if !huawei.IsCallback(r.url) {
			return "", errors.New("system browser callback rejected")
		}
		return r.url, nil
	case <-ctx.Done():
		return "", errors.New("authorization cancelled")
	}
}

//export hm_auth_complete
func hm_auth_complete(handle C.uintptr_t, raw *C.char, status C.int) {
	h := cgo.Handle(handle)
	ch := h.Value().(chan result)
	value := ""
	if raw != nil {
		value = C.GoString(raw)
	}
	ch <- result{url: value, status: int(status)}
	h.Delete()
}

//export hm_auth_allowed
func hm_auth_allowed(raw *C.char) C.int {
	if raw == nil {
		return 0
	}
	if huawei.AllowedLoginURL(C.GoString(raw)) {
		return 1
	}
	return 0
}
