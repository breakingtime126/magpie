package nativelaunch

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/nativebootstrap"
	"github.com/yetone/magpie/internal/nativechannel"
	"github.com/yetone/magpie/internal/nativeprofile"
	"golang.org/x/sys/windows"
)

// Only this copied test executable participates. No installed runtime, account,
// external model request or product identity bypass is involved.
func init() {
	if !strings.HasPrefix(filepath.Base(os.Args[0]), "launch-family-fixture") || len(os.Args) < 2 {
		return
	}
	if os.Args[1] == "fixture-leaf" {
		for {
			time.Sleep(time.Hour)
		}
	}
	if os.Args[1] != "app-server" {
		os.Exit(21)
	}
	role := "primary"
	for _, arg := range os.Args {
		if strings.Contains(arg, `model_provider="magpie_shared_aihub"`) {
			role = "secondary"
		}
	}
	leaf := exec.Command(os.Args[0], "fixture-leaf")
	leaf.Stdout = os.Stdout
	if err := leaf.Start(); err != nil {
		os.Exit(22)
	}
	data, _ := json.Marshal([]int{os.Getpid(), leaf.Process.Pid})
	if os.WriteFile(filepath.Join(os.Getenv("HOME"), role+".family"), data, 0600) != nil {
		os.Exit(23)
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var msg struct {
			ID json.RawMessage `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			os.Exit(24)
		}
		// Only initialize is needed to release serial secondary startup.
		// Later barrier requests deliberately remain pending until shutdown.
		if msg.Method == "initialize" && len(msg.ID) > 0 {
			fmt.Printf("{\"id\":%s,\"result\":{\"userAgent\":\"fixture\",\"platformFamily\":\"windows\",\"platformOs\":\"windows\"}}\n", msg.ID)
		}
	}
	// Exercise forced owned-Job termination, not only cooperative EOF.
	for {
		time.Sleep(time.Hour)
	}
}

func launchFamilyHandle(t *testing.T, pid int) windows.Handle {
	t.Helper()
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		state, err := windows.WaitForSingleObject(h, 0)
		if err == nil && state == uint32(windows.WAIT_TIMEOUT) {
			_ = windows.TerminateProcess(h, 1)
			_, _ = windows.WaitForSingleObject(h, 5000)
		}
		_ = windows.CloseHandle(h)
	})
	if state, err := windows.WaitForSingleObject(h, 0); err != nil || state != uint32(windows.WAIT_TIMEOUT) {
		t.Fatal("fixture was not alive before shutdown", state, err)
	}
	return h
}

func launchFamilyWait(t *testing.T, home, role string) []windows.Handle {
	t.Helper()
	deadline := time.Now().Add(20*time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(filepath.Join(home, role+".family"))
		var ids []int
		if err == nil && json.Unmarshal(data, &ids) == nil && len(ids) == 2 && ids[0] > 0 && ids[1] > 0 {
			return []windows.Handle{launchFamilyHandle(t, ids[0]), launchFamilyHandle(t, ids[1])}
		}
		time.Sleep(10*time.Millisecond)
	}
	t.Fatal("synthetic backend family not ready", role)
	return nil
}

// Covers the production launcher run/real pipe lease/real owned endpoint seam.
// Peer discovery is injected solely to trust this test executable. This is not
// an installed GUI or packaged-launcher provenance acceptance test.
func TestWindowsLaunchLeaseDrainsBackendFamilies(t *testing.T) {
	for _, stop := range []string{"client-eof", "host-lease-close"} {
		t.Run(stop, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			root := t.TempDir()
			home, codex, control := filepath.Join(root,"home"), filepath.Join(root,"native"), filepath.Join(root,"control")
			for _, dir := range []string{home,codex,control} {
				if err := os.Mkdir(dir,0700); err != nil { t.Fatal(err) }
			}
			t.Setenv("HOME",home)
			t.Setenv("USERPROFILE",home)
			self, err := os.Executable()
			if err != nil { t.Fatal(err) }
			exe, err := os.ReadFile(self)
			if err != nil { t.Fatal(err) }
			command := filepath.Join(root,"launch-family-fixture.exe")
			if err := os.WriteFile(command,exe,0700); err != nil { t.Fatal(err) }
			digest := sha256.Sum256(exe)
			catalog := filepath.Join(control,"synthetic-catalog.json")
			if err := os.WriteFile(catalog,[]byte("{}"),0600); err != nil { t.Fatal(err) }
			cfg := appserverbridge.Config{
				ControlPlane: appserverbridge.ControlPlaneRouting167, ControlHome: control,
				SharedNativeRoute: &appserverbridge.SharedNativeRoute167{Mode: appserverbridge.SharedNativeHome167, GatewayURL:"http://127.0.0.1:1/v1", GatewayEnvKey:"MAGPIE_SYNTHETIC_GATEWAY_CALLER",GatewayAuth:appserverbridge.SharedNativeLocalGateway167,SecondaryCatalog:catalog},
			}
			for i, id := range []string{"primary","secondary"} {
				provider := "openai"
				if i==1 { provider="magpie_shared_aihub" }
				cfg.Backends=append(cfg.Backends,appserverbridge.Backend{ID:id,Provider:provider,Command:command,Home:home,CodexHome:codex,StartupProbeSHA256:hex.EncodeToString(digest[:])})
			}
			var nonce [16]byte
			if _,err := rand.Read(nonce[:]); err != nil { t.Fatal(err) }
			profile, err := nativeprofile.New(hex.EncodeToString(nonce[:]),cfg)
			if err != nil { t.Fatal(err) }
			session,err := nativechannel.Start(ctx,http.HandlerFunc(func(http.ResponseWriter,*http.Request){ t.Error("unexpected model request") }))
			if err != nil { t.Fatal(err) }
			defer func(){ cleanup,done:=context.WithTimeout(context.Background(),5*time.Second);defer done();if err:=session.Close(cleanup);err!=nil {t.Error(err)} }()
			ref,err := nativechannel.Publish(filepath.Join(control,"publication.json"),session.Descriptor())
			if err != nil { t.Fatal(err) }
			record:=nativebootstrap.Record{Version:1,Generation:profile.Generation(),ProfileSHA256:profile.SHA256(),Reference:ref}
			peer:=nativebootstrap.Peer{Path:self}
			owner,err:=nativebootstrap.Listen(record,peer)
			if err != nil { t.Fatal(err) }
			defer owner.Close()
			type accepted struct { lease *nativebootstrap.Lease; err error }
			acceptedCh:=make(chan accepted,1)
			go func(){ l,e:=owner.Accept(ctx);acceptedCh<-accepted{l,e} }()
			in,writer:=io.Pipe()
			out,reader:=io.Pipe()
			defer writer.Close()
			defer out.Close()
			go func(){ _,_=io.Copy(io.Discard,out) }()
			done:=make(chan error,1)
			go func(){
				done<-run(ctx,bytes.NewReader(profile.Bytes()),appserverbridge.LaunchOptions{},in,reader,dependencies{
					connect:func(c context.Context,g string)(lease,error){return nativebootstrap.Connect(c,g,peer)},
					allocate:allocateEndpoint,
				})
			}()
			var host *nativebootstrap.Lease
			select {
			case a:=<-acceptedCh:
				if a.err!=nil {t.Fatal(a.err)};host=a.lease
			case <-ctx.Done():t.Fatal("lease accept timed out")
			}
			defer func(){ cleanup,finish:=context.WithTimeout(context.Background(),5*time.Second);defer finish();if err:=host.Close(cleanup);err!=nil {t.Error(err)} }()
			if _,err:=fmt.Fprintln(writer,`{"id":1,"method":"initialize","params":{"clientInfo":{"name":"family-fixture","version":"1"}}}`);err!=nil {t.Fatal(err)}
			handles:=launchFamilyWait(t,home,"primary")
			handles=append(handles,launchFamilyWait(t,home,"secondary")...)
			if stop=="client-eof" { _=writer.Close() } else {
				cleanup,finish:=context.WithTimeout(context.Background(),5*time.Second)
				err:=host.Close(cleanup);finish()
				if err!=nil {t.Fatal(err)}
			}
			select {
			case err:=<-done:
				if stop=="client-eof" && err!=nil {t.Fatal("EOF cleanup failed",err)}
				if stop=="host-lease-close" && !errors.Is(err,context.Canceled) {t.Fatal("lease revocation not returned",err)}
			case <-ctx.Done():t.Fatal("launcher did not finish cleanup")
			}
			for _,h:=range handles {
				if state,err:=windows.WaitForSingleObject(h,10000);err!=nil || state!=windows.WAIT_OBJECT_0 {
					t.Error("backend family survived launcher cleanup",state,err)
				}
			}
		})
	}
}
