package main

import "testing"

func TestBuffer(t *testing.T) {
	buf := newBuffer(4096)

	//

	for i := 0; i < 3; i++ {
		buf.writeString(`foobar`)
	}
	if exp, got := `foobarfoobarfoobar`, buf.String(); exp != got {
		t.Fatalf("expected %q but got %q", exp, got)
	}

	//

	for i := 0; i < 10; i++ {
		buf.writeByte('_')
	}
	if exp, got := `foobarfoobarfoobar__________`, buf.String(); exp != got {
		t.Fatalf("expected %q but got %q", exp, got)
	}

	//

	for i := 0; i < 2; i++ {
		buf.write([]byte("hello"))
	}
	if exp, got := `foobarfoobarfoobar__________hellohello`, buf.String(); exp != got {
		t.Fatalf("expected %q but got %q", exp, got)
	}

	//

	for i := 0; i < 2; i++ {
		buf.appendInt(20000)
	}
	if exp, got := `foobarfoobarfoobar__________hellohello2000020000`, buf.String(); exp != got {
		t.Fatalf("expected %q but got %q", exp, got)
	}
}

func BenchmarkBuffer(b *testing.B) {
	buf := newBuffer(4096)

	data := []byte("hello")

	for i := 0; i < b.N; i++ {
		buf.reset()
		buf.writeString("foooooobar")
		buf.writeByte('_')
		buf.write(data)
		buf.appendInt(1234567890)
	}
}
