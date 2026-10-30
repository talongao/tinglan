//go:build windows

package webauth

import (
	"context"
	"errors"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/go-webview2/webviewloader"
	"github.com/zzl/go-com/com"
	"github.com/zzl/go-webview2/wv2"
	"github.com/zzl/go-win32api/v2/win32"
	"huawei-music-desktop/native/internal/huawei"
)

func NeedsManualCallback() bool { return false }

func Authorize(ctx context.Context, attempt *huawei.Attempt) (string, error) {
	return webViewAuthorize(ctx, attempt.AuthorizeURL(), huawei.AllowedLoginURL)
}

type authWindow struct {
	hwnd       win32.HWND
	controller *wv2.ICoreWebView2Controller
	view       *wv2.ICoreWebView2
	ctx        context.Context
	allowed    func(string) bool
	result     string
	err        error
	settled    bool
	pending    bool
}

var authWindows sync.Map
var authWindowClass sync.Once
var authWindowClassOK bool
var authWindowProc = syscall.NewCallback(authWndProc)
var authWindowSlot = make(chan struct{}, 1)

func (w *authWindow) finish(raw string, err error) {
	if !w.settled {
		w.result, w.err, w.settled = raw, err, true
	}
}

func (w *authWindow) resize() {
	if w.controller != nil {
		var rect win32.RECT
		win32.GetClientRect(w.hwnd, &rect)
		w.controller.SetBounds(wv2.TagRECT(rect))
	}
}

func authWndProc(hwnd win32.HWND, message uint32, wp win32.WPARAM, lp win32.LPARAM) win32.LRESULT {
	if value, ok := authWindows.Load(hwnd); ok {
		w := value.(*authWindow)
		switch message {
		case win32.WM_CLOSE:
			w.finish("", context.Canceled)
			win32.ShowWindow(hwnd, win32.SW_HIDE)
			return 0
		case win32.WM_SIZE:
			w.resize()
		case win32.WM_TIMER:
			if err := w.ctx.Err(); err != nil {
				w.finish("", err)
				win32.ShowWindow(hwnd, win32.SW_HIDE)
			}
		}
	}
	return win32.DefWindowProc(hwnd, message, wp, lp)
}

type authEnvironment struct {
	complete func(webviewloader.HRESULT, *webviewloader.ICoreWebView2Environment)
}

func (e *authEnvironment) EnvironmentCompleted(hr webviewloader.HRESULT, env *webviewloader.ICoreWebView2Environment) webviewloader.HRESULT {
	e.complete(hr, env)
	return 0
}

