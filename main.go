// io7 — a local, graph-backed text editor. Running the binary starts a
// loopback-only HTTP server and opens the frontend in the default browser.
// Stop with Ctrl-C or POST /shutdown. See IOX_PLAN.md for the architecture.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/W-Garlington/io7/server"
	"github.com/W-Garlington/io7/store"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:7117", "listen address (keep this loopback-only)")
	dataDir := flag.String("data", defaultDataDir(), "directory for the database")
	noBrowser := flag.Bool("no-browser", false, "do not open the browser on start")
	flag.Parse()

	if err := run(*addr, *dataDir, *noBrowser); err != nil {
		log.Fatal(err)
	}
}

func run(addr, dataDir string, noBrowser bool) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(dataDir, "graph.db"))
	if err != nil {
		return err
	}
	defer st.Close()

	// stop doubles as the /shutdown handler's trigger: both Ctrl-C and the
	// endpoint cancel the same context, taking the same graceful path.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := server.New(st, stop)
	return srv.Run(ctx, addr, func(listenAddr string) {
		url := "http://" + listenAddr
		log.Printf("io7 serving at %s (data: %s)", url, dataDir)
		if !noBrowser {
			openBrowser(url)
		}
	})
}

func defaultDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "io7")
	}
	return "io7-data"
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("could not open browser (open %s manually): %v", url, err)
	}
}
