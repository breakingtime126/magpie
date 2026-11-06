package main

import (
	"bytes"
	"strings"
	"testing"
)

// This behavioral fixture also compiles against the exact pre-recognition
// observer. It deliberately uses only its existing event fields and helpers.
func TestSchemaMetadataRecognizesKnownSpelling(t *testing.T) {
	for _, arg := range []string{
		" model = SYNTHETIC_SECRET",
		"\tmodel\r\n=SYNTHETIC_SECRET",
		"\v\fmodel\v\f=SYNTHETIC_SECRET",
	} {
		var log bytes.Buffer
		args := []string{"-c", arg}
		o := newObserver(&log)
		o.argv(args)
		es := events(t, log.Bytes())
		if len(es) != 2 || es[1].ConfigKey != "model" || es[1].ConfigValue != "other" {
			t.Fatal("known schema key spelling was not recognized opaquely")
		}
		if args[0] != "-c" || args[1] != arg {
			t.Fatal("diagnostic observation changed an argument")
		}
		if strings.Contains(log.String(), "SYNTHETIC_SECRET") {
			t.Fatal("opaque schema value leaked")
		}
	}
}
