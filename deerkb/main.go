package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
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
	port := flag.Int("port", 7842, "localhost port for the app's internal server")
	headless := flag.Bool("headless", false, "run the server without the window (for tests)")
	flag.Parse()

	dir := appDir()
	setupLog(dir)
	log.Printf("DeerKB %s starting (simulate=%t)", appVersion, *sim)

	if err := hid.Init(); err != nil {
		log.Printf("hid init: %v", err)
	}
	store, err := newProfileStore(filepath.Join(dir, "profiles"))
	if err != nil {
		log.Fatal(err)
	}

	kb := NewKeyboard(*sim, log.Printf)
	go kb.Run()

	quit := func() {
		kb.Close()
		_ = hid.Exit()
		os.Exit(0)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	url := "http://" + addr + "/"
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// Most likely a copy is already running and serving the same window.
		log.Printf("listen: %v", err)
		if *headless {
			return
		}
	} else {
		srv := &http.Server{Handler: newMux(kb, store, quit, *port), ReadHeaderTimeout: 5 * time.Second}
		go func() { log.Printf("http: %v", srv.Serve(ln)) }()
	}

	if *headless {
		fmt.Println("DeerKB running at", url)
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt)
		<-sig
		quit()
	}

	openApp(url)
	quit()
}

// openApp shows the UI in a native window with no address bar or browser tabs. It blocks until the
// window closes. The window is a WebView2 surface, which Windows 10 and 11 ship with.
func openApp(url string) {
	w := webview.New(false)
	if w == nil {
		log.Printf("WebView2 runtime not available")
		messageBox("DeerKB", "DeerKB needs the Microsoft Edge WebView2 Runtime, which is missing on this PC.\n\nInstall it from:\n"+webView2URL)
		return
	}
	defer w.Destroy()
	w.SetTitle("DeerKB")
	w.SetSize(1180, 800, webview.HintNone)
	w.SetSize(960, 640, webview.HintMin)
	// No right-click menu: it would show browser commands such as Reload and View Source.
	w.Init(`document.addEventListener("contextmenu", function (e) { e.preventDefault(); });`)
	w.Navigate(url)
	w.Run()
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
