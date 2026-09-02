package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"syscall"
	"time"

	"landrop/internal/server"
	"landrop/internal/storage"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		addr  = flag.String("addr", ":8080", "listen address")
		dir   = flag.String("dir", "./shared", "directory used to store shared files")
		maxMB = flag.Int64("max-mb", 4096, "maximum size of one uploaded file in MiB")
		token = flag.String("token", "", "access code; a random six-digit code is generated when empty")
		open  = flag.Bool("open", true, "open the share page in the default browser")
	)
	flag.Parse()

	if *maxMB <= 0 || *maxMB > 1024*1024 {
		fmt.Fprintln(os.Stderr, "-max-mb must be between 1 and 1048576")
		return 2
	}

	accessCode := *token
	if accessCode == "" {
		var err error
		accessCode, err = randomCode()
		if err != nil {
			fmt.Fprintf(os.Stderr, "generate access code: %v\n", err)
			return 1
		}
	}
	if !validAccessCode(accessCode) {
		fmt.Fprintln(os.Stderr, "-token must contain 4-64 letters, digits, hyphens, or underscores")
		return 2
	}

	store, err := storage.New(*dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open shared directory: %v\n", err)
		return 1
	}

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen on %s: %v\n", *addr, err)
		return 1
	}
	defer listener.Close()
	shareLinks := shareURLs(listener, accessCode)
	browserURL := preferredShareURL(shareLinks)

	app := server.New(server.Options{
		Store:    store,
		Token:    accessCode,
		MaxBytes: *maxMB * 1024 * 1024,
		ShareURL: browserURL,
		Logger:   slog.Default(),
	})
	httpServer := &http.Server{
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	absDir, _ := filepath.Abs(*dir)
	fmt.Println("bigbang 已启动")
	fmt.Printf("连接码：%s\n", accessCode)
	fmt.Printf("共享目录：%s\n", absDir)
	for _, shareURL := range shareLinks {
		fmt.Printf("打开：%s\n", shareURL)
	}
	fmt.Println("按 Ctrl+C 停止")

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- httpServer.Serve(listener)
	}()
	if *open && browserURL != "" {
		if err := openBrowser(browserURL); err != nil {
			fmt.Fprintf(os.Stderr, "自动打开浏览器失败，请手动打开上方地址：%v\n", err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err = <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			return 1
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "shutdown: %v\n", err)
			return 1
		}
	}

	return 0
}

func openBrowser(rawURL string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command = "open"
		args = []string{rawURL}
	case "windows":
		command = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", rawURL}
	default:
		command = "xdg-open"
		args = []string{rawURL}
	}
	return exec.Command(command, args...).Start()
}

func randomCode() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", binary.BigEndian.Uint32(b[:])%1_000_000), nil
}

func validAccessCode(code string) bool {
	if len(code) < 4 || len(code) > 64 {
		return false
	}
	for _, char := range code {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		return false
	}
	return true
}

func shareURLs(listener net.Listener, token string) []string {
	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return nil
	}

	port := strconv.Itoa(tcpAddr.Port)
	var ips []net.IP
	if tcpAddr.IP != nil && !tcpAddr.IP.IsUnspecified() {
		ips = append(ips, tcpAddr.IP)
	} else {
		interfaces, _ := net.Interfaces()
		for _, iface := range interfaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, _ := iface.Addrs()
			for _, addr := range addrs {
				ip, _, err := net.ParseCIDR(addr.String())
				if err == nil && ip.To4() != nil && (ip.IsPrivate() || ip.IsLinkLocalUnicast()) {
					ips = append(ips, ip)
				}
			}
		}
	}
	if len(ips) == 0 {
		ips = append(ips, net.ParseIP("127.0.0.1"))
	}

	seen := make(map[string]bool)
	urls := make([]string, 0, len(ips)+1)
	for _, ip := range ips {
		host := net.JoinHostPort(ip.String(), port)
		u := url.URL{Scheme: "http", Host: host, Path: "/"}
		query := u.Query()
		query.Set("token", token)
		u.RawQuery = query.Encode()
		if !seen[u.String()] {
			seen[u.String()] = true
			urls = append(urls, u.String())
		}
	}

	localhost := url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", port), Path: "/"}
	query := localhost.Query()
	query.Set("token", token)
	localhost.RawQuery = query.Encode()
	if !seen[localhost.String()] {
		urls = append(urls, localhost.String())
	}

	sort.Strings(urls)
	return urls
}

func preferredShareURL(urls []string) string {
	for _, rawURL := range urls {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			continue
		}
		host := parsed.Hostname()
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return rawURL
		}
	}
	if len(urls) != 0 {
		return urls[0]
	}
	return ""
}
