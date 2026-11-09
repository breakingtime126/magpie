// Package nativepreview owns disposable, separate native Codex contexts. It is
// not the bridge or ordinary Magpie startup and never imports either migration.
package nativepreview

import (
 "context"
 "crypto/sha256"
 "errors"
 "io"
 "net/url"
 "os"
 "path/filepath"
 "runtime"
 "strings"
 "sync"
 "unicode"
 "unicode/utf8"
)

var (
 ErrUnavailable = errors.New("native preview unavailable")
 ErrBusy = errors.New("native preview busy")
 ErrUnsupported = errors.New("native preview operation unsupported")
)

type Model struct { ID string `json:"id"`; Name string `json:"name"` }
type Reply struct { Text string `json:"text"` }
type State struct {
 ExecutableSelected bool `json:"executableSelected"`
 AIHubConfigured bool `json:"aiHubConfigured"`
 Busy bool `json:"busy"`
 CanDiscover bool `json:"canDiscover"`
 CanLogin bool `json:"canLogin"`
 CanSend bool `json:"canSend"`
 Blocker string `json:"blocker"`
}

type Controller struct {
 mu sync.Mutex
 root, executable, endpoint, key string
 primaryHome, primaryCodexHome string
 digest [32]byte
 cancel context.CancelFunc
 done chan struct{}
 closed bool
 models map[string]map[string]bool
 sendCompatible bool
}

// New creates only new application-owned directories. Close does not remove
// native state or revoke authentication. Homes are never selected by the GUI.
func New() (*Controller, error) {
	parent := os.TempDir()
	if !safeStateParent(parent) {
		return nil, ErrUnavailable
	}
	root, err := os.MkdirTemp(parent, "magpie-native-preview-")
	if err != nil {
		return nil, ErrUnavailable
	}
	for _, provider := range []string{"openai", "aihub"} {
		for _, dir := range []string{"home", "codex"} {
			if os.MkdirAll(filepath.Join(root, provider, dir), 0700) != nil {
				return nil, ErrUnavailable
			}
		}
	}
	return &Controller{root: root, primaryHome: filepath.Join(root, "openai", "home"), primaryCodexHome: filepath.Join(root, "openai", "codex")}, nil
}

func (c *Controller) State() State {
 c.mu.Lock(); defer c.mu.Unlock()
 selected := c.executable != "" && !c.closed
 canSend:=selected && c.cancel==nil && c.sendCompatible && (len(c.models["openai"])>0 || len(c.models["aihub"])>0)
 blocker := "discover_models_first"
 if len(c.models["openai"])+len(c.models["aihub"])>0 && !c.sendCompatible{blocker="native_version_does_not_support_guarded_text"}
 if canSend{blocker="guarded_short_text_preview"}
 if !nativeLoginAllowed() { blocker = "windows_job_login_unavailable" }
 return State{ExecutableSelected: selected, AIHubConfigured: c.key != "" && !c.closed,
  Busy: c.cancel != nil, CanDiscover: selected && c.cancel == nil, CanLogin: selected && c.cancel == nil && nativeLoginAllowed(),
  CanSend:canSend, Blocker: blocker}
}

func (c *Controller) SelectExecutable(path string) error {
 c.mu.Lock(); defer c.mu.Unlock()
 if c.closed || c.cancel != nil { return ErrBusy }
 if !filepath.IsAbs(path) { return ErrUnavailable }
 resolved, err := filepath.EvalSymlinks(path)
 if err != nil { return ErrUnavailable }
 digest, err := executableDigest(resolved)
 if err != nil { return ErrUnavailable }
 c.executable, c.digest = resolved, digest
 c.models=nil
 c.sendCompatible=false
 return nil
}

