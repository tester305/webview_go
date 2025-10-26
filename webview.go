package webview

/*
#cgo CFLAGS: -I${SRCDIR}/libs/webview/include
#cgo CXXFLAGS: -I${SRCDIR}/libs/webview/include -DWEBVIEW_STATIC

#cgo linux openbsd freebsd netbsd CXXFLAGS: -DWEBVIEW_GTK -std=c++11
#cgo linux openbsd freebsd netbsd LDFLAGS: -ldl
#cgo linux openbsd freebsd netbsd pkg-config: gtk+-3.0 webkit2gtk-4.1

#cgo darwin CXXFLAGS: -DWEBVIEW_COCOA -std=c++11
#cgo darwin LDFLAGS: -framework WebKit -ldl

#cgo windows CXXFLAGS: -DWEBVIEW_EDGE -std=c++14 -I${SRCDIR}/libs/mswebview2/include
#cgo windows LDFLAGS: -static -ladvapi32 -lole32 -lshell32 -lshlwapi -luser32 -lversion

#include "webview.h"

#include <stdlib.h>
#include <stdint.h>

void CgoWebViewDispatch(webview_t w, uintptr_t arg);
void CgoWebViewBind(webview_t w, const char *name, uintptr_t index);
void CgoWebViewUnbind(webview_t w, const char *name);
*/
import "C"
import (
	"encoding/json"
	"errors"
	"reflect"
	"runtime"
	"sync"
	"unsafe"

	_ "github.com/webview/webview_go/libs/mswebview2"
	_ "github.com/webview/webview_go/libs/mswebview2/include"
	_ "github.com/webview/webview_go/libs/webview"
	_ "github.com/webview/webview_go/libs/webview/include"
)

func init() {
	// Ensure that main.main is called from the main thread
	runtime.LockOSThread()
}

// ---------------------------------------------------------------------
// Public constants & types
// ---------------------------------------------------------------------

type Hint int

const (
	HintNone  Hint = C.WEBVIEW_HINT_NONE
	HintFixed Hint = C.WEBVIEW_HINT_FIXED
	HintMin   Hint = C.WEBVIEW_HINT_MIN
	HintMax   Hint = C.WEBVIEW_HINT_MAX
)

type WebView interface {
	Run()
	Terminate()
	Dispatch(f func())
	Destroy()
	Window() unsafe.Pointer
	SetTitle(title string)
	SetSize(w int, h int, hint Hint)
	Navigate(url string)
	SetHtml(html string)
	Init(js string)
	Eval(js string)
	Bind(name string, f interface{}) error
	Unbind(name string) error
}

// ---------------------------------------------------------------------
// Internal implementation
// ---------------------------------------------------------------------

type webview struct {
	w C.webview_t // pointer type, nil == invalid
}

var (
	m        sync.Mutex
	index    uintptr
	dispatch = map[uintptr]func(){}
	bindings = map[uintptr]func(id, req string) (interface{}, error){}
)

