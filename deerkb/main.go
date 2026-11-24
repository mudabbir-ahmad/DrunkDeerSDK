package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"syscall"
	"unsafe"

	webview "github.com/webview/webview_go"

	"github.com/sstallion/go-hid"
)

// appVersion is set at build time with -X main.appVersion=...
var appVersion = "0.1.0"

const webView2URL = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"

func init() {
	// The window must be created and pumped on the main OS thread.
	runtime.LockOSThread()
}

func main() {
	sim := flag.Bool("simulate", false, "use a simulated A75 ISO instead of a real keyboard")
	flag.Parse()

	dir := appDir()
	setupLog(dir)
	log.Printf("DeerKB %s starting (simulate=%t)", appVersion, *sim)

	if !claimSingleInstance() {
		log.Printf("another DeerKB is already running")
		messageBox("DeerKB", "DeerKB is already running")
		return
	}

	if err := hid.Init(); err != nil {
		log.Printf("hid init: %v", err)
	}
	store, err := newProfileStore(filepath.Join(dir, "profiles"))
	if err != nil {
		log.Fatal(err)
	}

	kb := NewKeyboard(*sim, log.Printf)
	go runLogged("keyboard", kb.Run)

	quit := func() {
		log.Printf("quit: closing keyboard and exiting")
		kb.Close()
		_ = hid.Exit()
		os.Exit(0)
	}

	openApp(newMux(kb, store, quit))
	log.Printf("main: window closed")
	quit()
}

// runLogged runs fn and logs a panic. The release build has no console, so a panic's own output
// would be lost and the process would end with nothing in the log.
func runLogged(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic in %s: %v\n%s", name, r, debug.Stack())
		}
	}()
	fn()
}

// quitBinding is the Go side of the page's quit(). It stops the window's loop, and a panic is logged
// and returned as an error instead of ending the process.
func quitBinding(w webview.WebView) func() error {
	return func() (err error) {
		defer recoverBinding("quit", &err)
		log.Printf("quit: page asked to close the window")
		w.Terminate()
		return nil
	}
}

// recoverBinding turns a panic in a bound function into an error, after logging it with its stack.
func recoverBinding(name string, err *error) {
	if r := recover(); r != nil {
		log.Printf("panic in bound %s: %v\n%s", name, r, debug.Stack())
		*err = fmt.Errorf("%s failed: %v", name, r)
	}
}

// claimSingleInstance returns false when another DeerKB already holds the named mutex. The handle is
// never closed: it lasts for the life of the process.
func claimSingleInstance() bool {
	name, _ := syscall.UTF16PtrFromString(`Global\DeerKBSingleInstance`)
	createMutex := syscall.NewLazyDLL("kernel32.dll").NewProc("CreateMutexW")
	h, _, err := createMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		log.Printf("single instance mutex: %v", err)
		return true
	}
	return err != syscall.ERROR_ALREADY_EXISTS
}

// openApp shows the UI in a native window with no address bar or browser tabs. It blocks until the
// window closes. The window is a WebView2 surface, which Windows 10 and 11 ship with. The page talks to
// h in-process through the rpc binding, so the app opens no network port.
func openApp(h http.Handler) {
	page, err := webFS.ReadFile("web/index.html")
	if err != nil {
		log.Printf("read page: %v", err)
		return
	}
	log.Printf("window: before New")
	w := webview.New(false)
	if w == nil {
		log.Printf("WebView2 runtime not available")
		messageBox("DeerKB", "DeerKB needs the Microsoft Edge WebView2 Runtime, which is missing on this PC.\n\nInstall it from:\n"+webView2URL)
		return
	}
	defer w.Destroy()
	log.Printf("window: after New")
	w.SetTitle("DeerKB")
	w.SetSize(1520, 960, webview.HintNone)
	w.SetSize(1240, 780, webview.HintMin)
	// No right-click menu: it would show browser commands such as Reload and View Source.
	w.Init(`document.addEventListener("contextmenu", function (e) { e.preventDefault(); });`)
	w.Init(bridgeShim)
	log.Printf("window: after Init")
	if err := w.Bind("rpc", rpcBinding(h)); err != nil {
		log.Printf("bind rpc: %v", err)
	}
	if err := w.Bind("quit", quitBinding(w)); err != nil {
		log.Printf("bind quit: %v", err)
	}
	log.Printf("window: after Bind")
	w.SetHtml(string(page))
	log.Printf("window: before Run")
	w.Run()
	log.Printf("window: after Run returned")
}

// messageBox shows a Windows message box. It is used only when the window cannot start.
func messageBox(title, text string) {
	user32 := syscall.NewLazyDLL("user32.dll")
	msgBox := user32.NewProc("MessageBoxW")
	t, _ := syscall.UTF16PtrFromString(text)
	c, _ := syscall.UTF16PtrFromString(title)
	const mbOK, mbIconError = 0x0, 0x10
	_, _, _ = msgBox.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), mbOK|mbIconError)
}

// appDir is %APPDATA%\DeerKB on Windows. Profiles and the log live here.
func appDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "DeerKB")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// setupLog writes to a file only: the release build has no console.
func setupLog(dir string) {
	f, err := os.OpenFile(filepath.Join(dir, "deerkb.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		log.SetOutput(f)
	}
}
