// CS2 Font Studio: local web server + browser UI.
// Only writes conf.d\99-spddl.conf and copies the chosen fonts to csgo\panorama\fonts.
package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
)

//go:embed all:web
var webFS embed.FS

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	noOpen := flag.Bool("no-open", false, "do not open the browser automatically")
	port := flag.Int("port", 0, "port (0 = pick a free one)")
	flag.Parse()

	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}

	if err := writeBuiltinFonts(sub, "fonts"); err != nil {
		log.Println("stand-in fonts:", err) // only the "Preview fonts" preset needs them
	}

	mux := http.NewServeMux()
	registerAPI(mux)
	mux.Handle("/", http.FileServer(http.FS(sub)))

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		log.Fatal(err)
	}
	url := "http://" + ln.Addr().String() + "/"
	fmt.Println("CS2 Font Studio:", url)
	if !*noOpen {
		if err := openBrowser(url); err != nil {
			// keep running: the address works in any browser, it just has to be opened by hand
			msg := fmt.Sprintf("Cannot open the browser: %v.\n\nOpen this address in your browser yourself:\n%s\n\nTo open it automatically next time, set a default browser in Windows (Settings > Apps > Default apps).", err, url)
			fmt.Fprintln(os.Stderr, msg)
			go showError("CS2 Font Studio", msg) // the message box blocks until it is closed; the server starts meanwhile
		} else {
			minimizeConsoleSoon() // the console stays available, but gets out of the way
		}
	}

	// The server runs until the program itself is closed (close this console window or press Ctrl+C).
	// It does not try to guess whether a browser tab is still open: browsers throttle background tabs,
	// so any "is the tab alive" signal can be wrong.
	fmt.Println("Running. Close this window to quit.")
	log.Fatal(http.Serve(ln, localOnly(ln.Addr().String(), mux)))
}

// localOnly rejects requests that do not come from the app's own page: a Host other than the listen address
// (DNS rebinding) and writes whose Origin is a foreign web page (browsers send Origin with every cross-site POST).
func localOnly(addr string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != addr {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); r.Method != http.MethodGet && r.Method != http.MethodHead && o != "" && o != "http://"+addr {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}
