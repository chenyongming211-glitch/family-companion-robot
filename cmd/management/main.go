// management serves the independent management UI and onboarding API.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"familyrobot/internal/management"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func main() {
	if e := run(os.Args[1:], os.Stdout); e != nil {
		fmt.Fprintln(os.Stderr, "管理命令失败：请检查参数、私有配置和目录权限")
		os.Exit(1)
	}
}
func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return management.ErrInvalid
	}
	switch args[0] {
	case "init":
		return initialize(args[1:], out)
	case "migrate-legacy":
		return supplyFactory(args[1:], out, true)
	case "provision":
		return provision(args[1:], out)
	case "grant-device-add":
		return grantDeviceAdd(args[1:], out)
	case "publish-firmware":
		return publishFirmware(args[1:], out)
	case "serve":
		return serve(args[1:])
	default:
		return management.ErrInvalid
	}
}
func flags(name string) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	return f
}
func initialize(args []string, out io.Writer) error {
	f := flags("init")
	p := f.String("config", "", "new private config path")
	user := f.String("username", "owner", "account name")
	family := f.String("family", "我的家", "family name")
	device := f.String("device-id", "", "verified ESP32-S3 device ID")
	name := f.String("name", "小满", "device name")
	model := f.String("model", "微雪 ESP32-S3", "device model")
	location := f.String("location", "未设置", "device location")
	if f.Parse(args) != nil || f.NArg() != 0 || *p == "" {
		return management.ErrInvalid
	}
	if _, e := os.Lstat(*p); !os.IsNotExist(e) {
		return management.ErrInvalid
	}
	raw := make([]byte, 32)
	if _, e := rand.Read(raw); e != nil {
		return e
	}
	password := base64.RawURLEncoding.EncodeToString(raw)
	hash, e := management.HashPassword(password)
	if e != nil {
		return e
	}
	now := time.Now().UTC()
	c := management.Config{SchemaVersion: 1, Accounts: []management.AccountConfig{{ID: "owner-user", Username: *user, Password: hash, InternalAdmin: true, Memberships: []management.Membership{{FamilyID: "family-home", Devices: []management.DeviceGrant{{DeviceID: *device, CanReadBody: true}}}}}}, Families: []management.FamilyConfig{{ID: "family-home", Name: *family}}, Devices: []management.DeviceConfig{{ID: *device, Name: *name, Model: *model, Location: *location}}, Bindings: []management.Binding{{ID: "binding-home-1", Version: 1, FamilyID: "family-home", DeviceID: *device, From: now}}}
	if management.ValidateConfig(c) != nil {
		return management.ErrInvalid
	}
	dir := filepath.Dir(*p)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	st, e := os.Lstat(dir)
	if e != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
		return management.ErrInvalid
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		return e
	}
	defer root.Close()
	file, e := root.OpenFile(filepath.Base(*p), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	ok := false
	defer func() {
		file.Close()
		if !ok {
			_ = root.Remove(filepath.Base(*p))
		}
	}()
	if e = json.NewEncoder(file).Encode(c); e != nil {
		return e
	}
	if e = file.Sync(); e != nil {
		return e
	}
	if e = file.Close(); e != nil {
		return e
	}
	parent, e := os.Open(dir)
	if e != nil {
		return e
	}
	e = parent.Sync()
	parent.Close()
	if e != nil {
		return e
	}
	ok = true
	return json.NewEncoder(out).Encode(struct {
		Username string    `json:"username"`
		Password string    `json:"password"`
		From     time.Time `json:"binding_from"`
	}{*user, password, now})
}
func validListen(addr string) bool {
	host, port, e := net.SplitHostPort(addr)
	if e != nil {
		return false
	}
	ip := net.ParseIP(host)
	n, e := strconv.Atoi(port)
	return e == nil && n > 0 && n <= 65535 && ip != nil && ip.IsLoopback()
}
func serve(args []string) error {
	f := flags("serve")
	listen := f.String("listen", "127.0.0.1:18791", "loopback listener")
	config := f.String("config", "", "private config path")
	records := f.String("records", "", "existing records root")
	profiles := f.String("profiles", "", "optional profiles file")
	static := f.String("static", "", "live static dist")
	audit := f.String("audit", "", "private audit file")
	firmwareRoot := f.String("firmware-root", "", "optional private firmware release and task directory")
	musicLibrary := f.String("music-library", "", "optional private music library directory")
	memorySocket := f.String("memory-control-socket", "", "optional private voice memory control Unix socket")
	publicOrigin := f.String("public-origin", "", "explicit public HTTPS origin")
	build := f.String("build", "unknown", "build identifier")
	if f.Parse(args) != nil || f.NArg() != 0 || !validListen(*listen) || *config == "" || *records == "" || *static == "" || *audit == "" {
		return management.ErrInvalid
	}
	for _, p := range []string{*records, *static} {
		st, e := os.Stat(p)
		if e != nil || !st.IsDir() {
			return management.ErrInvalid
		}
	}
	st, e := os.Lstat(filepath.Join(*static, "index.html"))
	if e != nil || !st.Mode().IsRegular() {
		return management.ErrInvalid
	}
	c, e := management.LoadConfig(*config)
	if e != nil {
		return e
	}
	handler, e := management.NewServer(c, management.Options{RecordsPath: *records, ProfilesPath: *profiles, StaticDir: *static, AuditPath: *audit, Build: *build, ConfigPath: *config, PublicOrigin: *publicOrigin, MusicLibraryRoot: *musicLibrary, FirmwareRoot: *firmwareRoot, MemoryControlSocket: *memorySocket})
	if e != nil {
		return e
	}
	ln, e := net.Listen("tcp", *listen)
	if e != nil {
		return e
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if srv.Shutdown(shutdown) != nil {
				_ = srv.Close()
			}
		case <-done:
		}
	}()
	e = srv.Serve(ln)
	close(done)
	<-stopped
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}

func publishFirmware(args []string, out io.Writer) error {
	f := flags("publish-firmware")
	root := f.String("root", "", "private firmware directory")
	manifest := f.String("manifest", "", "verified release manifest")
	app := f.String("app", "", "verified ESP32-S3 application image")
	partition := f.String("partition", "", "verified binary partition table")
	if f.Parse(args) != nil || f.NArg() != 0 || *root == "" || *manifest == "" || *app == "" || *partition == "" {
		return management.ErrInvalid
	}
	id, e := management.PublishFirmware(*root, *manifest, *app, *partition)
	if e != nil {
		return e
	}
	return json.NewEncoder(out).Encode(map[string]string{"releaseId": id})
}