// The STA owns all window/COM objects. Cancellation is polled by its timer so
// no worker can call COM or destroy the window during a navigation callback.
func webViewAuthorize(ctx context.Context, address string, allowed func(string) bool) (result string, resultErr error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	select {
	case authWindowSlot <- struct{}{}:
		defer func() { <-authWindowSlot }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if _, err := webviewloader.GetAvailableCoreWebView2BrowserVersionString(""); err != nil {
		return "", errors.New("webview2 runtime unavailable")
	}
	dir, err := os.MkdirTemp("", "tinglan-webview-auth-")
	if err != nil {
		return "", errors.New("temporary browser profile unavailable")
	}
	defer func() {
		// Runtime/antivirus file locks must not discard a valid authorization.
		// InPrivate keeps account cookies out of the on-disk profile. Only our
		// freshly created directory is retried, never another browser's profile.
		cleanupAuthProfile(dir)
	}()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if hr := win32.CoInitializeEx(nil, win32.COINIT_APARTMENTTHREADED); hr < 0 {
		return "", errors.New("authorization window initialization failed")
	}
	defer win32.CoUninitialize()
	com.InitializeContext()
	defer com.UninitializeContext()
	scope := com.NewScope()
	defer scope.Leave()
	instance, _ := win32.GetModuleHandle(nil)
	className := win32.StrToPwstr("TinglanAuthorizationWindow")
	authWindowClass.Do(func() {
		class := win32.WNDCLASSEX{LpfnWndProc: authWindowProc, HInstance: instance, LpszClassName: className}
		class.CbSize = uint32(unsafe.Sizeof(class))
		class.HCursor, _ = win32.LoadCursor(0, win32.IDC_ARROW)
		class.HIcon, _ = win32.LoadIcon(instance, win32.MAKEINTRESOURCE(1))
		class.HbrBackground = win32.HBRUSH(win32.COLOR_WINDOW + 1)
		atom, _ := win32.RegisterClassEx(&class)
		authWindowClassOK = atom != 0
	})
	if !authWindowClassOK {
		return "", errors.New("authorization window initialization failed")
	}
	hwnd, _ := win32.CreateWindowEx(0, className, win32.StrToPwstr("Tinglan - Huawei Account"), win32.WS_OVERLAPPEDWINDOW,
		win32.CW_USEDEFAULT, win32.CW_USEDEFAULT, 820, 720, 0, 0, instance, nil)
	if hwnd == 0 {
		return "", errors.New("authorization window initialization failed")
	}
	w := &authWindow{hwnd: hwnd, ctx: ctx, allowed: allowed, pending: true}
	authWindows.Store(hwnd, w)
	defer authWindows.Delete(hwnd)
	defer win32.DestroyWindow(hwnd)
	defer func() {
		if w.controller != nil {
			w.controller.Close()
			w.controller.Release()
		}
		if w.view != nil {
			w.view.Release()
		}
	}()
	if timer, _ := win32.SetTimer(hwnd, 1, 50, 0); timer == 0 {
		return "", errors.New("authorization window initialization failed")
	}
	defer win32.KillTimer(hwnd, 1)
	win32.ShowWindow(hwnd, win32.SW_SHOW)
	envHandler := &authEnvironment{complete: func(hr webviewloader.HRESULT, env *webviewloader.ICoreWebView2Environment) {
		if hr < 0 || env == nil || w.settled {
			w.pending = false
			w.finish("", errors.New("authorization window initialization failed"))
			return
		}
		e := (*wv2.ICoreWebView2Environment)(unsafe.Pointer(env))
		handler := wv2.NewICoreWebView2CreateCoreWebView2ControllerCompletedHandlerByFunc(func(hr com.Error, controller *wv2.ICoreWebView2Controller) com.Error {
			w.pending = false
			if controller != nil {
				controller.AddRef()
				w.controller = controller
			}
			if hr.FAILED() || controller == nil || w.settled {
				w.finish("", errors.New("authorization window initialization failed"))
				return 0
			}
			if controller.GetCoreWebView2(&w.view).FAILED() || w.view == nil || !w.configure() {
				w.finish("", errors.New("authorization window initialization failed"))
				return 0
			}
			w.resize()
			if w.view.Navigate(address).FAILED() {
				w.finish("", errors.New("authorization page failed to load"))
			}
			return 0
		}, true)
		var privateEnv *wv2.ICoreWebView2Environment10
		if e.QueryInterface(&wv2.IID_ICoreWebView2Environment10, unsafe.Pointer(&privateEnv)) < 0 || privateEnv == nil {
			w.pending = false
			w.finish("", errors.New("webview2 runtime update required"))
			return
		}
		defer privateEnv.Release()
		var options *wv2.ICoreWebView2ControllerOptions
		if privateEnv.CreateCoreWebView2ControllerOptions(&options).FAILED() || options == nil {
			w.pending = false
			w.finish("", errors.New("authorization window initialization failed"))
			return
		}
		defer options.Release()
		if options.SetIsInPrivateModeEnabled(1).FAILED() || privateEnv.CreateCoreWebView2ControllerWithOptions(hwnd, options, handler).FAILED() {
			w.pending = false
			w.finish("", errors.New("authorization window initialization failed"))
		}
	}}
	if err := webviewloader.CreateCoreWebView2EnvironmentWithOptions(envHandler, webviewloader.WithUserDataFolder(dir)); err != nil {
		return "", errors.New("authorization window initialization failed")
	}
	for !w.settled || w.pending {
		var msg win32.MSG
		ret, _ := win32.GetMessage(&msg, 0, 0, 0)
		if ret <= 0 {
			w.finish("", context.Canceled)
			break
		}
		win32.TranslateMessage(&msg)
		win32.DispatchMessage(&msg)
	}
	runtime.KeepAlive(envHandler)
	return w.result, w.err
}

func cleanupAuthProfile(dir string) {
	if os.RemoveAll(dir) == nil {
		return
	}
	go func() {
		for i := 0; i < 120; i++ {
			time.Sleep(time.Second)
			if os.RemoveAll(dir) == nil {
				return
			}
		}
	}()
}

func (w *authWindow) inspect(raw string) bool {
	if huawei.IsCallback(raw) {
		w.finish(raw, nil)
		return false
	}
	return !w.settled && w.allowed(raw)
}

func (w *authWindow) configure() bool {
	var settings *wv2.ICoreWebView2Settings
	if w.view.GetSettings(&settings).FAILED() || settings == nil {
		return false
	}
	defer settings.Release()
	for _, disable := range []func(int32) com.Error{settings.SetIsWebMessageEnabled, settings.SetAreHostObjectsAllowed, settings.SetAreDevToolsEnabled, settings.SetAreDefaultContextMenusEnabled, settings.SetIsStatusBarEnabled} {
		if disable(0).FAILED() {
			return false
		}
	}
	var token wv2.EventRegistrationToken
	navigation := wv2.NewICoreWebView2NavigationStartingEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2NavigationStartingEventArgs) com.Error {
		var uri win32.PWSTR
		if args.GetUri(&uri).FAILED() {
			args.SetCancel(1)
			return 0
		}
		defer win32.CoTaskMemFree(unsafe.Pointer(uri))
		if !w.inspect(win32.PwstrToStr(uri)) {
			args.SetCancel(1)
		}
		return 0
	}, true)
	if w.view.Add_NavigationStarting(navigation, &token).FAILED() {
		return false
	}
	if w.view.Add_FrameNavigationStarting(navigation, &token).FAILED() {
		return false
	}
	if w.view.Add_NewWindowRequested(wv2.NewICoreWebView2NewWindowRequestedEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2NewWindowRequestedEventArgs) com.Error {
		args.SetHandled(1)
		var uri win32.PWSTR
		if !args.GetUri(&uri).FAILED() {
			defer win32.CoTaskMemFree(unsafe.Pointer(uri))
			raw := win32.PwstrToStr(uri)
			if w.inspect(raw) {
				w.view.Navigate(raw)
			}
		}
		return 0
	}, true), &token).FAILED() {
		return false
	}
	if w.view.Add_PermissionRequested(wv2.NewICoreWebView2PermissionRequestedEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2PermissionRequestedEventArgs) com.Error {
		return args.SetState(2) // Deny device/clipboard permissions in the login page.
	}, true), &token).FAILED() {
		return false
	}
	if w.view.Add_NavigationCompleted(wv2.NewICoreWebView2NavigationCompletedEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2NavigationCompletedEventArgs) com.Error {
		var success, status int32
		args.GetIsSuccess(&success)
		args.GetWebErrorStatus(&status)
		if success == 0 && status != 14 { // Explicitly cancelled navigation is expected for our callback.
			w.finish("", errors.New("authorization page failed to load"))
		}
		return 0
	}, true), &token).FAILED() {
		return false
	}
	var downloads *wv2.ICoreWebView2_4
	if w.view.QueryInterface(&wv2.IID_ICoreWebView2_4, unsafe.Pointer(&downloads)) < 0 || downloads == nil {
		return false
	}
	defer downloads.Release()
	return !downloads.Add_DownloadStarting(wv2.NewICoreWebView2DownloadStartingEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2DownloadStartingEventArgs) com.Error {
		args.SetHandled(1)
		return args.SetCancel(1)
	}, true), &token).FAILED()
}