func boolToInt(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------
// Factory functions
// ---------------------------------------------------------------------

func New(debug bool) WebView { return NewWindow(debug, nil) }

func NewWindow(debug bool, window unsafe.Pointer) WebView {
	w := &webview{}
	// C.webview_create returns a pointer; nil means failure
	w.w = C.webview_create(boolToInt(debug), window)
	return w
}

// ---------------------------------------------------------------------
// WebView methods – all now compare w.w with nil
// ---------------------------------------------------------------------

func (w *webview) Destroy() {
	if w == nil || w.w == nil {
		return
	}
	C.webview_destroy(w.w)
}

func (w *webview) Run() {
	if w == nil || w.w == nil {
		return
	}
	C.webview_run(w.w)
}

func (w *webview) Terminate() {
	if w == nil || w.w == nil {
		return
	}
	C.webview_terminate(w.w)
}

func (w *webview) Window() unsafe.Pointer {
	if w == nil || w.w == nil {
		return nil
	}
	return C.webview_get_window(w.w)
}

func (w *webview) Navigate(url string) {
	if w == nil || w.w == nil {
		return
	}
	s := C.CString(url)
	defer C.free(unsafe.Pointer(s))
	C.webview_navigate(w.w, s)
}

func (w *webview) SetHtml(html string) {
	if w == nil || w.w == nil {
		return
	}
	s := C.CString(html)
	defer C.free(unsafe.Pointer(s))
	C.webview_set_html(w.w, s)
}

func (w *webview) SetTitle(title string) {
	if w == nil || w.w == nil {
		return
	}
	s := C.CString(title)
	defer C.free(unsafe.Pointer(s))
	C.webview_set_title(w.w, s)
}

func (w *webview) SetSize(width int, height int, hint Hint) {
	if w == nil || w.w == nil {
		return
	}
	C.webview_set_size(w.w, C.int(width), C.int(height), C.webview_hint_t(hint))
}

func (w *webview) Init(js string) {
	if w == nil || w.w == nil {
		return
	}
	s := C.CString(js)
	defer C.free(unsafe.Pointer(s))
	C.webview_init(w.w, s)
}

func (w *webview) Eval(js string) {
	if w == nil || w.w == nil {
		return
	}
	s := C.CString(js)
	defer C.free(unsafe.Pointer(s))
	C.webview_eval(w.w, s)
}

// ---------------------------------------------------------------------
// Dispatch (run Go func on UI thread)
// ---------------------------------------------------------------------

func (w *webview) Dispatch(f func()) {
	if w == nil || w.w == nil {
		go f()
		return
	}

	m.Lock()
	for dispatch[index] != nil {
		index++
	}
	dispatch[index] = f
	m.Unlock()

	C.CgoWebViewDispatch(w.w, C.uintptr_t(index))
}

//export _webviewDispatchGoCallback
func _webviewDispatchGoCallback(idx unsafe.Pointer) {
	i := uintptr(idx)
	m.Lock()
	f := dispatch[i]
	delete(dispatch, i)
	m.Unlock()
	if f != nil {
		f()
	}
}

// ---------------------------------------------------------------------
// Bind / Unbind
// ---------------------------------------------------------------------

func (w *webview) Bind(name string, f interface{}) error {
	if w == nil || w.w == nil {
		return errors.New("webview not created")
	}

	v := reflect.ValueOf(f)
	if err := checkBindFuncSignature(v); err != nil {
		return err
	}

	binding := func(id, req string) (interface{}, error) {
		args, err := parseBindArgs(v, req)
		if err != nil {
			return nil, err
		}
		return callBindFunc(v, args)
	}

	m.Lock()
	for bindings[index] != nil {
		index++
	}
	bindings[index] = binding
	m.Unlock()

	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	C.CgoWebViewBind(w.w, cname, C.uintptr_t(index))
	return nil
}

func (w *webview) Unbind(name string) error {
	if w == nil || w.w == nil {
		return errors.New("webview not created")
	}
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	C.CgoWebViewUnbind(w.w, cname)
	return nil
}

// ---------------------------------------------------------------------
// Binding helpers (unchanged, only tiny safety tweaks)
// ---------------------------------------------------------------------

func checkBindFuncSignature(v reflect.Value) error {
	if v.Kind() != reflect.Func {
		return errors.New("only functions can be bound")
	}
	if n := v.Type().NumOut(); n > 2 {
		return errors.New("function may only return a value or a value+error")
	}
	return nil
}

func parseBindArgs(v reflect.Value, req string) ([]reflect.Value, error) {
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(req), &raw); err != nil {
		return nil, err
	}
	isVariadic := v.Type().IsVariadic()
	numIn := v.Type().NumIn()
	if (isVariadic && len(raw) < numIn-1) || (!isVariadic && len(raw) != numIn) {
		return nil, errors.New("function arguments mismatch")
	}
	args := make([]reflect.Value, 0, len(raw))
	for i := range raw {
		var arg reflect.Value
		if isVariadic && i >= numIn-1 {
			arg = reflect.New(v.Type().In(numIn - 1).Elem())
		} else {
			arg = reflect.New(v.Type().In(i))
		}
		if err := json.Unmarshal(raw[i], arg.Interface()); err != nil {
			return nil, err
		}
		args = append(args, arg.Elem())
	}
	return args, nil
}

func callBindFunc(v reflect.Value, args []reflect.Value) (interface{}, error) {
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	res := v.Call(args)
	switch len(res) {
	case 0:
		return nil, nil
	case 1:
		if res[0].Type().Implements(errorType) {
			if res[0].Interface() != nil {
				return nil, res[0].Interface().(error)
			}
			return nil, nil
		}
		return res[0].Interface(), nil
	case 2:
		if !res[1].Type().Implements(errorType) {
			return nil, errors.New("second return value must be an error")
		}
		if res[1].Interface() == nil {
			return res[0].Interface(), nil
		}
		return res[0].Interface(), res[1].Interface().(error)
	default:
		return nil, errors.New("unexpected number of return values")
	}
}

// ---------------------------------------------------------------------
// Exported C callback for bindings
// ---------------------------------------------------------------------

//export _webviewBindingGoCallback
func _webviewBindingGoCallback(w C.webview_t, id *C.char, req *C.char, idx uintptr) {
	m.Lock()
	f := bindings[idx]
	m.Unlock()

	jsString := func(v interface{}) string {
		b, _ := json.Marshal(v)
		return string(b)
	}

	status, result := 0, ""
	if f == nil {
		status = -1
		result = jsString("binding not found")
	} else if res, err := f(C.GoString(id), C.GoString(req)); err != nil {
		status = -1
		result = jsString(err.Error())
	} else if b, err := json.Marshal(res); err != nil {
		status = -1
		result = jsString(err.Error())
	} else {
		status = 0
		result = string(b)
	}

	s := C.CString(result)
	defer C.free(unsafe.Pointer(s))
	C.webview_return(w, id, C.int(status), s)
}