func executableDigest(path string) ([32]byte, error) {
	var result [32]byte
	if runtime.GOOS == "windows" && !strings.EqualFold(filepath.Ext(path), ".exe") {
		return result, ErrUnavailable
	}
	file, err := os.Open(path)
	if err != nil {
		return result, ErrUnavailable
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() <= 0 || stat.Size() > 512<<20 {
		return result, ErrUnavailable
	}
	hash := sha256.New()
	if n, err := io.Copy(hash, io.LimitReader(file, (512<<20)+1)); err != nil || n > 512<<20 {
		return result, ErrUnavailable
	}
	copy(result[:], hash.Sum(nil))
	return result, nil
}

// ConfigureAIHub keeps the URL and secret only in controller memory. The key
// is supplied only to the AIHub child environment, never argv or GUI state.
func (c *Controller) ConfigureAIHub(endpoint, key string) error {
 c.mu.Lock(); defer c.mu.Unlock()
 if c.closed || c.cancel != nil { return ErrBusy }
 u, err := url.Parse(endpoint)
 if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
  len(endpoint) > 2048 || !cleanText(endpoint) || strings.TrimSpace(key) == "" || len(key) > 8192 || !cleanText(key) { return ErrUnavailable }
 c.endpoint, c.key = endpoint, key
 delete(c.models,"aihub")
 return nil
}

func cleanText(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return !strings.ContainsRune(s, '\x00')
}

func (c *Controller) Discover(parent context.Context, provider string) ([]Model, error) {
 c.mu.Lock()
 if c.closed || c.cancel != nil { c.mu.Unlock(); return nil, ErrBusy }
 if c.executable == "" || (provider != "openai" && provider != "aihub") || (provider == "aihub" && c.key == "") {
  c.mu.Unlock(); return nil, ErrUnavailable
 }
 ctx, cancel := context.WithCancel(parent)
 c.cancel = cancel
 c.done = make(chan struct{})
 spec := peerSpec{command: c.executable, digest: c.digest, home: filepath.Join(c.root, provider, "home"), codexHome: filepath.Join(c.root, provider, "codex"), provider: provider}
 if provider == "aihub" { spec.endpoint, spec.key = c.endpoint, c.key } else { spec.home, spec.codexHome = c.primaryHome, c.primaryCodexHome }
 c.mu.Unlock()
 defer func() { cancel(); c.mu.Lock(); c.cancel = nil; close(c.done); c.done = nil; c.mu.Unlock() }()
 models,version,err:=discover(ctx,spec)
 c.mu.Lock()
 delete(c.models,provider)
 if err==nil && !c.closed {
  c.sendCompatible=version=="codex-cli 0.167.0-alpha.3.2"
  if c.models==nil{c.models=map[string]map[string]bool{}}
  c.models[provider]=map[string]bool{}
  for _,model:=range models{c.models[provider][model.ID]=true}
 }
 c.mu.Unlock()
 return models,err
}

// Login is an explicit native browser handoff into a fresh dedicated context.
// Native login can revoke/clear authentication according to effective managed
// storage policy; a fresh directory does not prove an empty system keyring.
// No OAuth output or credential is captured. Success is native exit only.
func (c *Controller) Login(parent context.Context) error {
 c.mu.Lock()
 if c.closed || c.cancel != nil { c.mu.Unlock(); return ErrBusy }
 if c.executable == "" || !nativeLoginAllowed() { c.mu.Unlock(); return ErrUnavailable }
 if _, present := os.LookupEnv("CODEX_WINDOWS_REGISTERED_CORE"); present { c.mu.Unlock(); return ErrUnavailable }
 if !safeStateParent(c.root) { c.mu.Unlock(); return ErrUnavailable }
 fresh, err := os.MkdirTemp(c.root,"openai-login-")
 if err != nil { c.mu.Unlock(); return ErrUnavailable }
 home, codexHome := filepath.Join(fresh,"home"), filepath.Join(fresh,"codex")
 if os.Mkdir(home,0700)!=nil || os.Mkdir(codexHome,0700)!=nil { c.mu.Unlock(); return ErrUnavailable }
 c.primaryHome, c.primaryCodexHome = home, codexHome
 delete(c.models,"openai")
 ctx, cancel := context.WithCancel(parent)
 c.cancel, c.done = cancel, make(chan struct{})
 spec := peerSpec{command:c.executable,digest:c.digest,home:home,codexHome:codexHome,provider:"openai"}
 c.mu.Unlock()
 defer func(){cancel();c.mu.Lock();c.cancel=nil;close(c.done);c.done=nil;c.mu.Unlock()}()
 return login(ctx,spec)
}
// Send is one explicit short-text request in a new ephemeral thread. A cached
// provider-owned model selection is required and revalidated in the native
// process that will execute it. No resume, tools approval or fallback provider.
func (c *Controller) Send(parent context.Context,provider,model,text string)(Reply,error){
 c.mu.Lock()
 if c.closed || c.cancel!=nil{c.mu.Unlock();return Reply{},ErrBusy}
 if c.executable=="" || !c.sendCompatible || !c.models[provider][model] || (provider!="openai" && provider!="aihub") ||
  (provider=="aihub" && (c.key=="" || strings.Contains(text,c.key))) || strings.TrimSpace(text)=="" || len(text)>4096 || !utf8.ValidString(text) || strings.ContainsRune(text,'\x00'){
  c.mu.Unlock();return Reply{},ErrUnavailable
 }
 ctx,cancel:=context.WithCancel(parent);c.cancel,c.done=cancel,make(chan struct{})
 spec:=peerSpec{command:c.executable,digest:c.digest,home:filepath.Join(c.root,provider,"home"),codexHome:filepath.Join(c.root,provider,"codex"),provider:provider,only167:true}
 if provider=="aihub"{spec.endpoint,spec.key=c.endpoint,c.key}else{spec.home,spec.codexHome=c.primaryHome,c.primaryCodexHome}
 c.mu.Unlock()
 defer func(){cancel();c.mu.Lock();c.cancel=nil;close(c.done);c.done=nil;c.mu.Unlock()}()
 return sendText(ctx,spec,model,text)
}

func (c *Controller) Cancel() { c.mu.Lock(); defer c.mu.Unlock(); if c.cancel != nil { c.cancel() } }
func (c *Controller) Close() error {
 c.mu.Lock()
 c.closed = true
 if c.cancel != nil { c.cancel() }
 done := c.done
 c.key, c.endpoint = "", ""
 c.models=nil
 c.sendCompatible=false
 c.mu.Unlock()
 if done != nil { <-done }
 return nil
}

// AutoSelectExecutable performs only a supported read-only installation query.
// It never downloads, registers, repairs, or launches the discovered native CLI.
func (c *Controller) AutoSelectExecutable(parent context.Context) error {
 c.mu.Lock()
 if c.closed || c.cancel!=nil { c.mu.Unlock(); return ErrBusy }
 ctx,cancel:=context.WithCancel(parent)
 c.cancel,c.done=cancel,make(chan struct{})
 c.mu.Unlock()
 defer func(){cancel();c.mu.Lock();c.cancel=nil;close(c.done);c.done=nil;c.mu.Unlock()}()
 path,err:=installedExecutable(ctx)
 if err!=nil || !filepath.IsAbs(path){return ErrUnavailable}
 resolved,err:=filepath.EvalSymlinks(path)
 if err!=nil{return ErrUnavailable}
 digest,err:=executableDigest(resolved)
 if err!=nil || ctx.Err()!=nil{return ErrUnavailable}
 c.mu.Lock();defer c.mu.Unlock()
 if c.closed{return ErrUnavailable}
 c.executable,c.digest=resolved,digest
 c.models=nil
 c.sendCompatible=false
 return nil
}
