package testutils

import (
	"io/ioutil"
	"os"
	"testing"
)

var tempDir = os.Getenv("OVERRIDE_TEMPDIR")

func GetTempFilename(tb testing.TB) string {
	f, err := ioutil.TempFile(tempDir, "vmscraper_diskqueue")
	if err != nil {
		tb.Fatal(err)
	}
	f.Close()
	os.Remove(f.Name())

	return f.Name()
}
