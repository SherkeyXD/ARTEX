package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebFetchReadsPageAndCapturesOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><script>secret script</script><style>hidden</style><body><h1>Hello</h1><p>Readable text</p></body></html>`)
	}))
	defer server.Close()
	input, _ := json.Marshal(map[string]string{"url": server.URL})
	value, err := NewWebFetch().Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	text, ok := value.(string)
	if !ok || text != "Hello Readable text" || strings.Contains(text, "script") {
		t.Fatalf("page text=%v", value)
	}
}

func TestWebFetchRejectsNonHTTPURL(t *testing.T) {
	_, err := NewWebFetch().Execute(context.Background(), json.RawMessage(`{"url":"file:///etc/passwd"}`))
	if err == nil {
		t.Fatal("non-HTTP URL accepted")
	}
}
