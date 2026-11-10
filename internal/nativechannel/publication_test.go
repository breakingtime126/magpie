package nativechannel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func publicationDir(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func publicationDescriptor(t *testing.T) Descriptor {
	t.Helper()
	_, der, err := newCertificate()
	if err != nil {
		t.Fatal(err)
	}
	return Descriptor{URL: "https://127.0.0.1:34567", CertificateDER: der}
}

func trustedFixtureReference(path string, data []byte) Reference {
	sum := sha256.Sum256(data)
	return Reference{Path: path, SHA256: hex.EncodeToString(sum[:])}
}

func TestPublicationRoundTripAndExplicitRecovery(t *testing.T) {
	s := session(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("fixture response")) }))
	path := filepath.Join(publicationDir(t), "channel.json")
	reference, err := Publish(path, s.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		descriptor, err := ReadPublication(reference)
		if err != nil {
			t.Fatal(err)
		}
		c := client(t, descriptor)
		response, err := c.Do(request(t, descriptor.URL, nil))
		if err != nil {
			t.Fatal(err)
		}
		if got := readResponse(t, response); got != "fixture response" {
			t.Fatal(got)
		}
		c.Close()
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	descriptor, err := ReadPublication(reference)
	if err != nil {
		t.Fatal("public artifact recovery is not a liveness probe", err)
	}
	c := client(t, descriptor)
	if response, err := c.Do(request(t, descriptor.URL, nil)); err == nil {
		response.Body.Close()
		t.Fatal("publication recovery revived dead session")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("PRIVATE")) || bytes.Contains(data, []byte("token")) {
		t.Fatal("publication contains unexpected secret material")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatal("unexpected publication permissions")
	}
}

func TestPublishNeverOverwritesAndConcurrentWritersHaveOneWinner(t *testing.T) {
	path := filepath.Join(publicationDir(t), "channel.json")
	descriptor := publicationDescriptor(t)
	var successes atomic.Int64
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Publish(path, descriptor); err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful exclusive writers: %d", successes.Load())
	}
	before, _ := os.ReadFile(path)
	if _, err := Publish(path, publicationDescriptor(t)); err == nil {
		t.Fatal("overwrote existing publication")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("existing publication changed")
	}
}

func TestPublicationReplacementAndPartialWritesFailWithoutIndependentNewTrust(t *testing.T) {
	path := filepath.Join(publicationDir(t), "channel.json")
	reference, err := Publish(path, publicationDescriptor(t))
	if err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	replacement := publicationDescriptor(t)
	other, _ := json.Marshal(publication{Version: 1, URL: replacement.URL, CertificateDER: replacement.CertificateDER})
	for _, data := range [][]byte{other, original[:len(original)/2], nil, bytes.Repeat([]byte("x"), publicationLimit+1)} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		// A matching self-asserted sidecar is deliberately irrelevant.
		sidecar, _ := json.Marshal(trustedFixtureReference(path, data))
		if err := os.WriteFile(path+".manifest", sidecar, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadPublication(reference); err == nil {
			t.Fatal("accepted replaced or partial publication")
		}
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPublication(reference); err != nil {
		t.Fatal("could not explicitly recover exact original bytes", err)
	}
}

func TestPublicationSchemaStrictEvenWhenFixtureDigestMatches(t *testing.T) {
	path := filepath.Join(publicationDir(t), "channel.json")
	descriptor := publicationDescriptor(t)
	valid, _ := json.Marshal(publication{Version: 1, URL: descriptor.URL, CertificateDER: descriptor.CertificateDER})
	for _, data := range [][]byte{
		[]byte(strings.Replace(string(valid), `"version":1`, `"version":2`, 1)),
		[]byte(strings.Replace(string(valid), `"version":1`, `"version":1,"version":1`, 1)),
		[]byte(strings.Replace(string(valid), `"version":1`, `"version":1,"pid":1234`, 1)),
		append(bytes.Clone(valid), []byte(`{}`)...),
		[]byte(strings.Replace(string(valid), "https://127.0.0.1", "http://127.0.0.1", 1)),
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadPublication(trustedFixtureReference(path, data)); err == nil {
			t.Fatal("accepted unsupported publication schema")
		}
	}
}

func TestPublicationRejectsPathsAndLinkedComponents(t *testing.T) {
	descriptor := publicationDescriptor(t)
	root := publicationDir(t)
	for _, path := range []string{"relative.json", filepath.Join(root, "missing", "channel.json"), filepath.Join(root, "channel:stream"), root + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "channel.json"} {
		if _, err := Publish(path, descriptor); err == nil {
			t.Fatal("accepted unsupported path", path)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "missing")); !os.IsNotExist(err) {
		t.Fatal("created missing publication directory")
	}
	path := filepath.Join(root, "original.json")
	reference, err := Publish(path, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.json")
	if err := os.Symlink(path, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("Windows symlink creation privilege unavailable")
		}
		t.Fatal(err)
	}
	reference.Path = link
	if _, err := ReadPublication(reference); err == nil {
		t.Fatal("followed file symlink")
	}
	if _, err := Publish(link, descriptor); err == nil {
		t.Fatal("published through file symlink")
	}
	linkedDir := filepath.Join(root, "linked-dir")
	if err := os.Symlink(root, linkedDir); err != nil {
		t.Fatal(err)
	}
	reference.Path = filepath.Join(linkedDir, "original.json")
	if _, err := ReadPublication(reference); err == nil {
		t.Fatal("followed linked parent")
	}
	if _, err := Publish(filepath.Join(linkedDir, "new.json"), descriptor); err == nil {
		t.Fatal("published through linked parent")
	}
}

func TestPublicationInvalidTrustCreatesNothing(t *testing.T) {
	root := publicationDir(t)
	path := filepath.Join(root, "channel.json")
	if _, err := Publish(path, Descriptor{URL: "https://127.0.0.1:1234", CertificateDER: []byte("invalid")}); err == nil {
		t.Fatal("published malformed trust")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("invalid descriptor wrote state", err)
	}
	for _, digest := range []string{"", "bad", strings.Repeat("a", 62), strings.Repeat("A", 64)} {
		if _, err := ReadPublication(Reference{Path: path, SHA256: digest}); err == nil {
			t.Fatal("accepted malformed expected hash")
		}
	}
}
